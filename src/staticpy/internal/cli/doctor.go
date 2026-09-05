package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/ensure"
)

var cmdDoctor = &command{
	Name:     "doctor",
	Short:    "check this machine before a build spends an hour finding out",
	Synopsis: "staticpy doctor [--target TRIPLE]... [--provision-plan -- <command line>]",
	Long: `Reports what this machine has and what it is missing, so a missing tool is a
line of output now rather than a failure forty minutes into a build.

WHAT IS CHECKED
  go        only the ./staticpy shim needs it, to rebuild this binary from
            source. A released binary does not.
  perl      OpenSSL's Configure is a perl program. This is the one genuinely
            irreducible host dependency: everything else staticpy needs it
            either builds or is handed by the shim, but no toolchain ships a
            perl.
  patch     applies the pinned diffs to the source trees.
  toolchain one <triple>-cross or <triple>-native tree per target, under
            --toolchains. staticpy never fetches these; the shim does.
  qemu      needed to RUN a non-native target's binaries, so only verification
            depends on it. An explicit --qemu <triple>=<path> wins, otherwise
            the binary named in targets.toml is looked up on PATH.

The verdict per target is therefore two-sided: buildable (a toolchain resolves)
and runnable (native, or a qemu resolves). A target can be perfectly buildable
and unverifiable on the same machine.

Exit status is non-zero when something a build actually needs is missing.

--provision-plan
  Machine-readable output for the ./staticpy shim, which runs it before every
  invocation to decide what to download. It prints one line per MISSING
  toolchain as "<triple>\t<kind>" and nothing else, and exits 0 whether or not
  anything is missing - including when no toolchain is present at all, which is
  the case it exists for. The real command line is passed after a "--" so the
  plan covers exactly the targets that invocation needs:

      staticpy doctor --provision-plan --toolchains DIR -- build --target aarch64-linux-musl`,
	Run: runDoctor,
}

func runDoctor(g *Global, args []string) error {
	left, invocation, hasSep := splitDoubleDash(args)
	fs := g.flagSet("doctor")
	provision := fs.Bool("provision-plan", false, "print the missing toolchains for the command line after --, for the shim")
	if err := parse(fs, left); err != nil {
		return finish("doctor", err)
	}
	if *provision {
		printProvisionPlan(g, invocation)
		return nil
	}
	if hasSep {
		return usagef("a `--` separator is only meaningful with --provision-plan")
	}
	return doctorReport(g)
}

func splitDoubleDash(args []string) (before, after []string, found bool) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:], true
		}
	}
	return args, nil, false
}

type toolCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	// Required marks a check whose failure will stop a build.
	Required bool `json:"required"`
}

type targetCheck struct {
	Triple string `json:"triple"`
	Status string `json:"status"`
	Cross  string `json:"cross,omitempty"`
	Native string `json:"native,omitempty"`
	// Missing is the kind that still has to be provisioned, "" when none.
	Missing   string `json:"missing,omitempty"`
	IsHost    bool   `json:"is_host"`
	Buildable bool   `json:"buildable"`
	Runner    string `json:"runner"`
	Qemu      string `json:"qemu"`
	Runnable  bool   `json:"runnable"`
}

