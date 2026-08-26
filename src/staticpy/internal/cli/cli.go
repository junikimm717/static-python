// Package cli is staticpy's command surface. `staticpy help` and
// `staticpy help <command>` are the documentation: every flag carries a
// sentence saying what it does and, where it is not obvious, why its default is
// what it is. If you find yourself wanting to write a doc file, put it in a
// command's Long string instead.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/ensure"
	"github.com/junikimm717/static-python/src/staticpy/internal/logging"
)

// Global holds every flag that is accepted on either side of the subcommand.
type Global struct {
	Dist       string
	ConfigDir  string
	SourcesDir string

	Toolchains string
	Overrides  map[string]string
	Busybox    string
	Qemu       map[string]string

	Profile string
	Host    string
	Targets []string

	Workers int
	Jobs    int

	Offline    bool
	hermetic   bool
	noHermetic bool
	KeepWork   bool
	Verbose    bool
	JSON       bool
	ColorWhen  string

	repoRoot string
	resolved bool
}

type command struct {
	Name     string
	Short    string
	Synopsis string
	Long     string
	Run      func(g *Global, args []string) error
}

// registry is populated in init rather than as a var initializer: the commands
// reference help rendering, which reads the registry, and Go rejects that as an
// initialization cycle.
var registry []*command

func init() {
	registry = []*command{
		cmdBuild, cmdStatus, cmdVerify, cmdLogs, cmdShell,
		cmdDoctor, cmdConfig, cmdSources, cmdPrint,
	}
}

func commands() []*command { return registry }

func lookup(name string) *command {
	for _, c := range commands() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func Main(args []string) int {
	g := &Global{
		Dist:       os.Getenv("STATICPY_DIST"),
		Toolchains: os.Getenv("STATICPY_TOOLCHAINS"),
		Busybox:    os.Getenv("STATICPY_BUSYBOX"),
		Profile:    envOr("STATICPY_PROFILE", "default"),
		Host:       os.Getenv("STATICPY_HOST"),
		ColorWhen:  envOr("STATICPY_COLOR", "auto"),
		Overrides:  map[string]string{},
		Qemu:       map[string]string{},
	}
	setColor(g.ColorWhen)

	if len(args) == 0 {
		printHelp(os.Stdout, "")
		return 0
	}

	// Global flags are accepted before the subcommand as well as after it, so
	// `staticpy -v build` and `staticpy build -v` both work.
	pre := flag.NewFlagSet("staticpy", flag.ContinueOnError)
	pre.SetOutput(io.Discard)
	g.register(pre)
	if err := pre.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printHelp(os.Stdout, "")
			return 0
		}
		fmt.Fprintf(os.Stderr, "staticpy: %v\n\nRun `staticpy help` for usage.\n", err)
		return 2
	}
	rest := pre.Args()
	if len(rest) == 0 {
		printHelp(os.Stdout, "")
		return 0
	}
	setColor(g.ColorWhen)

	name, rest := rest[0], rest[1:]
	switch name {
	case "help", "-h", "--help":
		topic := ""
		if len(rest) > 0 {
			topic = rest[0]
		}
		if topic != "" && lookup(topic) == nil && topic != "layout" && topic != "targets" {
			fmt.Fprintf(os.Stderr, "staticpy: no such command %q\n", topic)
			printHelp(os.Stderr, "")
			return 2
		}
		printHelp(os.Stdout, topic)
		return 0
	}

	cmd := lookup(name)
	if cmd == nil {
		fmt.Fprintf(os.Stderr, "staticpy: unknown command %q\n\n", name)
		printHelp(os.Stderr, "")
		return 2
	}

	if err := cmd.Run(g, rest); err != nil {
		switch {
		case errors.Is(err, errUsage):
			fmt.Fprintf(os.Stderr, "staticpy %s: %v\n\n", cmd.Name, err)
			printHelp(os.Stderr, cmd.Name)
			return 2
		case errors.Is(err, context.Canceled):
			fmt.Fprintln(os.Stderr, dim("interrupted"))
			return 130
		}
		var q quietExit
		if errors.As(err, &q) {
			return q.code
		}
		fmt.Fprintf(os.Stderr, "%s %v\n", red("error:"), err)
		return 1
	}
	return 0
}

