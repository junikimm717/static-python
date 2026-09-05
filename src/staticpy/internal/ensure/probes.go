package ensure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// ProbeModules are the extension modules a static interpreter is expected to
// have compiled in. Each is imported and reported as its own check, so one
// missing module reads as one line rather than a wall of tracebacks.
var ProbeModules = []string{
	"ssl", "zlib", "sqlite3", "ctypes", "_lzma", "_hashlib", "readline", "curses", "uuid", "compression.zstd",
}

type ProbeOptions struct {
	// Modules overrides ProbeModules.
	Modules []string
	// WantVersion, if set, is the prefix sys.version must start with.
	WantVersion string
	// PythonArgs is inserted before the script path.
	PythonArgs []string
}

const probeScriptName = "staticpy_probe.py"

// probeScript emits one PROBE line per check so a single interpreter start
// covers the whole smoke tier — under qemu, process startup dominates
// everything else here.
const probeScript = `
import sys

_BITS = int(sys.argv[1])
_WANT_VERSION = sys.argv[2]
_MODULES = sys.argv[3].split(",") if sys.argv[3] else []


def emit(name, ok, detail=""):
    detail = " ".join(str(detail).split())
    sys.stdout.write("PROBE\t%s\t%s\t%s\n" % (name, "ok" if ok else "fail", detail))
    sys.stdout.flush()


def guard(name, fn):
    try:
        ok, detail = fn()
    except BaseException as exc:
        emit(name, False, "%s: %s" % (type(exc).__name__, exc))
        return
    emit(name, ok, detail)


for _name in _MODULES:
    def _do(_name=_name):
        mod = __import__(_name)
        return True, getattr(mod, "__file__", None) or "builtin"
    guard("import:" + _name, _do)


def _version():
    text = sys.version
    if not text:
        return False, "sys.version is empty"
    want = "%d.%d" % sys.version_info[:2]
    if not text.startswith(want):
        return False, "sys.version %r does not start with %s" % (text, want)
    if _WANT_VERSION and not text.startswith(_WANT_VERSION):
        return False, "sys.version %r is not the built version %s" % (text, _WANT_VERSION)
    return True, " ".join(text.split())


guard("sys.version", _version)


def _platform():
    if sys.platform != "linux":
        return False, "sys.platform is %r, expected 'linux'" % (sys.platform,)
    return True, sys.platform


guard("sys.platform", _platform)


def _pointer_size():
    import ctypes
    import sysconfig

    want = _BITS // 8
    recorded = sysconfig.get_config_var("SIZEOF_VOID_P")
    live = ctypes.sizeof(ctypes.c_void_p)
    if recorded is None:
        return False, (
            "sysconfig.get_config_var('SIZEOF_VOID_P') is None: the _sysconfigdata module "
            "for this build is missing or was not the one that got installed"
        )
    recorded = int(recorded)
    if recorded != want:
        return False, (
            "sysconfig SIZEOF_VOID_P is %d but the target is %d-bit (want %d): "
            "_sysconfigdata came from a different build" % (recorded, _BITS, want)
        )
    if live != want:
        return False, (
            "ctypes.sizeof(c_void_p) is %d but the target is %d-bit (want %d)" % (live, _BITS, want)
        )
    return True, "%d bytes, sysconfig and ctypes agree" % want


guard("sysconfig.SIZEOF_VOID_P", _pointer_size)


def _pythonapi():
    import ctypes

    fn = ctypes.pythonapi.Py_GetVersion
    fn.restype = ctypes.c_char_p
    fn.argtypes = []
    raw = fn()
    if raw is None:
        return False, "ctypes.pythonapi.Py_GetVersion() returned NULL"
    text = raw.decode("utf-8", "replace")
    want = "%d.%d" % sys.version_info[:2]
    if not text.startswith(want):
        return False, "Py_GetVersion() said %r, expected it to start with %s" % (text, want)
    return True, " ".join(text.split())


guard("ctypes.pythonapi", _pythonapi)
`

// RunProbes is the smoke tier: start the built interpreter, import everything
// the recipe promised, and confirm it agrees with the target it was built for.
func RunProbes(ctx context.Context, r *core.Runner, l *Launcher, t config.Target, python, work string, opts ProbeOptions) *Report {
	rep := NewReport(fmt.Sprintf("smoke %s (%s)", t.Triple, l.Runner))
	start := time.Now()
	defer func() { rep.Dur = time.Since(start) }()

	modules := opts.Modules
	if modules == nil {
		modules = ProbeModules
	}
	bits := t.Bits
	if bits == 0 {
		bits = 64
	}

	dir := filepath.Join(work, "probes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		rep.Fail("probe:setup", err, "cannot create the probe working directory %s", dir)
		return rep
	}
	script := filepath.Join(dir, probeScriptName)
	if err := os.WriteFile(script, []byte(probeScript), 0o644); err != nil {
		rep.Fail("probe:setup", err, "cannot write %s", script)
		return rep
	}

	args := append(append([]string(nil), opts.PythonArgs...), "-B", script,
		strconv.Itoa(bits), opts.WantVersion, strings.Join(modules, ","))
	// The artifact directory is published read-only; a stray .pyc write would
	// fail the run for a reason that has nothing to do with the interpreter.
	l.Env["PYTHONDONTWRITEBYTECODE"] = "1"

	res, err := l.Run(ctx, r, "smoke-probes", dir, python, args...)
	if err != nil {
		rep.Fail("probe:startup", err, "the interpreter could not be executed at all")
		return rep
	}

	seen := parseProbes(res.Stdout)
	if len(seen) == 0 {
		rep.FailCmd("probe:startup", res, fmt.Errorf("the interpreter produced no probe output"),
			"%s", l.FailDetail(res))
		return rep
	}
	rep.PassIn("probe:startup", res.Dur, "interpreter started and ran %d probes", len(seen))

	want := make([]string, 0, len(modules)+4)
	for _, m := range modules {
		want = append(want, "import:"+m)
	}
	want = append(want, "sys.version", "sys.platform", "sysconfig.SIZEOF_VOID_P", "ctypes.pythonapi")

	for _, name := range want {
		p, ok := seen[name]
		switch {
		case !ok:
			// A probe with no line did not merely fail: the interpreter died
			// partway, so the command output is the only evidence there is.
			rep.FailCmd("probe:"+name, res,
				fmt.Errorf("no result: the interpreter stopped before this probe ran"),
				"%s", l.FailDetail(res))
		case p.ok:
			rep.Pass("probe:"+name, "%s", p.detail)
		default:
			rep.Add(Check{
				Name: "probe:" + name, Status: StatusFail,
				Err:    fmt.Errorf("%s", p.detail),
				Cmd:    res.Argv,
				Dir:    res.Dir,
				Output: res.Stderr,
			})
		}
	}
	return rep
}

type probeLine struct {
	ok     bool
	detail string
}

func parseProbes(stdout string) map[string]probeLine {
	out := map[string]probeLine{}
	for _, line := range strings.Split(stdout, "\n") {
		if !strings.HasPrefix(line, "PROBE\t") {
			continue
		}
		f := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 4)
		if len(f) < 3 {
			continue
		}
		detail := ""
		if len(f) == 4 {
			detail = f[3]
		}
		out[f[1]] = probeLine{ok: f[2] == "ok", detail: detail}
	}
	return out
}
