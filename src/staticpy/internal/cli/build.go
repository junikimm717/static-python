package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/ensure"
	"github.com/junikimm717/static-python/src/staticpy/internal/recipe"
	"github.com/junikimm717/static-python/src/staticpy/internal/tui"
)

var cmdBuild = &command{
	Name:     "build",
	Short:    "build a static interpreter for one or more targets",
	Synopsis: "staticpy build [--target TRIPLE]... [--profile NAME] [--bundle NAME] [--verify LEVEL] [--pack] [--dry-run]",
	Long: `Resolves the requested interpreters into a job DAG and builds it. With no
--target the build is for this machine's own triple, which is the only case
that needs no cross toolchain and the only case where PGO can train against the
interpreter it is optimizing.

On a terminal, no --target instead opens a short wizard asking for whatever
the command line left open - targets, profile, verification, packing - and
skipping whatever it did not; targets this machine cannot build are shown with
the reason rather than hidden. Every menu names the flag it stands in for, and
the finished wizard prints the equivalent command line, which is also how to
skip it. Without a terminal (CI, a pipe, TERM=dumb, STATICPY_NO_TUI set), and
under --dry-run or --json, nothing is asked: no --target keeps meaning this
machine's own triple with the flag defaults, exactly as scripts expect.

Everything is content-addressed. A job is rebuilt only when its key changes -
its sources, its flags, its triples, and its dependencies' keys - so re-running
after an interrupted build resumes rather than restarts, and two staticpy
processes may share one dist/ safely.

Cross builds depend on a static host interpreter of the same version (job
` + "`pyhost`" + `), never on the host's shipped interpreter: freezing bytecode needs a
runnable Python that agrees exactly with the one being built.

FLAGS
  --verify LEVEL  run verification as part of the build, so a broken interpreter
                  never becomes a published artifact.
                    smoke  import probes only; seconds, and it gates every target
                    core   the curated subset: the language core plus every
                           extension module staticpy links in by hand
                    full   CPython's whole test suite
  --pack          also produce the distributable tarball for each target.
                    Host-built trees are $ORIGIN-relocatable; they still
                    need a compatible glibc on the machine that unpacks them.
  --bundle NAME   Python packages to compile in, from bundles.toml. Overrides
                  whatever the profile selects; a static interpreter cannot
                  dlopen an extension, so this is the only way one gets in.
  --dry-run       print the plan and what each job's state is, build nothing

Progress goes to the terminal; every command's full output goes to
dist/logs/jobs/<slug>/latest/ whether or not -v is given. When a job fails, the
error names the log file and ` + "`staticpy shell <slug>`" + ` re-enters its environment.`,
	Run: runBuild,
}

func runBuild(g *Global, args []string) error {
	fs := g.flagSet("build")
	verify := fs.String("verify", "", "verification level to run as part of the build: smoke|core|full")
	pack := fs.Bool("pack", false, "also produce the distributable tarball")
	bundle := fs.String("bundle", "", "python package bundle to compile in, overriding the profile's")
	dryRun := fs.Bool("dry-run", false, "print the plan without building anything")
	if err := parse(fs, args); err != nil {
		return finish("build", err)
	}
	g.noteGiven(fs)
	if *verify != "" {
		if _, err := ensure.ParseLevel(*verify); err != nil {
			return usagef("%v", err)
		}
	}

	// --dry-run and --json are script surfaces, so they keep the
	// non-interactive meaning of no --target: this machine's own triple.
	if len(g.Targets) == 0 && !*dryRun && !g.JSON && tui.Interactive() {
		err := runBuildWizard(g, buildOpts{verify: verify, pack: pack, bundle: bundle})
		switch {
		case errors.Is(err, tui.ErrAborted):
			fmt.Fprintln(os.Stderr, dim("aborted; nothing was built"))
			return nil
		case errors.Is(err, tui.ErrNotInteractive):
			// The terminal went away between the check and the prompt; fall
			// through to the host-triple default.
		case err != nil:
			return err
		}
	}

	s, err := g.session(recipe.PlanOptions{Verify: *verify, Pack: *pack, Bundle: *bundle}, !*dryRun)
	if err != nil {
		return err
	}
	defer s.close()

	nodes, err := core.Plan(s.e, s.jobs)
	if err != nil {
		return err
	}
	if *dryRun {
		return s.printPlan(nodes, "would build")
	}

	todo := 0
	for _, n := range nodes {
		if nodeState(s.e, n) != stateOK {
			todo++
		}
	}
	fmt.Fprintf(os.Stderr, "%s %s -> %s   profile %s   %d of %d job%s to build\n",
		bold("build:"), s.host, strings.Join(s.targets, " "), s.g.Profile, todo, len(nodes), plural(len(nodes)))
	fmt.Fprintf(os.Stderr, "%s\n", dim(fmt.Sprintf("dist %s   %d worker%s x make -j%d   logs %s",
		s.e.Dist, s.e.Workers(), plural(s.e.Workers()), s.e.MakeJobs(), s.e.Path(core.DirLogs))))
	if !s.e.Hermetic {
		fmt.Fprintf(os.Stderr, "%s the host PATH is visible to this build (--no-hermetic, or no busybox found); its artifacts are not reproducible elsewhere\n", yellow("warning:"))
	}

	ctx, stop := signalContext()
	defer stop()

	started := time.Now()
	runErr := core.Run(ctx, s.e, s.jobs)

	after, planErr := core.Plan(s.e, s.jobs)
	if planErr == nil {
		printVerifyReports(s.e, after)
	}
	if runErr != nil {
		return runErr
	}
	fmt.Fprintf(os.Stderr, "\n%s %s in %s\n", green("built"), strings.Join(s.targets, " "), humanDur(time.Since(started)))
	if planErr == nil {
		printArtifacts(s.e, s.jobs, after)
	}
	return nil
}