// errUsage marks an error that should be followed by the command's help text.
var errUsage = errors.New("usage")

type usageError struct{ msg string }

func (e usageError) Error() string         { return e.msg }
func (e usageError) Is(target error) bool  { return target == errUsage }
func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

// quietExit carries an exit status for a command that has already printed its
// own report, so a failed `verify` does not append a redundant error line under
// a table that already says which checks failed.
type quietExit struct{ code int }

func (e quietExit) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func (g *Global) register(fs *flag.FlagSet) {
	fs.StringVar(&g.Dist, "dist", g.Dist,
		"artifact + build root (default ./dist, or <repo>/dist from a checkout; the ./staticpy shim sets it)")
	fs.StringVar(&g.ConfigDir, "config", g.ConfigDir,
		"extra config directory, overlaid on the embedded defaults and on <repo>/config")
	fs.StringVar(&g.SourcesDir, "sources", g.SourcesDir,
		"read sources.toml and patches/ from here instead of the binary; this is a supply-chain override and is recorded in every artifact's manifest")
	fs.StringVar(&g.Toolchains, "toolchains", g.Toolchains,
		"directory of provisioned toolchains, one <triple>-cross or <triple>-native subdir each")
	fs.Var(kvFlag{&g.Overrides, "triple"}, "toolchain",
		"<triple>=<path> for one hand-built toolchain tree; repeatable, wins over --toolchains")
	fs.StringVar(&g.Busybox, "busybox", g.Busybox,
		"busybox binary supplying sh/awk/sed to hermetic builds (default: whatever is on PATH)")
	fs.Var(kvFlag{&g.Qemu, "triple"}, "qemu",
		"<triple>=<path> to a qemu-user binary for running that target's binaries; repeatable")
	fs.StringVar(&g.Profile, "profile", g.Profile,
		"flag profile from profiles.toml (default \"default\")")
	fs.StringVar(&g.Host, "host", g.Host,
		"build machine's triple; inferred from this machine's architecture, pass it only when that inference is ambiguous")
	fs.Var(listFlag{&g.Targets}, "target",
		"target triple to build for; repeatable, comma-separated accepted, empty means this machine's own triple")
	fs.IntVar(&g.Workers, "workers", g.Workers,
		"how many jobs to build at once (default: up to 4, leaving each one a share of the CPUs)")
	fs.IntVar(&g.Jobs, "j", g.Jobs,
		"-j handed to each job's make (default: CPUs divided by --workers)")
	fs.BoolVar(&g.Offline, "offline", g.Offline,
		"never touch the network; only sources already verified in dist/src may be used")
	fs.BoolVar(&g.hermetic, "hermetic", g.hermetic,
		"compose PATH from busybox and the selected toolchain only (default when a busybox is available)")
	fs.BoolVar(&g.noHermetic, "no-hermetic", g.noHermetic,
		"let the host PATH through: friendlier on a dev box, reproducible nowhere")
	fs.BoolVar(&g.KeepWork, "keep-work", g.KeepWork,
		"keep dist/work/<job> after a job succeeds, so `staticpy shell <slug>` has a tree to land in")
	fs.BoolVar(&g.Verbose, "v", g.Verbose,
		"mirror every command's output to the terminal as it runs (it always goes to dist/logs in full)")
	fs.BoolVar(&g.Verbose, "verbose", g.Verbose, "alias for -v")
	fs.BoolVar(&g.JSON, "json", g.JSON,
		"emit machine-readable JSON instead of a table, where the command has a JSON form")
	fs.StringVar(&g.ColorWhen, "color", g.ColorWhen, "colorize output: auto|always|never")
}

// flagSet returns a FlagSet that also understands the global flags, so they may
// appear on either side of the subcommand name.
func (g *Global) flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("staticpy "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	g.register(fs)
	return fs
}

// A single fs.Parse stops at the first positional, silently dropping flags
// after it; re-parsing past each one keeps `logs <slug> --follow` working.
func parse(fs *flag.FlagSet, args []string) error {
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return errHelpRequested
			}
			return usagef("%v", err)
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(positional) > 0 {
		if err := fs.Parse(positional); err != nil {
			return usagef("%v", err)
		}
	}
	return nil
}

