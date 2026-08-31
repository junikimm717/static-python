package cli

import (
	"debug/elf"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/assets"
	"github.com/junikimm717/static-python/src/staticpy/internal/bench"
	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/recipe"
)

var cmdBench = &command{
	Name:     "bench",
	Short:    "compare interpreters' CPU and startup performance",
	Synopsis: "staticpy bench --interp NAME|LABEL=PATH ... [--baseline LABEL] [--cpu N] [--suite NAME]",
	Long: `Runs pyperformance against a lineup of interpreters and renders a markdown
and HTML comparison report.

pyperformance is the default because it is what speed.python.org publishes
against. It is installed into each arm's venv at a pinned version
(pyperformance ` + bench.DefaultPyperformance + `, pyperf ` + bench.DefaultPyperf + `, --no-deps), so nothing has to be located
or passed in. ` + "`--with-ensurepip=no`" + ` means pip was never installed into the
interpreter's own prefix; it leaves the ensurepip module and its bundled wheel
in the stdlib, so ` + "`-m venv`" + ` seeds a working pip. What a static interpreter
genuinely cannot do is load a C extension, so a benchmark whose requirement
ships one is dropped by name into skipped.json rather than quietly excluded.

Every session records protocol ` + fmt.Sprintf("%d", bench.Protocol) + `. Bump that constant when a measurement bug
means old numbers cannot be compared to new ones; a pyperformance pin bump is
a suite change, not a protocol bump.

--suite micro selects the built-in suite instead: eleven stdlib-only loops
(dispatch, attribute access, dict/list churn) plus a spawn-latency probe. It
needs no network and no installation, which makes it the offline answer and a
quick check - but it is a micro-benchmark suite, and reporting its geometric
mean as though it described a workload overstates whatever the interpreter is
good at.

LINEUP
  Nothing is benchmarked unless it is named. --interp is repeatable and takes
  a well-known name, a profile name, or an explicit path:
    --interp static             the pynative artifact for --profile
    --interp reference          the dynamic interpreter, from
                                ` + "`staticpy build --profile reference`" + `
    --interp system             python3 from PATH
    --interp PROFILE            any other profile's built interpreter
                                (nomimalloc, nolto, reference-nolto, ...)
    --interp LABEL=/path/to/py  any other binary
  Name each arm. There is no bundled lineup: the comparison set is whatever
  --interp flags you pass, in that order. --baseline LABEL fixes the
  denominator of every ratio. When the lineup contains reference and
  --baseline is omitted, reference is the baseline; otherwise the first
  --interp wins.

  Each arm runs inside its own venv, so the arms differ only in the
  interpreter. Without one, a system python drags in distro site-packages --
  whose .pth files execute during the startup probe -- and sys.path ends up a
  different length per arm, which is a real cost in import-heavy benchmarks.
  --no-venv opts out; --pyperf DIR says where to find the pyperf package to
  install into each one.

SUITES
  --suite pyperformance (the default) installs pyperformance into each venv and
  runs its benchmarks directly, installing each one's requirements first.
  --pyperformance DIR uses a copy already on disk instead of installing one,
  which is also how an --offline run gets a suite.

  pyperformance's own runner is still not used: it builds a venv per benchmark,
  and the arms have to differ only in the interpreter. Three benchmarks take a
  required positional sub-benchmark (bm_argparse, bm_async_tree, bm_pickle);
  one variant of each is run and the report names it, as bm_pickle[pickle].
  Anything whose dependencies will not install is listed in skipped.json with
  the reason, because a geometric mean over a silently narrowed set is worse
  than no number at all.

  Results land in dist/bench/<UTC-stamp>-<arch>/, never overwritten and never
  deduplicated: a measurement is not a pure function of its inputs. The session
  holds report.md, report.html (geomean bars, ratio table, env, identities),
  the unaggregated pyperf JSON, a manifest with protocol and pins plus each
  binary's sha256, env.json (kernel, cpu, memory, affinity), and
  timeline.jsonl -- one record per measurement with its wall time, load
  average and the busy fraction of the pinned core's SMT sibling, which is
  what lets a suspicious number be audited months later.

MEASUREMENT
  The run is confined to one logical CPU with sched_setaffinity, inherited by
  every interpreter spawned. On a hybrid CPU the fast and slow core types can
  differ by more than 1.5x, so an unpinned run that migrates between them
  varies by more than most effects under test. --no-pin opts out and says so
  in the report.

  --cpu N picks the core. Without it, and on a terminal, bench shows the
  machine's topology and asks; the slower core type is listed but not
  selectable. With no terminal it takes the fastest-class core the firmware
  ranks highest, skipping cpu0 because it takes the most interrupts, and
  prints the --cpu that would have pinned the choice.

  Pinning is not isolation. Nothing here reserves the core from the rest of
  the system, and an SMT sibling under load biases the pinned core through
  shared execution units. bench samples the pinned CPU and its siblings before
  measuring and warns when one is busy, but a clean warning is not a promise:
  for numbers you intend to publish, run on an otherwise idle machine.

  --repeat N runs the CPU suite N times and keeps each benchmark's MINIMUM,
  not its mean. Benchmark noise is one-sided -- scheduling, cache eviction and
  frequency scaling can only make a run slower than the machine is capable of
  -- so the minimum is the best estimate of the true speed, and an average
  folds in exactly the contamination the repeats exist to remove. The spread
  between the best and worst repeat is reported alongside as the noise
  indicator.

Native only: under qemu you would be measuring qemu, and the overhead is not
uniform across benchmarks, so the numbers would be comparable to nothing - not
to native, and not to each other. Passing --target for anything but this
machine's own triple is refused.

The markdown report is also printed to stdout; --json prints the raw
measurements instead. --out writes a copy of the markdown beside the session
directory.`,
	Run: runBench,
}

