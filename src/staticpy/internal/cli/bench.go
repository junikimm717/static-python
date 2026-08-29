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
	Synopsis: "staticpy bench --interp NAME|LABEL=PATH ... [--baseline LABEL] [--cpu N] [--pyperformance DIR]",
	Long: `Runs a suite of pure-Python micro-benchmarks and a startup-latency probe
against a lineup of interpreters, and renders a markdown comparison report.

pyperformance is the long-term intent - it is what speed.python.org publishes
against - but it pip-installs each benchmark's dependencies into a venv, and
this interpreter has no pip (` + "`--with-ensurepip=no`" + `) and cannot dlopen the
C-extension dependencies pyperformance needs either way. Both are answered by
a bundle of pure-Python dependencies compiled in, which arrives with bundles
(see ` + "`config/bundles.toml`" + `). Until then this command uses a small
interpreter-bound suite (loops, dispatch, attribute access, dict/list churn)
that needs nothing beyond the standard library, plus a spawn-latency probe.

LINEUP
  Nothing is benchmarked unless it is named. --interp is repeatable and takes
  either a well-known name or an explicit path:
    --interp static             the pynative artifact for this machine
    --interp reference          the dynamic reference build
    --interp system             python3 from PATH
    --interp LABEL=/path/to/py  any other binary
  --baseline LABEL fixes the denominator of every ratio; without it the first
  --interp wins. Naming it matters: with auto-discovery, adding an interpreter
  could silently change which one everything was measured against.

  Each arm runs inside its own venv, so the arms differ only in the
  interpreter. Without one, a system python drags in distro site-packages --
  whose .pth files execute during the startup probe -- and sys.path ends up a
  different length per arm, which is a real cost in import-heavy benchmarks.
  --no-venv opts out; --pyperf DIR says where to find the pyperf package to
  install into each one.

SUITES
  The built-in suite needs nothing beyond the standard library. Passing
  --pyperformance DIR runs pyperformance's own benchmarks from that directory
  instead, skipping pyperformance's own runner and its per-benchmark pip install,
  which a --with-ensurepip=no interpreter cannot do. Benchmarks needing wheels
  are skipped and listed in skipped.json rather than silently dropped.

  Results land in dist/bench/<UTC-stamp>-<arch>/, never overwritten and never
  deduplicated: a measurement is not a pure function of its inputs. Alongside
  the report are the unaggregated pyperf JSON, a manifest recording each
  binary's sha256, and timeline.jsonl -- one record per measurement with its
  wall time, load average and the busy fraction of the pinned core's SMT
  sibling, which is what lets a suspicious number be audited months later.

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

The report is written to dist/bench/<UTC-stamp>_<arch>.md (or --out) and also
printed to stdout; --json prints the raw measurements instead.`,
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
	fs.Var(interpFlag{&interpOverrides}, "interp", "<label>=<path> to add or override one interpreter; repeatable")
	out := fs.String("out", "", "write the report here instead of dist/bench/<stamp>_<arch>.md")
	suiteRoot := fs.String("pyperformance", "", "run pyperformance's suite from this benchmarks directory instead of the built-in one")
	pyperfSrc := fs.String("pyperf", "", "directory holding the pyperf package to install into each venv")
	baselineFlag := fs.String("baseline", "", "the interpreter every ratio is measured against (default: the first --interp)")
	noVenv := fs.Bool("no-venv", false, "run interpreters directly instead of through a per-arm venv")
	timeout := fs.Duration("timeout", 400*time.Second, "per-benchmark timeout")
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
		return fmt.Errorf("nothing to compare: pass --interp at least twice.\n"+
			"  --interp static                the artifact for %s\n"+
			"  --interp reference             the dynamic reference build\n"+
			"  --interp system                python3 from PATH\n"+
			"  --interp LABEL=/path/to/python any other binary", host)
	}
	for _, label := range overrideOrder {
		p := overrides[label]
		if p == "" {
			var err error
			if p, err = resolveKnownInterp(g, label, abi, host, *build); err != nil {
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
	baseline := order[0]
	if *baselineFlag != "" {
		if !slices.Contains(order, *baselineFlag) {
			return fmt.Errorf("--baseline %q is not one of the interpreters being compared (have: %s)",
				*baselineFlag, strings.Join(order, ", "))
		}
		baseline = *baselineFlag
	}

	if *suiteRoot != "" {
		e, done, err := g.newEnv(cfg, true)
		if err != nil {
			return err
		}
		defer done()
		return runPyperfSuite(e, order, paths, baseline, *suiteRoot, *pyperfSrc,
			!*noVenv, *noPin, *cpu, *timeout)
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

	pin, topo := choosePin(*noPin, *cpu)

	measurements := map[string]*interpMeasurement{}
	for _, l := range order {
		fmt.Fprintf(os.Stderr, "%s %s (%s)\n", bold("benchmarking:"), l, paths[l])
		m, err := measureInterpreter(paths[l], scriptPath, *iters, *repeat)
		if err != nil {
			return fmt.Errorf("%s: %w", l, err)
		}
		measurements[l] = m
	}

	env := gatherBenchEnv()
	env.Affinity = pin.Describe()
	if topo != nil {
		env.Topology = topo.Describe()
	}
	arch := cfg.Targets[host].Arch
	stamp := time.Now().UTC().Format("2006-01-02T1504Z")

	if g.JSON {
		return emitJSON(benchJSONReport(arch, stamp, baseline, env, order, measurements))
	}

	report := renderBenchReport(arch, stamp, baseline, env, order, measurements)
	fmt.Print(report)

	reportPath := *out
	if reportPath == "" {
		if err := g.resolve(); err != nil {
			return err
		}
		reportPath = filepath.Join(g.Dist, "bench", stamp+"_"+arch+".md")
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", dim("saved:"), reportPath)
	return nil
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
func findStaticInterp(g *Global, abi string, build bool) (string, error) {
	s, err := g.session(recipe.PlanOptions{}, build)
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
	return findPythonBinary(root.ArtifactDir(s.e), abi)
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

// A bare well-known name resolves to the artifact staticpy built; anything
// else must say where it is.
func (f interpFlag) Set(v string) error {
	label, path, ok := strings.Cut(v, "=")
	if !ok {
		if !slices.Contains(knownInterps, v) {
			return fmt.Errorf("want <label>=<path>, or one of %s; got %q",
				strings.Join(knownInterps, ", "), v)
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

// Names that resolve without a path.
var knownInterps = []string{"static", "reference", "system"}

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

type benchEnv struct {
	Container bool
	CPUModel  string
	Cores     int
	CacheL1d  string
	CacheL1i  string
	CacheL2   string
	CacheL3   string
	Kernel    string
	// A ratio is only meaningful alongside the placement it was measured under.
	Topology string
	Affinity string
}

func gatherBenchEnv() benchEnv {
	e := benchEnv{Cores: runtime.NumCPU(), CPUModel: "unknown", Kernel: "unknown"}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		e.Container = true
	}
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, key := range []string{"model name", "Model name", "Hardware"} {
			if v := cpuinfoField(string(b), key); v != "" {
				e.CPUModel = v
				break
			}
		}
	}
	e.CacheL1d = cacheSizeAt(0)
	e.CacheL1i = cacheSizeAt(1)
	e.CacheL2 = cacheSizeAt(2)
	e.CacheL3 = cacheSizeAt(3)
	if out, err := exec.Command("uname", "-sr").Output(); err == nil {
		e.Kernel = strings.TrimSpace(string(out))
	}
	return e
}

func cpuinfoField(text, key string) string {
	for _, line := range strings.Split(text, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func cacheSizeAt(index int) string {
	b, err := os.ReadFile(fmt.Sprintf("/sys/devices/system/cpu/cpu0/cache/index%d/size", index))
	if err != nil {
		return "?"
	}
	return strings.TrimSpace(string(b))
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

func renderBenchReport(arch, stamp, baseline string, env benchEnv, order []string, m map[string]*interpMeasurement) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# staticpy bench -- %s -- %s\n\n", arch, stamp)
	fmt.Fprintf(&b, "Generated: %s\n\n", time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))

	b.WriteString("## Environment\n\n")
	container := ""
	if env.Container {
		container = " (inside container)"
	}
	fmt.Fprintf(&b, "- arch: `%s`%s\n", arch, container)
	fmt.Fprintf(&b, "- cpu: %s\n", env.CPUModel)
	fmt.Fprintf(&b, "- logical cores: %d\n", env.Cores)
	fmt.Fprintf(&b, "- caches: L1d %s / L1i %s / L2 %s / L3 %s\n", env.CacheL1d, env.CacheL1i, env.CacheL2, env.CacheL3)
	fmt.Fprintf(&b, "- kernel: %s\n", env.Kernel)
	if env.Topology != "" {
		fmt.Fprintf(&b, "- topology: %s\n", env.Topology)
	}
	fmt.Fprintf(&b, "- affinity: %s\n\n", env.Affinity)

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

func benchJSONReport(arch, stamp, baseline string, env benchEnv, order []string, m map[string]*interpMeasurement) map[string]any {
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
		"environment": map[string]any{
			"container": env.Container, "cpu_model": env.CPUModel, "logical_cores": env.Cores,
			"cache_l1d": env.CacheL1d, "cache_l1i": env.CacheL1i, "cache_l2": env.CacheL2, "cache_l3": env.CacheL3,
			"kernel": env.Kernel, "topology": env.Topology, "affinity": env.Affinity,
		},
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

func resolveKnownInterp(g *Global, label, abi, host string, build bool) (string, error) {
	switch label {
	case "static":
		p, err := findStaticInterp(g, abi, build)
		if err != nil {
			return "", fmt.Errorf("--interp static: %w\nBuild it with `staticpy build`, or pass --build", err)
		}
		return p, nil
	case "reference":
		p := filepath.Join(g.Dist, "artifacts", "pyref_default_"+host, "rootfs", "bin", "python"+abi)
		if !isExecutable(p) {
			return "", fmt.Errorf("--interp reference: no reference interpreter at %s\nBuild it with `staticpy build --reference`", p)
		}
		return p, nil
	case "system":
		p, err := exec.LookPath("python3")
		if err != nil {
			return "", fmt.Errorf("--interp system: no python3 on PATH")
		}
		return p, nil
	}
	return "", fmt.Errorf("--interp %s: unknown name", label)
}
