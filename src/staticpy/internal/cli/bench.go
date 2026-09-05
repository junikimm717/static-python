package cli

import (
	"encoding/json"
	"fmt"
	"io"
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
                                (nomimalloc, nolto, seplto, reference-nolto, ...)
    --interp LABEL=/path/to/py  any other binary
  Name each arm. There is no bundled lineup: the comparison set is whatever
  --interp flags you pass, in that order. --baseline LABEL fixes the
  denominator of every ratio. When the lineup contains reference and
  --baseline is omitted, reference is the baseline; otherwise the first
  --interp wins.

  --kit DIR is the other way to name a lineup: an unpacked kit tarball
  (staticpy kit) already lists every arm in kit.json, so ./run on the quiet
  box passes --kit and nothing else. --interp still adds or overrides an
  arm. Results write to DIR/results/ rather than dist/bench/. --cpu is the
  same pin as without a kit. The session manifest copies kit.json in
  full (python_version, pins, pack-time arm hashes, git_revision) so the
  experiment can be retraced without the tarball. git_revision also
  comes from kit.json when the runner itself was not stamped.

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
  deduplicated: a measurement is not a pure function of its inputs. Every
  suite writes the same files: report.json (rows + geomean_vs_baseline,
  >1 is faster), report.md, report.html, manifest.json (protocol, suite.name,
  pins, each binary's sha256/factors/artifact key, venv packages, this
  executable's git revision), env.json (kernel, cpu, memory, affinity,
  fingerprint identity + telemetry), skipped.json, and timeline.jsonl --
  one record per measurement with its wall time, load average and the busy
  fraction of the pinned core's SMT sibling. pyperformance also keeps the
  unaggregated pyperf JSON under raw/.

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

The markdown report is also printed to stdout; --json prints report.json
instead. The session directory is written either way. --out writes a copy of
the markdown beside the session directory.`,
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
	kitDir := fs.String("kit", "", "unpacked kit directory; lineup comes from kit.json")
	listArms := fs.Bool("list", false, "print the lineup and exit")
	if err := parse(fs, args); err != nil {
		return finish("bench", err)
	}
	g.applyGitRevision()
	if *iters < 1 {
		return usagef("--iters must be at least 1, got %d", *iters)
	}
	if *repeat < 1 {
		return usagef("--repeat must be at least 1, got %d", *repeat)
	}

	if *kitDir != "" {
		abs, err := filepath.Abs(*kitDir)
		if err != nil {
			return err
		}
		*kitDir = abs
		if !g.flagGiven("dist") {
			g.Dist = filepath.Join(*kitDir, "results", ".staticpy")
		}
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

	var kitDoc *bench.KitDoc
	sessionParent := ""
	findLinks := ""
	if *kitDir != "" {
		if *build {
			return fmt.Errorf("--kit cannot be combined with --build; the kit already holds the interpreters")
		}
		kitDoc, err = bench.LoadKit(*kitDir)
		if err != nil {
			return err
		}
		if err := kitDoc.MatchesThisMachine(); err != nil {
			return err
		}
		if kitDoc.Triple != "" && kitDoc.Triple != host {
			return fmt.Errorf("kit is for %s; this machine is %s", kitDoc.Triple, host)
		}
		bench.AdoptKitRevision(kitDoc)
		sessionParent = filepath.Join(*kitDir, "results")
		if vendor := filepath.Join(*kitDir, "vendor"); isDir(vendor) {
			findLinks = vendor
		}
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

	// Nothing is benchmarked unless it was asked for by name, or a kit
	// already named the lineup. Auto-discovery would let adding an
	// interpreter silently change the baseline, and with it every ratio.
	if len(overrideOrder) == 0 {
		if kitDoc == nil {
			return fmt.Errorf("nothing to benchmark: pass --interp at least once, or twice to compare.\n"+
				"  --interp static                the artifact for %s\n"+
				"  --interp reference             the dynamic reference build\n"+
				"  --interp system                python3 from PATH\n"+
				"  --interp PROFILE               any other profile's interpreter\n"+
				"  --interp LABEL=/path/to/python any other binary\n"+
				"  --kit DIR                      lineup from an unpacked kit", host)
		}
		kitOrder, kitPaths, err := kitDoc.ResolveArms(*kitDir)
		if err != nil {
			return err
		}
		for _, label := range kitOrder {
			add(label, kitPaths[label])
		}
		if *baselineFlag == "" {
			*baselineFlag = kitDoc.Baseline
		}
	} else {
		var kitPaths map[string]string
		if kitDoc != nil {
			_, kitPaths, err = kitDoc.ResolveArms(*kitDir)
			if err != nil {
				return err
			}
		}
		for _, label := range overrideOrder {
			p := overrides[label]
			if p == "" && kitPaths != nil {
				p = kitPaths[label]
			}
			if p == "" {
				if p, err = resolveKnownInterp(g, cfg, label, abi, *build); err != nil {
					return err
				}
			}
			add(label, p)
		}
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
	if *listArms {
		for _, l := range order {
			fmt.Printf("%s\t%s\n", l, paths[l])
		}
		fmt.Fprintf(os.Stderr, "baseline %s\n", baseline)
		return nil
	}

	switch *suite {
	case "pyperformance", "micro":
	default:
		return fmt.Errorf("--suite %q: want \"pyperformance\" or \"micro\"", *suite)
	}
	e, done, err := g.newEnv(cfg, true)
	if err != nil {
		return err
	}
	defer done()
	// A --pyperformance directory names the suite as surely as --suite does.
	if *suite == "pyperformance" || *suiteRoot != "" {
		return runPyperfSuite(g, cfg, e, order, paths, baseline, *suiteRoot, *pyperfSrc,
			!*noVenv, *noPin, *cpu, *timeout, g.Offline, pins, sessionParent, findLinks, *out, kitDoc)
	}
	return runMicroSuite(g, cfg, e, order, paths, baseline, *noPin, *cpu, *iters, *repeat, pins, sessionParent, *out, kitDoc)
}

func pinnedPythonVersion(cfg *config.Config) string {
	s, err := lookupSource(cfg, "python")
	if err != nil {
		return ""
	}
	return s.Version
}

func publishSession(sess *bench.Session, md string, report map[string]any, out string, asJSON bool) error {
	if asJSON {
		if err := emitJSON(report); err != nil {
			return err
		}
	} else {
		fmt.Print(md)
	}
	if out != "" {
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, []byte(md), 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "\n%s %s\n", bold("session:"), sess.Dir)
	return nil
}

func runMicroSuite(g *Global, cfg *config.Config, e *core.Env, order []string, paths map[string]string,
	baseline string, noPin bool, cpu, iters, repeat int, pins bench.Pins, sessionParent, outPath string, kit *bench.KitDoc) error {

	tmpDir, err := os.MkdirTemp("", "staticpy-bench-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	if err := assets.WriteTo(tmpDir, "bench/microbench.py"); err != nil {
		return err
	}
	scriptPath := filepath.Join(tmpDir, "bench", "microbench.py")

	pin, topo, err := choosePin(noPin, cpu)
	if err != nil {
		return err
	}
	machine := bench.ReadMachine()
	topoDesc := ""
	if topo != nil {
		topoDesc = topo.Describe()
	}
	machine.SetRunPlacement(pin.Describe(), topoDesc)

	sess, err := openBenchSession(g.Dist, sessionParent, runtime.GOARCH, time.Now())
	if err != nil {
		return err
	}
	defer sess.Close()

	var ids []bench.Identity
	for _, label := range order {
		id, err := bench.Identify(label, paths[label])
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		enrichIdentity(g, cfg, e, &id)
		ids = append(ids, id)
	}
	acc := bench.Accounting{
		Baseline:      baseline,
		SuiteName:     bench.SuiteMicro,
		Pins:          pins,
		Identities:    ids,
		Skipped:       []string{},
		Machine:       machine,
		Kit:           kit,
		PythonVersion: pinnedPythonVersion(cfg),
	}
	if err := sess.WriteAccounting(acc); err != nil {
		return err
	}

	measurements := map[string]*interpMeasurement{}
	for _, l := range order {
		fmt.Fprintf(os.Stderr, "%s %s (%s)\n", bold("benchmarking:"), l, paths[l])
		m, err := measureInterpreter(sess, pin, l, paths[l], scriptPath, iters, repeat)
		if err != nil {
			return fmt.Errorf("%s: %w", l, err)
		}
		measurements[l] = m
	}

	rows, geo := bench.Compare(samplesToResults(order, measurements), baseline, order)
	md, report, err := sess.WriteReports(bench.Reports{
		Accounting: acc,
		Order:      order,
		Rows:       rows,
		Geomean:    geo,
	})
	if err != nil {
		return err
	}
	return publishSession(sess, md, report, outPath, g.JSON)
}

func openBenchSession(dist, parent, arch string, now time.Time) (*bench.Session, error) {
	if parent != "" {
		return bench.NewSessionIn(parent, arch, now)
	}
	return bench.NewSession(dist, arch, now)
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

type startupScenario struct {
	Name string
	Args []string
}

var startupScenarios = []startupScenario{
	{"bare", []string{"-c", "pass"}},
	{"sysmod", []string{"-c", "import sys"}},
	{"stdlib", []string{"-c", "import json, re, os, sys, collections, hashlib"}},
}

type interpMeasurement struct {
	CPU     map[string][]float64 // ns/op
	Startup map[string][]float64 // milliseconds
}

func samplesToResults(order []string, ms map[string]*interpMeasurement) bench.Results {
	res := bench.Results{}
	for _, l := range order {
		res[l] = map[string][]float64{}
		m := ms[l]
		if m == nil {
			continue
		}
		for name, ns := range m.CPU {
			secs := make([]float64, len(ns))
			for i, v := range ns {
				secs[i] = v / 1e9
			}
			res[l][name] = secs
		}
		for name, millis := range m.Startup {
			secs := make([]float64, len(millis))
			for i, v := range millis {
				secs[i] = v / 1000
			}
			res[l]["startup."+name] = secs
		}
	}
	return res
}

func measureInterpreter(sess *bench.Session, pin bench.Pin, label, path, scriptPath string, iters, repeat int) (*interpMeasurement, error) {
	series := map[string][]float64{}
	for range repeat {
		var one map[string]float64
		err := sess.Trace(pin, label, "cpu", func() error {
			var e error
			one, e = runMicrobench(path, scriptPath)
			return e
		})
		if err != nil {
			return nil, fmt.Errorf("cpu micro-benchmarks: %w", err)
		}
		for k, v := range one {
			series[k] = append(series[k], v)
		}
	}
	startup := make(map[string][]float64, len(startupScenarios))
	for _, sc := range startupScenarios {
		var samples []float64
		err := sess.Trace(pin, label, "startup."+sc.Name, func() error {
			var e error
			samples, e = measureStartupScenario(path, sc, iters)
			return e
		})
		if err != nil {
			return nil, err
		}
		startup[sc.Name] = samples
	}
	return &interpMeasurement{CPU: series, Startup: startup}, nil
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

func measureStartupScenario(path string, sc startupScenario, iters int) ([]float64, error) {
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
	return samples, nil
}

func runQuiet(path string, args []string) error {
	cmd := exec.Command(path, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
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