var errHelpRequested = errors.New("help requested")

func finish(name string, err error) error {
	if errors.Is(err, errHelpRequested) {
		printHelp(os.Stdout, name)
		return nil
	}
	return err
}

type listFlag struct{ v *[]string }

func (f listFlag) String() string {
	if f.v == nil {
		return ""
	}
	return strings.Join(*f.v, ",")
}

func (f listFlag) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*f.v = append(*f.v, p)
		}
	}
	return nil
}

type kvFlag struct {
	m   *map[string]string
	key string
}

func (f kvFlag) String() string {
	if f.m == nil || *f.m == nil {
		return ""
	}
	var parts []string
	for _, k := range sortedKeys(*f.m) {
		parts = append(parts, k+"="+(*f.m)[k])
	}
	return strings.Join(parts, ",")
}

func (f kvFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || k == "" || val == "" {
		return fmt.Errorf("want <%s>=<path>, got %q", f.key, v)
	}
	if *f.m == nil {
		*f.m = map[string]string{}
	}
	abs, err := filepath.Abs(val)
	if err != nil {
		return err
	}
	(*f.m)[k] = abs
	return nil
}

// resolve fixes the paths every command needs. It is idempotent: commands call
// it directly when they need only paths, and through load/newEnv otherwise.
func (g *Global) resolve() error {
	if g.resolved {
		return nil
	}
	setColor(g.ColorWhen)
	if g.hermetic && g.noHermetic {
		return usagef("--hermetic and --no-hermetic contradict each other")
	}
	g.repoRoot = findRepoRoot()
	if g.Dist == "" {
		if g.repoRoot != "" {
			g.Dist = filepath.Join(g.repoRoot, "dist")
		} else {
			g.Dist = "dist"
		}
	}
	abs, err := filepath.Abs(g.Dist)
	if err != nil {
		return fmt.Errorf("resolve --dist %s: %w", g.Dist, err)
	}
	g.Dist = abs
	if g.repoRoot == "" {
		g.repoRoot = filepath.Dir(abs)
	}
	if g.Toolchains != "" {
		if g.Toolchains, err = filepath.Abs(g.Toolchains); err != nil {
			return err
		}
	}
	if g.Busybox == "" {
		if p, err := exec.LookPath("busybox"); err == nil {
			g.Busybox = p
		}
	} else if g.Busybox, err = filepath.Abs(g.Busybox); err != nil {
		return err
	}
	if g.Profile == "" {
		g.Profile = "default"
	}
	g.resolved = true
	return nil
}