// Every command that touches jobs goes through session, so none of them ever
// constructs a job itself.
type session struct {
	g       *Global
	cfg     *config.Config
	e       *core.Env
	close   func()
	host    string
	targets []string
	jobs    []core.Job
	kit     string
}

func (g *Global) session(o recipe.PlanOptions, runLog bool) (*session, error) {
	cfg, err := g.load()
	if err != nil {
		return nil, err
	}
	host, err := g.HostTriple(cfg)
	if err != nil {
		return nil, err
	}
	targets, err := g.selectTargets(cfg, host)
	if err != nil {
		return nil, err
	}
	e, done, err := g.newEnv(cfg, runLog)
	if err != nil {
		return nil, err
	}
	// A caller that already named a profile is asking for that one specifically,
	// as `bench --interp reference` does while --profile still selects what the
	// static arm is built from.
	if o.Profile == "" {
		o.Profile = g.Profile
	}
	o.Host = host
	o.Targets = targets
	// Job keys have to include the toolchain's identity, and KeyInputs sees no
	// Env; binding it here is how that reaches job construction.
	recipe.Bind(e)
	jobs, err := recipe.Plan(cfg, patchTree{cfg}, o)
	if err != nil {
		done()
		if strings.Contains(err.Error(), "toolchain") {
			return nil, fmt.Errorf("%w\n`staticpy doctor` lists what this machine has and which targets are buildable", err)
		}
		return nil, err
	}
	return &session{g: g, cfg: cfg, e: e, close: done, host: host, targets: targets, jobs: jobs, kit: o.Kit}, nil
}

type planRow struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Key      string `json:"key"`
	State    string `json:"state"`
	Artifact string `json:"artifact"`
	Step     string `json:"step,omitempty"`
}

func (s *session) planRows(nodes []core.PlanNode) []planRow {
	out := make([]planRow, 0, len(nodes))
	for _, n := range nodes {
		r := planRow{
			Slug:     n.Job.Slug(),
			Name:     n.Job.Name(),
			Key:      n.Key,
			State:    nodeState(s.e, n),
			Artifact: n.Job.ArtifactDir(s.e),
		}
		if h := liveHeartbeat(s.e, n.Job.Slug()); h != nil {
			r.Step = h.Step
		}
		out = append(out, r)
	}
	return out
}

// printPlan lists the DAG in dependency-first order, which is also build order.
func (s *session) printPlan(nodes []core.PlanNode, verb string) error {
	rows := s.planRows(nodes)
	if s.g.JSON {
		return emitJSON(map[string]any{
			"dist": s.e.Dist, "host": s.host, "targets": s.targets,
			"profile": s.g.Profile, "kit": s.kit, "jobs": rows,
		})
	}
	if s.kit != "" {
		fmt.Printf("%s %s -> %s   kit %s\n", bold("plan:"), s.host, strings.Join(s.targets, " "), s.kit)
	} else {
		fmt.Printf("%s %s -> %s   profile %s\n", bold("plan:"), s.host, strings.Join(s.targets, " "), s.g.Profile)
	}
	fmt.Printf("%s\n\n", dim("dist "+s.e.Dist))

	t := newTable("#", "STATE", "SLUG", "KEY")
	todo := 0
	for i, r := range rows {
		if r.State != stateOK {
			todo++
		}
		t.add(dim(fmt.Sprint(i+1)), colorState(r.State), r.Slug, dim(shortKey(r.Key)))
	}
	t.rightAlign(0).render(os.Stdout)
	fmt.Printf("\n%s %d of %d job%s, in dependency order.\n", verb, todo, len(rows), plural(len(rows)))
	if todo == 0 {
		fmt.Printf("%s\n", dim("everything is up to date; a build would publish nothing new"))
	}
	return nil
}

// Only the root jobs are named, since they are the only ones anybody is going
// to open by hand.
func printArtifacts(e *core.Env, roots []core.Job, nodes []core.PlanNode) {
	valid := map[string]bool{}
	for _, n := range nodes {
		valid[n.Job.Slug()] = n.Valid
	}
	t := newTable("JOB", "ARTIFACT", "SIZE")
	for _, j := range roots {
		if !valid[j.Slug()] {
			continue
		}
		dir := j.ArtifactDir(e)
		t.add(j.Slug(), cyan(dir), humanBytes(dirSize(dir)))
	}
	if t.empty() {
		return
	}
	fmt.Println()
	t.rightAlign(2).render(os.Stdout)
}
