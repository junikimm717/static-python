package ensure

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// The two ways a target binary can be executed, and the key half of an
// expectation lookup. qemu-user has its own failures around signals, threads
// and subprocesses that say nothing about whether the build is correct, so an
// expectation recorded under qemu must never silence the same test running
// natively.
const (
	RunnerNative = "native"
	RunnerQemu   = "qemu"
)

// DefaultRunTimeout bounds a single target invocation. A probe run that hangs
// under qemu would otherwise hold a build slot forever.
const DefaultRunTimeout = 10 * time.Minute

// RunResult is one execution of a target binary. A non-zero ExitCode is a
// normal outcome here — it becomes a failed Check — so it is reported rather
// than returned as an error.
type RunResult struct {
	Argv     []string      `json:"argv"`
	Dir      string        `json:"dir,omitempty"`
	ExitCode int           `json:"exit_code"`
	Stdout   string        `json:"-"`
	Stderr   string        `json:"-"`
	Dur      time.Duration `json:"-"`
	// TimedOut distinguishes a hang from a crash; they look identical in the
	// exit status alone.
	TimedOut bool `json:"timed_out,omitempty"`
}

func (r RunResult) OK() bool { return r.ExitCode == 0 && !r.TimedOut }

// Combined is stdout followed by stderr, labelled when both are non-empty, for
// attaching to a failed check.
func (r RunResult) Combined() string {
	out := strings.TrimRight(r.Stdout, "\n")
	errOut := strings.TrimRight(r.Stderr, "\n")
	switch {
	case out == "":
		return errOut
	case errOut == "":
		return out
	}
	return "stdout:\n" + out + "\nstderr:\n" + errOut
}

// Launcher knows how to execute a binary built for one target.
type Launcher struct {
	// Runner is RunnerNative or RunnerQemu; it is half the expectation key.
	Runner string
	// Prefix is prepended to the program's argv: empty natively, the qemu
	// binary plus -L <sysroot> otherwise.
	Prefix  []string
	Sysroot string
	Env     map[string]string
	Timeout time.Duration
}

// NewLauncher decides how to run binaries for t and fails loudly when it
// cannot. staticpy never fetches qemu: the shim provisions it and passes it in
// through Env.Qemu, so a missing entry is a provisioning bug, not something to
// paper over by hoping binfmt_misc is registered.
func NewLauncher(e *core.Env, t config.Target) (*Launcher, error) {
	l := &Launcher{Timeout: DefaultRunTimeout, Env: map[string]string{}}

	if IsNativeTarget(t) {
		l.Runner = RunnerNative
		return l, nil
	}
	l.Runner = RunnerQemu

	qemu := ""
	if e != nil {
		qemu = e.Qemu[t.Triple]
	}
	if qemu == "" {
		return nil, fmt.Errorf("cannot run %s binaries: no qemu configured for %s. "+
			"staticpy does not fetch qemu; the shim provisions %s and passes it in as Env.Qemu[%q]. "+
			"Either provision it or restrict the build to targets this machine can execute",
			t.Triple, t.Triple, QemuBinaryName(t), t.Triple)
	}
	st, err := os.Stat(qemu)
	if err != nil {
		return nil, fmt.Errorf("cannot run %s binaries: qemu for %s is configured as %s but is not usable: %w",
			t.Triple, t.Triple, qemu, err)
	}
	if st.IsDir() || st.Mode()&0o111 == 0 {
		return nil, fmt.Errorf("cannot run %s binaries: qemu for %s is configured as %s, which is not an executable file",
			t.Triple, t.Triple, qemu)
	}
	l.Prefix = []string{qemu}

	// The interpreter itself is static and needs no loader, but the suite
	// execs other things; -L lets qemu resolve anything that does, and turns a
	// baffling loader error into a working run.
	if sysroot, err := Sysroot(e, t); err == nil {
		l.Sysroot = sysroot
		l.Prefix = append(l.Prefix, "-L", sysroot)
		l.Env["QEMU_LD_PREFIX"] = sysroot
	}
	return l, nil
}

// QemuBinaryName is the qemu-user binary the shim is expected to supply.
func QemuBinaryName(t config.Target) string {
	name := t.Qemu
	if name == "" {
		name = strings.SplitN(t.Triple, "-", 2)[0]
	}
	if strings.HasPrefix(name, "qemu-") {
		return name
	}
	return "qemu-" + name
}