func doctorReport(g *Global) error {
	cfg, err := g.load()
	if err != nil {
		return err
	}
	host, hostErr := g.HostTriple(cfg)
	targets, terr := g.selectTargets(cfg, host)
	if terr != nil {
		return terr
	}
	if hostErr == nil {
		targets = withHost(targets, host)
	}

	tools := []toolCheck{
		lookupTool("go", false, "only the ./staticpy shim needs it, to rebuild this binary"),
		lookupTool("perl", true, "OpenSSL's Configure is written in perl; nothing else can supply it"),
		lookupTool("patch", true, "applies the pinned diffs to each source tree"),
	}

	tcRoot := toolCheck{Name: "toolchains", OK: g.Toolchains != "" && isDir(g.Toolchains), Detail: g.Toolchains}
	switch {
	case g.Toolchains == "":
		tcRoot.Detail = "no --toolchains directory given; the ./staticpy shim passes dist/toolchains"
	case !tcRoot.OK:
		tcRoot.Detail = g.Toolchains + " does not exist yet; the shim creates and fills it"
	default:
		tcRoot.Detail = fmt.Sprintf("%s (%s)", g.Toolchains, describeToolchains(g.Toolchains))
	}
	tools = append(tools, tcRoot)

	var rows []targetCheck
	for _, name := range targets {
		t := cfg.Targets[name]
		st := g.toolchainState(t.Triple)
		row := targetCheck{
			Triple: t.Triple, Status: t.Status,
			Cross: st.Cross, Native: st.Native,
			Missing:   g.toolchainMissing(host, t.Triple),
			IsHost:    t.Triple == host,
			Buildable: st.any() != "",
		}
		if st.Override != "" {
			row.Cross, row.Native = st.Override, st.Override
		}
		if row.Missing != "" {
			row.Buildable = false
		}
		if ensure.IsNativeTarget(t) {
			row.Runner, row.Runnable = ensure.RunnerNative, true
		} else {
			row.Runner = ensure.RunnerQemu
			if p, ok := g.qemuMap(cfg)[t.Triple]; ok {
				row.Qemu, row.Runnable = p, true
			} else {
				row.Qemu = ensure.QemuBinaryName(t) + " (not found)"
			}
		}
		rows = append(rows, row)
	}

	if g.JSON {
		out := map[string]any{
			"dist": g.Dist, "toolchains": g.Toolchains,
			"tools": tools, "targets": rows,
		}
		if hostErr != nil {
			out["host_error"] = hostErr.Error()
		} else {
			out["host"] = host
		}
		if err := emitJSON(out); err != nil {
			return err
		}
		return doctorVerdict(tools, rows, hostErr, true)
	}

	fmt.Printf("%s %s\n", bold("staticpy doctor"), dim(g.Dist))
	if hostErr != nil {
		fmt.Printf("  %s %v\n", red("host:"), hostErr)
	} else {
		fmt.Printf("  %s %s\n", dim("build host:"), host)
	}
	fmt.Println()

	t := newTable("CHECK", "", "DETAIL")
	for _, c := range tools {
		t.add(c.Name, mark(c.OK, c.Required), c.Detail)
	}
	t.render(os.Stdout)

	fmt.Printf("\n%s\n", bold("TARGETS"))
	tt := newTable("TRIPLE", "", "CROSS", "NATIVE", "", "RUN")
	for _, r := range rows {
		run := r.Runner
		if r.Runner == ensure.RunnerQemu {
			run = "qemu " + r.Qemu
		}
		label := r.Triple
		if r.IsHost {
			label += dim(" (host)")
		}
		if r.Status != "proven" {
			label += dim(" (" + r.Status + ")")
		}
		tt.add(label, mark(r.Buildable, true), have(r.Cross), have(r.Native), mark(r.Runnable, false), run)
	}
	tt.render(os.Stdout)
	for _, r := range rows {
		if r.Missing != "" {
			fmt.Printf("  %s\n", dim(toolchainDetail(g, r)))
		}
	}
	fmt.Println()
	return doctorVerdict(tools, rows, hostErr, false)
}

// Which kind a target needs is not obvious for the host: a native tree there
// is what pyhost is built with.
func toolchainDetail(g *Global, r targetCheck) string {
	want := r.Triple + "-" + r.Missing
	why := ""
	if r.IsHost && r.Missing == core.KindNative {
		why = " The host needs the native tree specifically: pyhost, the runnable interpreter a cross build freezes its bytecode with, is built from it, and a cross tree carrying the same triple is the wrong compiler."
	}
	if g.Toolchains == "" && len(g.Overrides) == 0 {
		return want + " is not available: no --toolchains directory was given, and the ./staticpy shim provisions it." + why
	}
	return "missing " + filepath.Join(g.Toolchains, want) + "; the ./staticpy shim provisions it." + why
}

func have(dir string) string {
	if dir == "" {
		return dim("-")
	}
	return dim(dir)
}

func doctorVerdict(tools []toolCheck, rows []targetCheck, hostErr error, quiet bool) error {
	var missing []string
	for _, c := range tools {
		if c.Required && !c.OK {
			missing = append(missing, c.Name)
		}
	}
	var buildable, unbuildable []string
	for _, r := range rows {
		if r.Buildable {
			buildable = append(buildable, r.Triple)
		} else {
			unbuildable = append(unbuildable, r.Triple)
		}
	}
	bad := len(missing) > 0 || hostErr != nil || len(buildable) == 0
	if quiet {
		if bad {
			return quietExit{1}
		}
		return nil
	}
	if len(missing) > 0 {
		fmt.Printf("%s %s missing. Install %s and re-run; a build cannot get past OpenSSL or the source patches without them.\n",
			red("blocked:"), strings.Join(missing, " and "), strings.Join(missing, ", "))
	}
	switch {
	case len(buildable) == 0:
		fmt.Printf("%s no target has a toolchain yet. Run through the ./staticpy shim and it will fetch them, or pass --toolchains <dir> / --toolchain <triple>=<path>.\n", yellow("note:"))
	case len(unbuildable) > 0:
		fmt.Printf("%s %s\n%s   %s\n", green("buildable:"), strings.Join(buildable, " "),
			dim("missing:"), strings.Join(unbuildable, " "))
	default:
		fmt.Printf("%s %s\n", green("buildable:"), strings.Join(buildable, " "))
	}
	if bad {
		return quietExit{1}
	}
	return nil
}

func mark(ok, required bool) string {
	if ok {
		return green("+")
	}
	if required {
		return red("x")
	}
	return yellow("-")
}