// findRepoRoot looks for the checkout this binary belongs to, so a run from a
// clone picks up the editable config/ tree next to the shim. A released binary
// finds nothing and uses only what is embedded in it.
func findRepoRoot() string {
	var starts []string
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	for _, s := range starts {
		for d := s; ; d = filepath.Dir(d) {
			shim := filepath.Join(d, "staticpy")
			if fi, err := os.Stat(shim); err == nil && !fi.IsDir() {
				if st, err := os.Stat(filepath.Join(d, "config")); err == nil && st.IsDir() {
					return d
				}
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	return ""
}

func (g *Global) load() (*config.Config, error) {
	if err := g.resolve(); err != nil {
		return nil, err
	}
	if g.SourcesDir != "" {
		fmt.Fprintf(os.Stderr, "%s sources.toml and patches/ are being read from %s instead of the copy pinned in this binary.\n",
			yellow("warning:"), g.SourcesDir)
		fmt.Fprintf(os.Stderr, "  Checksums and patches are the supply chain; anything built this way is stamped with that provenance in its manifest.\n")
	}
	cfg, err := config.Load(config.Options{
		RepoRoot:   g.repoRoot,
		Dir:        g.ConfigDir,
		SourcesDir: g.SourcesDir,
	})
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// goarchToArch maps this binary's architecture onto the `arch` column of
// targets.toml. The triple is then whichever target row carries that arch.
var goarchToArch = map[string]string{
	"amd64":   "x86_64",
	"386":     "i386",
	"arm64":   "aarch64",
	"arm":     "arm",
	"riscv64": "riscv64",
	"ppc64":   "powerpc64",
	"ppc64le": "powerpc64le",
	"mips64":  "mips64",
	"s390x":   "s390x",
}

// HostTriple is the triple of the machine staticpy is running on. It decides
// which builds are native and which are crosses, so getting it wrong would
// silently turn a cross build into a native one.
func (g *Global) HostTriple(cfg *config.Config) (string, error) {
	if g.Host != "" {
		if _, ok := cfg.Targets[g.Host]; !ok {
			return "", fmt.Errorf("--host %q is not a target in targets.toml.\nConfigured targets: %s",
				g.Host, strings.Join(sortedKeys(cfg.Targets), ", "))
		}
		return g.Host, nil
	}
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("staticpy builds linux-musl interpreters and has to run on Linux; this binary is %s/%s",
			runtime.GOOS, runtime.GOARCH)
	}
	arch, ok := goarchToArch[runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("this machine is %s, which staticpy has no target architecture for.\nKnown architectures: %s.\nPass --host <triple> if one of the configured targets does run here",
			runtime.GOARCH, strings.Join(sortedKeys(goarchToArch), ", "))
	}
	var all, proven []string
	for _, name := range sortedKeys(cfg.Targets) {
		t := cfg.Targets[name]
		if t.Arch != arch {
			continue
		}
		all = append(all, t.Triple)
		if t.Status == "proven" {
			proven = append(proven, t.Triple)
		}
	}
	if len(proven) == 1 {
		return proven[0], nil
	}
	switch len(all) {
	case 0:
		return "", fmt.Errorf("this machine is %s (arch %q) and no row in targets.toml has that arch.\nConfigured targets: %s.\nAdd a target row for %s, or pass --host <triple>",
			runtime.GOARCH, arch, strings.Join(sortedKeys(cfg.Targets), ", "), arch)
	case 1:
		return all[0], nil
	}
	return "", fmt.Errorf("this machine is %s (arch %q) and several targets share it: %s.\nThey differ in ABI, so staticpy will not guess: pass --host <triple>",
		runtime.GOARCH, arch, strings.Join(all, ", "))
}

func (g *Global) selectTargets(cfg *config.Config, host string) ([]string, error) {
	if len(g.Targets) == 0 {
		return []string{host}, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, name := range g.Targets {
		if name == "all" {
			for _, n := range sortedKeys(cfg.Targets) {
				if !seen[n] {
					seen[n] = true
					out = append(out, n)
				}
			}
			continue
		}
		if name == "proven" {
			for _, n := range sortedKeys(cfg.Targets) {
				if cfg.Targets[n].Status == "proven" && !seen[n] {
					seen[n] = true
					out = append(out, n)
				}
			}
			continue
		}
		if _, ok := cfg.Targets[name]; !ok {
			return nil, fmt.Errorf("unknown target %q.\nConfigured targets: %s\n(\"all\" and \"proven\" are accepted as well)",
				name, strings.Join(sortedKeys(cfg.Targets), ", "))
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

// defaultParallelism splits the machine between concurrent jobs and each job's
// own make -j. One job with -j$(nproc) leaves the CPU idle every time a
// configure script runs; four jobs each with -j$(nproc) oversubscribes it by
// 4x. The product is what matters, so the default keeps it at nproc.
func defaultParallelism(workers, jobs int) (int, int) {
	cpus := runtime.NumCPU()
	if workers < 1 {
		workers = 4
		if cpus < workers {
			workers = cpus
		}
		if workers < 1 {
			workers = 1
		}
	}
	if jobs < 1 {
		jobs = cpus / workers
		if jobs < 1 {
			jobs = 1
		}
	}
	return workers, jobs
}

// newEnv builds the runtime context every job shares. runLog decides whether
// this invocation opens a dist/logs/runs/<stamp> stream: read-only commands do
// not, so `staticpy status` in a loop does not litter the log tree.
func (g *Global) newEnv(cfg *config.Config, runLog bool) (*core.Env, func(), error) {
	if err := g.resolve(); err != nil {
		return nil, nil, err
	}
	level := logging.LevelInfo
	if g.Verbose {
		level = logging.LevelDebug
	}
	opts := logging.Options{Level: level}
	if runLog {
		opts.RunsRoot = filepath.Join(g.Dist, core.DirLogs, "runs")
	}
	log, err := logging.New(opts)
	if err != nil {
		return nil, nil, fmt.Errorf("open run log under %s: %w", filepath.Join(g.Dist, core.DirLogs), err)
	}

	hermetic := g.Busybox != ""
	if g.hermetic {
		hermetic = true
	}
	if g.noHermetic {
		hermetic = false
	}
	if hermetic && g.Busybox == "" {
		log.Close()
		return nil, nil, fmt.Errorf("--hermetic composes PATH from busybox and the toolchain alone, but no busybox was found on PATH and --busybox was not given.\nInstall busybox, pass --busybox <path>, or build with --no-hermetic and accept the host's tools")
	}

	workers, jobs := defaultParallelism(g.Workers, g.Jobs)
	e := &core.Env{
		Dist:       g.Dist,
		RepoRoot:   g.repoRoot,
		Toolchains: g.Toolchains,
		Overrides:  g.Overrides,
		Busybox:    g.Busybox,
		Qemu:       g.qemuMap(cfg),
		Hermetic:   hermetic,
		Offline:    g.Offline,
		Jobs:       jobs,
		MaxWorkers: workers,
		KeepWork:   g.KeepWork,
		Log:        log,
	}
	return e, func() { log.Close() }, nil
}

// qemuMap resolves a qemu-user binary per target. An explicit --qemu always
// wins; otherwise the binary named by the target row is looked up on PATH,
// which is what makes verification work on a developer's machine without the
// shim having to hand every path in.
func (g *Global) qemuMap(cfg *config.Config) map[string]string {
	out := map[string]string{}
	if cfg != nil {
		for _, name := range sortedKeys(cfg.Targets) {
			t := cfg.Targets[name]
			if p, err := exec.LookPath(ensure.QemuBinaryName(t)); err == nil {
				out[t.Triple] = p
			}
		}
	}
	for k, v := range g.Qemu {
		out[k] = v
	}
	return out
}

// toolchainState is which trees exist for one triple. The two kinds are kept
// apart because they are not interchangeable in both directions: a build for
// some other target falls back from cross to native happily, but pyhost - the
// runnable interpreter a cross build freezes its bytecode with - has to come
// out of a native tree for this machine.
type toolchainState struct {
	Override string
	Cross    string
	Native   string
}

func (s toolchainState) any() string {
	switch {
	case s.Override != "":
		return s.Override
	case s.Cross != "":
		return s.Cross
	}
	return s.Native
}

func (g *Global) toolchainState(triple string) toolchainState {
	var st toolchainState
	if dir, ok := g.Overrides[triple]; ok && isDir(dir) {
		st.Override = dir
		return st
	}
	if g.Toolchains == "" {
		return st
	}
	if dir := filepath.Join(g.Toolchains, triple+"-"+core.KindCross); isDir(dir) {
		st.Cross = dir
	}
	if dir := filepath.Join(g.Toolchains, triple+"-"+core.KindNative); isDir(dir) {
		st.Native = dir
	}
	return st
}

// toolchainMissing names the kind that has to be provisioned for triple, or ""
// when what is on disk will do. The kinds are gccfactory's, which is what the
// shim fetches: a toolchain emitting code for the machine it runs on is
// "native", anything else is "cross".
func (g *Global) toolchainMissing(host, triple string) string {
	st := g.toolchainState(triple)
	if st.Override != "" {
		return ""
	}
	if triple == host {
		if st.Native == "" {
			return core.KindNative
		}
		return ""
	}
	if st.Cross == "" && st.Native == "" {
		return core.KindCross
	}
	return ""
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// signalContext cancels on the first SIGINT/SIGTERM and hard-exits on the
// second, so a wedged build is always escapable.
func signalContext() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		fmt.Fprintln(os.Stderr, "\n"+yellow("interrupt: finishing in-flight steps; press Ctrl-C again to abort now"))
		cancel()
		<-ch
		os.Exit(130)
	}()
	return ctx, func() { signal.Stop(ch); cancel() }
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func sortedTriples(cfg *config.Config) []string {
	out := make([]string, 0, len(cfg.Targets))
	for _, t := range cfg.Targets {
		out = append(out, t.Triple)
	}
	sort.Strings(out)
	return out
}