// Sysroot is the target sysroot inside the provisioned toolchain,
// <toolchain>/<triple>, matching the musl-cross-make layout gccfactory emits.
func Sysroot(e *core.Env, t config.Target) (string, error) {
	if e == nil {
		return "", fmt.Errorf("no build environment, so the sysroot for %s cannot be resolved", t.Triple)
	}
	dir, err := e.ToolchainDir(t.Triple, core.KindCross)
	if err != nil {
		var nerr error
		dir, nerr = e.ToolchainDir(t.Triple, core.KindNative)
		if nerr != nil {
			return "", err
		}
	}
	sysroot := filepath.Join(dir, t.Triple)
	if fi, serr := os.Stat(sysroot); serr != nil || !fi.IsDir() {
		return "", fmt.Errorf("no sysroot for %s at %s", t.Triple, sysroot)
	}
	return sysroot, nil
}

// Argv is the full command line for running prog under this launcher.
func (l *Launcher) Argv(prog string, args ...string) []string {
	out := make([]string, 0, len(l.Prefix)+1+len(args))
	out = append(out, l.Prefix...)
	return append(append(out, prog), args...)
}

// Run executes prog for the target and returns its exit status, stdout and
// stderr. A non-zero exit is reported in the result; the error is reserved for
// failures to execute at all.
func (l *Launcher) Run(ctx context.Context, r *core.Runner, name, dir, prog string, args ...string) (RunResult, error) {
	argv := l.Argv(prog, args...)
	res := RunResult{Argv: argv, Dir: dir}

	timeout := l.Timeout
	if timeout <= 0 {
		timeout = DefaultRunTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if r != nil {
		r.Step(name)
	}

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = l.environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	err := cmd.Run()
	res.Dur = time.Since(start)
	res.Stdout, res.Stderr = stdout.String(), stderr.String()

	switch {
	case err == nil:
		return res, nil
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		res.TimedOut = true
		res.ExitCode = -1
		return res, nil
	case errors.Is(ctx.Err(), context.Canceled):
		return res, ctx.Err()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		return res, nil
	}
	return res, fmt.Errorf("%s: cannot execute %s: %w", name, ShJoin(argv), err)
}

func (l *Launcher) environ() []string {
	env := os.Environ()
	keys := make([]string, 0, len(l.Env))
	for k := range l.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+l.Env[k])
	}
	return env
}

// FailDetail explains a non-zero exit in the terms an operator can act on.
func (l *Launcher) FailDetail(res RunResult) string {
	var b strings.Builder
	switch {
	case res.TimedOut:
		fmt.Fprintf(&b, "timed out after %s", res.Dur.Round(time.Second))
	default:
		fmt.Fprintf(&b, "exit status %d after %s", res.ExitCode, res.Dur.Round(time.Millisecond))
	}
	if l.Runner == RunnerQemu {
		b.WriteString(" (under " + l.Prefix[0] + ")")
	}
	if hint := runHint(res.Combined()); hint != "" {
		b.WriteString("\nhint: " + hint)
	}
	return b.String()
}

// runHint names the environmental failures that otherwise read as a mysterious
// interpreter crash.
func runHint(out string) string {
	switch {
	case strings.Contains(out, "Exec format error"), strings.Contains(out, "cannot execute binary file"):
		return "a foreign binary was executed without qemu; the target needs a qemu-user binary in Env.Qemu"
	case strings.Contains(out, "Could not open") && strings.Contains(out, "ld-musl"):
		return "qemu could not find the musl loader; pass -L <sysroot>, or check that the interpreter really is static"
	case strings.Contains(out, "Unable to find dynamic library"):
		return "the binary is dynamically linked against a library the sysroot does not have"
	case strings.Contains(out, "ModuleNotFoundError"), strings.Contains(out, "No module named"):
		return "the interpreter started but its stdlib is not where it expects; check PYTHONHOME and the frozen module set"
	}
	return ""
}

// IsNativeTarget reports whether this machine can execute the target's
// binaries directly.
func IsNativeTarget(t config.Target) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	id, ok := IdentityFor(t)
	if !ok {
		return false
	}
	host, ok := goarchIdentity[runtime.GOARCH]
	return ok && host == id
}

var goarchIdentity = func() map[string]identity {
	out := map[string]identity{}
	for goarch, arch := range map[string]string{
		"amd64":   "x86_64",
		"386":     "i386",
		"arm64":   "aarch64",
		"arm":     "arm",
		"riscv64": "riscv64",
		"ppc64":   "powerpc64",
		"ppc64le": "powerpc64le",
		"mips64":  "mips64",
		"s390x":   "s390x",
	} {
		if id, ok := identities[arch]; ok {
			out[goarch] = id
		}
	}
	return out
}()