func lookupTool(name string, required bool, why string) toolCheck {
	p, err := exec.LookPath(name)
	if err != nil {
		return toolCheck{Name: name, Required: required, Detail: "not on PATH - " + why}
	}
	return toolCheck{Name: name, OK: true, Required: required, Detail: p}
}

func describeToolchains(dir string) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	var names []string
	for _, ent := range ents {
		if ent.IsDir() && (strings.HasSuffix(ent.Name(), "-"+core.KindCross) || strings.HasSuffix(ent.Name(), "-"+core.KindNative)) {
			names = append(names, ent.Name())
		}
	}
	if len(names) == 0 {
		return "no toolchains unpacked here yet"
	}
	sort.Strings(names)
	return fmt.Sprintf("%d present: %s", len(names), strings.Join(names, ", "))
}

func withHost(targets []string, host string) []string {
	for _, t := range targets {
		if t == host {
			return targets
		}
	}
	// A cross build still needs the host's own toolchain: pyhost, the runnable
	// interpreter that freezes the bytecode, is built for this machine.
	return append([]string{host}, targets...)
}

// printProvisionPlan writes the shim's contract: one "<triple>\t<kind>" line
// per missing toolchain on stdout, nothing else, exit 0 regardless. It must
// work with no toolchain, no dist/ and no network, since it is what decides
// whether any of those get created.
func printProvisionPlan(g *Global, invocation []string) {
	inv := scanInvocation(invocation)
	if !inv.needsToolchains() {
		return
	}
	if inv.toolchains != "" {
		g.Toolchains = inv.toolchains
	}
	for k, v := range inv.overrides {
		g.Overrides[k] = v
	}
	if inv.host != "" {
		g.Host = inv.host
	}
	if err := g.resolve(); err != nil {
		return
	}
	cfg, err := config.Load(config.Options{RepoRoot: g.repoRoot, Dir: inv.configDir, SourcesDir: inv.sourcesDir})
	if err != nil {
		return
	}
	host, err := g.HostTriple(cfg)
	if err != nil {
		return
	}
	targets := inv.targets
	if len(targets) == 0 {
		targets = []string{host}
	}
	g.Targets = targets
	resolved, err := g.selectTargets(cfg, host)
	if err != nil {
		return
	}
	for _, triple := range withHost(resolved, host) {
		t, ok := cfg.Targets[triple]
		if !ok {
			continue
		}
		if kind := g.toolchainMissing(host, t.Triple); kind != "" {
			fmt.Printf("%s\t%s\n", t.Triple, kind)
		}
	}
}

// invocation is what a lenient scan can recover from a command line this
// binary has not parsed yet. It cannot use the flag package: the trailing
// arguments carry subcommand flags a global FlagSet would reject outright, and
// refusing to answer would leave the shim unable to provision anything.
type invocation struct {
	command    string
	targets    []string
	host       string
	toolchains string
	configDir  string
	sourcesDir string
	overrides  map[string]string
}

// needsToolchains keeps the shim from downloading a compiler because someone
// asked to read a log. Only the commands that compile or execute target code
// are worth provisioning for - and doctor is deliberately not one of them, or
// it could never report a toolchain as missing.
func (i invocation) needsToolchains() bool {
	return i.command == "build" || i.command == "verify"
}

func scanInvocation(args []string) invocation {
	inv := invocation{overrides: map[string]string{}}
	// Flags that consume the following argument, so their value is never
	// mistaken for the subcommand name.
	valued := map[string]bool{
		"dist": true, "config": true, "sources": true, "toolchains": true,
		"toolchain": true, "qemu": true, "profile": true,
		"host": true, "target": true, "workers": true, "j": true,
		"color": true, "verify": true, "level": true, "bundle": true,
		"step": true, "n": true, "scope": true,
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			continue
		}
		if !strings.HasPrefix(a, "-") {
			if inv.command == "" {
				inv.command = a
			}
			continue
		}
		name := strings.TrimLeft(a, "-")
		value := ""
		if k, v, ok := strings.Cut(name, "="); ok {
			name, value = k, v
		} else if valued[name] && i+1 < len(args) {
			i++
			value = args[i]
		}
		switch name {
		case "target":
			for _, p := range strings.Split(value, ",") {
				if p = strings.TrimSpace(p); p != "" {
					inv.targets = append(inv.targets, p)
				}
			}
		case "host":
			inv.host = value
		case "toolchains":
			inv.toolchains = value
		case "config":
			inv.configDir = value
		case "sources":
			inv.sourcesDir = value
		case "toolchain":
			if k, v, ok := strings.Cut(value, "="); ok && k != "" && v != "" {
				if abs, err := filepath.Abs(v); err == nil {
					v = abs
				}
				inv.overrides[k] = v
			}
		}
	}
	return inv
}