func runBench(g *Global, args []string) error {
	fs := g.flagSet("bench")
	iters := fs.Int("iters", 40, "startup-probe iterations per scenario")
	repeat := fs.Int("repeat", 1, "run the CPU suite this many times and keep each benchmark's minimum")
	noPin := fs.Bool("no-pin", false, "do not confine the run to one CPU (results will vary with scheduler placement)")
	cpu := fs.Int("cpu", -1, "logical CPU to pin every arm to; unset opens a picker showing the machine's topology")
	build := fs.Bool("build", false, "build the static interpreter first if it is missing")
	var only []string
	fs.Var(listFlag{&only}, "only", "restrict to these labels, comma-separated or repeated")
	var interpOverrides []interpEntry
	fs.Var(interpFlag{&interpOverrides}, "interp", "well-known name (static, reference, system), a profile, or <label>=<path>; repeatable")
	out := fs.String("out", "", "also write the markdown report here (session dir is still used)")
	suite := fs.String("suite", "pyperformance", "which benchmarks to run: \"pyperformance\" (installed into each venv) or \"micro\" (the built-in stdlib-only suite)")
	suiteRoot := fs.String("pyperformance", "", "use pyperformance's benchmarks from this directory instead of installing them")
	pyperfSrc := fs.String("pyperf", "", "directory holding the pyperf package to install into each venv")
	baselineFlag := fs.String("baseline", "", "the interpreter every ratio is measured against (default: reference if present, else the first --interp)")
	noVenv := fs.Bool("no-venv", false, "run interpreters directly instead of through a per-arm venv")
	// Generous on purpose. This is a hang detector, not a budget: pinned to one
	// core, bm_base64 legitimately takes over seven minutes, and a limit tight
	// enough to be interesting would drop real measurements and leave the
	// geomean quietly computed over the benchmarks that happened to be quick.
	timeout := fs.Duration("timeout", 20*time.Minute, "per-benchmark timeout; a hang detector, not a budget")
	if err := parse(fs, args); err != nil {
		return finish("bench", err)
	}
	if *iters < 1 {
		return usagef("--iters must be at least 1, got %d", *iters)
	}
	if *repeat < 1 {
		return usagef("--repeat must be at least 1, got %d", *repeat)
	}

	cfg, err := g.load()
	if err != nil {
		return err
	}
	pins := pinsOf(cfg)
	host, err := g.HostTriple(cfg)
	if err != nil {
		return err
	}
	targets, err := g.selectTargets(cfg, host)
	if err != nil {
		return err
	}
	if len(targets) != 1 || targets[0] != host {
		return fmt.Errorf("bench compares interpreters natively and only makes sense for this machine's own triple (%s); got --target %s.\nUnder qemu you would be measuring qemu, not the interpreter",
			host, strings.Join(targets, " "))
	}
	abi, err := pythonABI(cfg)
	if err != nil {
		return err
	}

	overrides := map[string]string{}
	var overrideOrder []string
	for _, e := range interpOverrides {
		if _, ok := overrides[e.Label]; !ok {
			overrideOrder = append(overrideOrder, e.Label)
		}
		overrides[e.Label] = e.Path
	}
	var order []string
	paths := map[string]string{}
	add := func(label, path string) {
		if _, ok := paths[label]; !ok {
			order = append(order, label)
		}
		paths[label] = path
	}

	// Nothing is benchmarked unless it was asked for by name. Auto-discovery
	// would let adding an interpreter silently change the baseline, and with
	// it every ratio the report prints.
	if len(overrideOrder) == 0 {
		return fmt.Errorf("nothing to benchmark: pass --interp at least once, or twice to compare.\n"+
			"  --interp static                the artifact for %s\n"+
			"  --interp reference             the dynamic reference build\n"+
			"  --interp system                python3 from PATH\n"+
			"  --interp PROFILE               any other profile's interpreter\n"+
			"  --interp LABEL=/path/to/python any other binary", host)
	}
	for _, label := range overrideOrder {
		p := overrides[label]
		if p == "" {
			var err error
			if p, err = resolveKnownInterp(g, cfg, label, abi, *build); err != nil {
				return err
			}
		}
		add(label, p)
	}

	if len(only) > 0 {
		wanted := map[string]bool{}
		for _, l := range only {
			wanted[l] = true
		}
		var filtered []string
		for _, l := range order {
			if wanted[l] {
				filtered = append(filtered, l)
			}
		}
		for l := range wanted {
			if !slices.Contains(filtered, l) {
				return fmt.Errorf("--only %q: no such interpreter was found (have: %s)", l, strings.Join(order, ", "))
			}
		}
		order = filtered
	}
	if len(order) == 0 {
		return fmt.Errorf("no interpreters to benchmark; pass --interp label=path, or build the static interpreter first")
	}
	for _, l := range order {
		if !isExecutable(paths[l]) {
			return fmt.Errorf("interpreter %q at %s is missing or not executable", l, paths[l])
		}
	}
	baseline, err := pickBaseline(order, *baselineFlag)
	if err != nil {
		return err
	}

	switch *suite {
	case "pyperformance", "micro":
	default:
		return fmt.Errorf("--suite %q: want \"pyperformance\" or \"micro\"", *suite)
	}
	// A --pyperformance directory names the suite as surely as --suite does.
	if *suite == "pyperformance" || *suiteRoot != "" {
		e, done, err := g.newEnv(cfg, true)
		if err != nil {
			return err
		}
		defer done()
		return runPyperfSuite(e, order, paths, baseline, *suiteRoot, *pyperfSrc,
			!*noVenv, *noPin, *cpu, *timeout, g.Offline, pins)
	}

	tmpDir, err := os.MkdirTemp("", "staticpy-bench-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	if err := assets.WriteTo(tmpDir, "bench/microbench.py"); err != nil {
		return err
	}
	scriptPath := filepath.Join(tmpDir, "bench", "microbench.py")

	pin, topo, err := choosePin(*noPin, *cpu)
	if err != nil {
		return err
	}

	measurements := map[string]*interpMeasurement{}
	for _, l := range order {
		fmt.Fprintf(os.Stderr, "%s %s (%s)\n", bold("benchmarking:"), l, paths[l])
		m, err := measureInterpreter(paths[l], scriptPath, *iters, *repeat)
		if err != nil {
			return fmt.Errorf("%s: %w", l, err)
		}
		measurements[l] = m
	}

	env := bench.ReadMachine()
	env.Affinity = pin.Describe()
	if topo != nil {
		env.Topology = topo.Describe()
	}
	arch := cfg.Targets[host].Arch
	sess, err := bench.NewSession(g.Dist, runtime.GOARCH, time.Now())
	if err != nil {
		return err
	}
	defer sess.Close()
	if err := sess.WriteJSON("manifest.json", bench.Manifest(sess.Stamp, baseline, pins, nil, nil)); err != nil {
		return err
	}
	if err := sess.WriteJSON("env.json", env); err != nil {
		return err
	}

	if g.JSON {
		return emitJSON(benchJSONReport(arch, sess.Stamp, baseline, env, pins, order, measurements))
	}

	report := renderBenchReport(arch, sess.Stamp, baseline, env, pins, order, measurements)
	fmt.Print(report)
	if err := os.WriteFile(filepath.Join(sess.Dir, "report.md"), []byte(report), 0o644); err != nil {
		return err
	}
	if *out != "" {
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", dim("session:"), sess.Dir)
	return nil
}

func pinsOf(cfg *config.Config) bench.Pins {
	p := bench.Pins{
		Pyperformance: cfg.Bench.Pyperformance,
		Pyperf:        cfg.Bench.Pyperf,
	}
	if p.Pyperformance == "" {
		p.Pyperformance = bench.DefaultPyperformance
	}
	if p.Pyperf == "" {
		p.Pyperf = bench.DefaultPyperf
	}
	return p
}

func pythonABI(cfg *config.Config) (string, error) {
	s, err := lookupSource(cfg, "python")
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(s.Version, ".", 3)
	if len(parts) < 2 {
		return "", fmt.Errorf("the pinned python version %q has no major.minor to take an ABI from", s.Version)
	}
	return parts[0] + "." + parts[1], nil
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// findStaticInterp resolves (and, with build, produces) this machine's own
// pynative artifact. Any failure here - not built, or no toolchain provisioned
// at all - is meant to be a soft "skip static" for the caller, not fatal: bench
// is still useful comparing just dynamic and system.
// findBuiltInterp locates the interpreter one profile produces, building it
// first when asked. An empty profile means whatever --profile selected.
func findBuiltInterp(g *Global, profile, abi string, build bool) (string, error) {
	s, err := g.session(recipe.PlanOptions{Profile: profile}, build)
	if err != nil {
		return "", err
	}
	defer s.close()
	root := s.jobs[0]

	nodes, err := core.Plan(s.e, s.jobs)
	if err != nil {
		return "", err
	}
	if !planNodeValid(nodes, root.Slug()) {
		if !build {
			return "", fmt.Errorf("not built yet at %s (pass --build, or run `./staticpy build` first)", root.ArtifactDir(s.e))
		}
		ctx, stop := signalContext()
		defer stop()
		if err := core.Run(ctx, s.e, s.jobs); err != nil {
			return "", err
		}
		if nodes, err = core.Plan(s.e, s.jobs); err != nil {
			return "", err
		}
		if !planNodeValid(nodes, root.Slug()) {
			return "", fmt.Errorf("build did not produce a valid artifact at %s", root.ArtifactDir(s.e))
		}
	}
	dir := root.ArtifactDir(s.e)
	// A host-built reference publishes a whole rootfs; pynative publishes the
	// prefix itself.
	if sub := filepath.Join(dir, "rootfs"); isDir(sub) {
		dir = sub
	}
	return findPythonBinary(dir, abi)
}

func planNodeValid(nodes []core.PlanNode, slug string) bool {
	for _, n := range nodes {
		if n.Job.Slug() == slug {
			return n.Valid
		}
	}
	return false
}

func findPythonBinary(dir, abi string) (string, error) {
	p := filepath.Join(dir, "bin", "python"+abi)
	if isExecutable(p) {
		return p, nil
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "bin", "python3.*"))
	sort.Strings(matches)
	for _, m := range matches {
		if isExecutable(m) {
			return m, nil
		}
	}
	return "", fmt.Errorf("no interpreter binary under %s/bin", dir)
}

// interpEntry and its flag.Value adapter keep --interp's label=path pairs in
// the order given, unlike the shared kvFlag (a map), so the report's column
// order for user-supplied interpreters is predictable.
type interpEntry struct{ Label, Path string }

type interpFlag struct{ v *[]interpEntry }

func (f interpFlag) String() string {
	if f.v == nil {
		return ""
	}
	var parts []string
	for _, e := range *f.v {
		parts = append(parts, e.Label+"="+e.Path)
	}
	return strings.Join(parts, ",")
}

// A bare name is a well-known interp or a profile.
// Profiles are validated after config load; Set has none.
func (f interpFlag) Set(v string) error {
	label, path, ok := strings.Cut(v, "=")
	if !ok {
		if v == "" {
			return fmt.Errorf("empty --interp")
		}
		*f.v = append(*f.v, interpEntry{Label: v})
		return nil
	}
	if label == "" || path == "" {
		return fmt.Errorf("want <label>=<path>, got %q", v)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	*f.v = append(*f.v, interpEntry{Label: label, Path: abs})
	return nil
}

// Names that resolve without a path, plus every profile.
var wellKnownInterps = []string{"static", "reference", "system"}

func pickBaseline(order []string, flag string) (string, error) {
	if flag != "" {
		if !slices.Contains(order, flag) {
			return "", fmt.Errorf("--baseline %q is not one of the interpreters being compared (have: %s)",
				flag, strings.Join(order, ", "))
		}
		return flag, nil
	}
	if slices.Contains(order, "reference") {
		return "reference", nil
	}
	return order[0], nil
}

// cpuBenchOrder fixes the CPU table's row order. It must match the
// registration order of BENCHMARKS in assets/files/bench/microbench.py -
// results come back as a JSON object, and object key order is not something
// Go's map-based unmarshal preserves.
var cpuBenchOrder = []string{
	"fib_recursive", "fib_iter", "arith_loop", "listcomp", "dictops",
	"attr_access", "str_format", "regex_match", "json_roundtrip",
	"func_call", "except_path",
}

type startupScenario struct {
	Name string
	Args []string
}

var startupScenarios = []startupScenario{
	{"bare", []string{"-c", "pass"}},
	{"sysmod", []string{"-c", "import sys"}},
	{"stdlib", []string{"-c", "import json, re, os, sys, collections, hashlib"}},
}

type startupStat struct{ MinMs, MedianMs float64 }

type interpMeasurement struct {
	Path      string
	Version   string
	Linkage   string
	SizeBytes int64
	SizeLabel string
	CPU       map[string]float64
	// Whether the minimum can be trusted: (max-min)/min across repeats.
	CPUSpread map[string]float64
	Startup   map[string]startupStat
}

func measureInterpreter(path, scriptPath string, iters, repeat int) (*interpMeasurement, error) {
	version, err := runCapture(path, "-c", "import sys; print(sys.version.split()[0])")
	if err != nil {
		return nil, fmt.Errorf("version probe: %w", err)
	}
	linkage, sizeBytes, sizeLabel, err := linkageAndSize(path)
	if err != nil {
		return nil, fmt.Errorf("inspect binary: %w", err)
	}
	// Minimum per benchmark, not averaged: see bench.Reduce.
	series := map[string][]float64{}
	for range repeat {
		one, err := runMicrobench(path, scriptPath)
		if err != nil {
			return nil, fmt.Errorf("cpu micro-benchmarks: %w", err)
		}
		for k, v := range one {
			series[k] = append(series[k], v)
		}
	}
	cpu := make(map[string]float64, len(series))
	spread := make(map[string]float64, len(series))
	for k, vs := range series {
		agg, ok := bench.Reduce(vs)
		if !ok {
			return nil, fmt.Errorf("cpu micro-benchmarks: %s produced no usable samples", k)
		}
		cpu[k] = agg.Min
		spread[k] = agg.Spread()
	}
	startup, err := measureStartup(path, iters)
	if err != nil {
		return nil, fmt.Errorf("startup probe: %w", err)
	}
	return &interpMeasurement{
		Path: path, Version: version, Linkage: linkage,
		SizeBytes: sizeBytes, SizeLabel: sizeLabel,
		CPU: cpu, CPUSpread: spread, Startup: startup,
	}, nil
}

func runCapture(path string, args ...string) (string, error) {
	cmd := exec.Command(path, args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// linkageAndSize replaces the shell script's `file` + `ldd` combo with
// debug/elf, so bench needs nothing on the host beyond the interpreters it is
// comparing.
func linkageAndSize(path string) (linkage string, sizeBytes int64, sizeLabel string, err error) {
	real, rerr := filepath.EvalSymlinks(path)
	if rerr != nil {
		real = path
	}
	fi, err := os.Stat(real)
	if err != nil {
		return "", 0, "", err
	}
	stubBytes := fi.Size()

	f, oerr := elf.Open(real)
	if oerr != nil {
		return "unknown", stubBytes, humanBytes(stubBytes), nil
	}
	defer f.Close()
	libs, lerr := f.ImportedLibraries()
	if lerr != nil || len(libs) == 0 {
		return "static", stubBytes, humanBytes(stubBytes), nil
	}

	total := stubBytes
	label := humanBytes(stubBytes)
	for _, lib := range libs {
		if !strings.HasPrefix(lib, "libpython") {
			continue
		}
		prefix := filepath.Dir(filepath.Dir(real)) // .../bin/pythonX.Y -> prefix
		candidate := filepath.Join(prefix, "lib", lib)
		if lfi, serr := os.Stat(candidate); serr == nil {
			total += lfi.Size()
			label = fmt.Sprintf("%s stub + %s libpython = %s", humanBytes(stubBytes), humanBytes(lfi.Size()), humanBytes(total))
		}
		break
	}
	return "dynamic", total, label, nil
}

func runMicrobench(path, scriptPath string) (map[string]float64, error) {
	out, err := exec.Command(path, scriptPath).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	var parsed struct {
		Results map[string]struct {
			NsPerOp float64 `json:"ns_per_op"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("parse output: %w", err)
	}
	res := make(map[string]float64, len(parsed.Results))
	for k, v := range parsed.Results {
		res[k] = v.NsPerOp
	}
	return res, nil
}

func measureStartup(path string, iters int) (map[string]startupStat, error) {
	out := make(map[string]startupStat, len(startupScenarios))
	for _, sc := range startupScenarios {
		for range 3 {
			if err := runQuiet(path, sc.Args); err != nil {
				return nil, fmt.Errorf("%s warmup: %w", sc.Name, err)
			}
		}
		samples := make([]float64, iters)
		for i := range iters {
			t0 := time.Now()
			if err := runQuiet(path, sc.Args); err != nil {
				return nil, fmt.Errorf("%s: %w", sc.Name, err)
			}
			samples[i] = time.Since(t0).Seconds() * 1000
		}
		out[sc.Name] = startupStat{MinMs: minFloat(samples), MedianMs: medianFloat(samples)}
	}
	return out, nil
}

func runQuiet(path string, args []string) error {
	cmd := exec.Command(path, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func minFloat(xs []float64) float64 {
	m := xs[0]
	for _, v := range xs[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func medianFloat(xs []float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func geomean(vals []float64) float64 {
	sum := 0.0
	for _, v := range vals {
		sum += math.Log(v)
	}
	return math.Exp(sum / float64(len(vals)))
}

func fmtNs(ns float64) string {
	switch {
	case ns >= 1_000_000:
		return fmt.Sprintf("%.2f ms", ns/1_000_000)
	case ns >= 1_000:
		return fmt.Sprintf("%.2f us", ns/1_000)
	default:
		return fmt.Sprintf("%.1f ns", ns)
	}
}

func renderBenchReport(arch, stamp, baseline string, env bench.Machine, pins bench.Pins, order []string, m map[string]*interpMeasurement) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# staticpy bench -- %s -- %s\n\n", arch, stamp)
	fmt.Fprintf(&b, "Generated: %s\n\n", time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))

	b.WriteString(bench.EnvMarkdown(env, bench.Protocol, pins))
	container := ""
	if env.Container {
		container = " (inside container)"
	}
	fmt.Fprintf(&b, "- arch: `%s`%s\n", arch, container)
	fmt.Fprintf(&b, "- baseline: %s\n\n", baseline)

	writeRow := func(label string, get func(string) string) {
		fmt.Fprintf(&b, "| %s |", label)
		for _, l := range order {
			fmt.Fprintf(&b, " %s |", get(l))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "| |")
	for _, l := range order {
		fmt.Fprintf(&b, " %s |", l)
	}
	b.WriteString("\n|---")
	for range order {
		b.WriteString("|---")
	}
	b.WriteString("|\n")
	writeRow("executable", func(l string) string { return "`" + m[l].Path + "`" })
	writeRow("version", func(l string) string { return m[l].Version })
	writeRow("linkage", func(l string) string { return m[l].Linkage })
	writeRow("size on disk", func(l string) string { return m[l].SizeLabel })
	b.WriteString("\n")

	var others []string
	for _, l := range order {
		if l != baseline {
			others = append(others, l)
		}
	}

	b.WriteString("## CPU micro-benchmarks (lower ns/op is better)\n\n")
	fmt.Fprintf(&b, "Best of 7 runs after warmup; values are per inner-loop op.")
	if len(others) > 0 {
		fmt.Fprintf(&b, " Ratio column is X / %s: > 1 means %s was faster on that row. The final row is the geometric mean of those ratios.", baseline, baseline)
	}
	b.WriteString("\n\n")
	header := append([]string{"benchmark"}, order...)
	for _, o := range others {
		header = append(header, o+"/"+baseline)
	}
	b.WriteString("| " + strings.Join(header, " | ") + " |\n")
	b.WriteString("|---" + strings.Repeat("|---:", len(header)-1) + "|\n")
	ratios := map[string][]float64{}
	for _, name := range cpuBenchOrder {
		cells := []string{name}
		for _, l := range order {
			if v, ok := m[l].CPU[name]; ok {
				cells = append(cells, fmtNs(v))
			} else {
				cells = append(cells, "-")
			}
		}
		base, hasBase := m[baseline].CPU[name]
		for _, o := range others {
			v, ok := m[o].CPU[name]
			if !ok || !hasBase || base == 0 {
				cells = append(cells, "-")
				continue
			}
			r := v / base
			ratios[o] = append(ratios[o], r)
			cells = append(cells, fmt.Sprintf("%.2fx", r))
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	if len(others) > 0 {
		cells := []string{"**geomean (X / " + baseline + ")**"}
		for range order {
			cells = append(cells, "")
		}
		for _, o := range others {
			cells = append(cells, fmt.Sprintf("**%.2fx**", geomean(ratios[o])))
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	b.WriteString("\n")

	b.WriteString("## Startup / first-import latency (lower ms is better)\n\n")
	fmt.Fprintf(&b, "Wall-clock spawn time, min of the sampled runs.")
	if len(others) > 0 {
		fmt.Fprintf(&b, " Ratio column is X / %s: > 1 means %s spawned faster. Final row is the geometric mean across scenarios.", baseline, baseline)
	}
	b.WriteString("\n\n")
	header = append([]string{"scenario"}, order...)
	for _, o := range others {
		header = append(header, o+"/"+baseline)
	}
	b.WriteString("| " + strings.Join(header, " | ") + " |\n")
	b.WriteString("|---" + strings.Repeat("|---:", len(header)-1) + "|\n")
	startRatios := map[string][]float64{}
	for _, sc := range startupScenarios {
		cells := []string{sc.Name}
		for _, l := range order {
			cells = append(cells, fmt.Sprintf("%.2f ms", m[l].Startup[sc.Name].MinMs))
		}
		base := m[baseline].Startup[sc.Name].MinMs
		for _, o := range others {
			v := m[o].Startup[sc.Name].MinMs
			if base == 0 {
				cells = append(cells, "-")
				continue
			}
			r := v / base
			startRatios[o] = append(startRatios[o], r)
			cells = append(cells, fmt.Sprintf("%.2fx", r))
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	if len(others) > 0 {
		cells := []string{"**geomean (X / " + baseline + ")**"}
		for range order {
			cells = append(cells, "")
		}
		for _, o := range others {
			cells = append(cells, fmt.Sprintf("**%.2fx**", geomean(startRatios[o])))
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	return b.String()
}

func benchJSONReport(arch, stamp, baseline string, env bench.Machine, pins bench.Pins, order []string, m map[string]*interpMeasurement) map[string]any {
	interps := make([]map[string]any, 0, len(order))
	for _, l := range order {
		mm := m[l]
		startup := map[string]any{}
		for _, sc := range startupScenarios {
			s := mm.Startup[sc.Name]
			startup[sc.Name] = map[string]float64{"min_ms": s.MinMs, "median_ms": s.MedianMs}
		}
		interps = append(interps, map[string]any{
			"label": l, "executable": mm.Path, "version": mm.Version,
			"linkage": mm.Linkage, "size_bytes": mm.SizeBytes,
			"cpu_ns_per_op": mm.CPU, "startup_ms": startup,
		})
	}
	return map[string]any{
		"arch": arch, "stamp": stamp, "baseline": baseline,
		"protocol":     bench.Protocol,
		"suite":        map[string]string{"pyperformance": pins.Pyperformance, "pyperf": pins.Pyperf},
		"environment":  env,
		"interpreters": interps,
	}
}

func applyPin(disabled bool) (bench.Pin, *bench.Topology) {
	topo, err := bench.ReadTopology()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s cannot read cpu topology (%v); running unpinned\n", yellow("note:"), err)
		return bench.Pin{}, nil
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", bold("machine:"), topo.Describe())
	if disabled {
		if topo.Hybrid {
			fmt.Fprintf(os.Stderr, "%s --no-pin on a hybrid cpu: runs that migrate between core classes are not comparable\n", yellow("warning:"))
		}
		return bench.Pin{}, topo
	}
	pin, err := topo.Apply()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %v; running unpinned\n", yellow("note:"), err)
		return pin, topo
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", bold("affinity:"), pin.Describe())

	busy, err := pin.CheckQuiet(300*time.Millisecond, 0.20)
	if err == nil && len(busy) > 0 {
		for _, b := range busy {
			what := "the pinned cpu"
			if b.CPU != pin.CPU {
				what = "an SMT sibling of the pinned cpu"
			}
			fmt.Fprintf(os.Stderr, "%s cpu%d (%s) is %.0f%% busy; measurements taken now will be biased\n",
				yellow("warning:"), b.CPU, what, b.Frac*100)
		}
	}
	return pin, topo
}

func resolveKnownInterp(g *Global, cfg *config.Config, label, abi string, build bool) (string, error) {
	switch label {
	case "static":
		p, err := findBuiltInterp(g, "", abi, build)
		if err != nil {
			return "", fmt.Errorf("--interp static: %w\nBuild it with `staticpy build`, or pass --build", err)
		}
		return p, nil
	case "reference":
		p, err := findBuiltInterp(g, config.ProfileReference, abi, build)
		if err != nil {
			return "", fmt.Errorf("--interp reference: %w\n"+
				"Build it with `staticpy build --profile %s`, or pass --build",
				err, config.ProfileReference)
		}
		return p, nil
	case "system":
		p, err := exec.LookPath("python3")
		if err != nil {
			return "", fmt.Errorf("--interp system: no python3 on PATH")
		}
		return p, nil
	}
	if cfg != nil {
		if _, ok := cfg.Profiles[label]; ok {
			p, err := findBuiltInterp(g, label, abi, build)
			if err != nil {
				return "", fmt.Errorf("--interp %s: %w\nBuild it with `staticpy build --profile %s`, or pass --build", label, err, label)
			}
			return p, nil
		}
	}
	return "", fmt.Errorf("--interp %s: unknown name (want %s, or a profile name)",
		label, strings.Join(wellKnownInterps, ", "))
}
