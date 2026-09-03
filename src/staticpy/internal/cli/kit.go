package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/ensure"
	"github.com/junikimm717/static-python/src/staticpy/internal/recipe"
)

var cmdKit = &command{
	Name:     "kit",
	Short:    "pack several interpreters and a bench runner into one tarball",
	Synopsis: "staticpy kit [--name NAME] [--verify LEVEL] [--dry-run]",
	Long: `Builds every arm of a named kit, verifies if asked, and writes one
tarball a quiet machine can unzip and measure without a git checkout.

A kit is not one Python. It is the comparison set: relocatable prefixes under
python/<profile>/, vendored pyperformance/pyperf sdists, kit.json naming the
lineup, and bin/staticpy-bench. ./run on the quiet box is:

  ./run                 every arm, kit baseline
  ./run --cpu 3         pin (sched_setaffinity, not isolcpus)
  ./run --list          show arms
  ./run --suite micro

The tarball lands in dist/out/kit/<name>/<triple>/. Per-profile pack
tarballs stay the "I just want one Python" artifact.

Native only, same reason as bench: a host-built reference arm cannot be
cross-compiled, and measuring under qemu would measure qemu.

FLAGS
  --name NAME     kit from bench.toml (default "default"). "smoke" is
                  default vs reference.
  --verify LEVEL  run verification on every arm before packing, as build
                  would. A broken interpreter is never kitted.
  --dry-run       print the plan, write nothing`,
	Run: runKit,
}

func runKit(g *Global, args []string) error {
	fs := g.flagSet("kit")
	name := fs.String("name", "default", "kit name from bench.toml")
	verify := fs.String("verify", "", "verification level to run as part of the kit: smoke|core|full")
	dryRun := fs.Bool("dry-run", false, "print the plan without building anything")
	if err := parse(fs, args); err != nil {
		return finish("kit", err)
	}
	if *verify != "" {
		if _, err := ensure.ParseLevel(*verify); err != nil {
			return usagef("%v", err)
		}
	}

	cfg, err := g.load()
	if err != nil {
		return err
	}
	if _, ok := cfg.Kits[*name]; !ok {
		return usagef("kit %q is not in bench.toml (have %s)", *name, strings.Join(sortedKeys(cfg.Kits), ", "))
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
		return fmt.Errorf("kit is native-only, same as bench: this machine is %s, got --target %s.\nA host-built reference arm cannot be cross-compiled",
			host, strings.Join(targets, " "))
	}

	s, err := g.session(recipe.PlanOptions{Kit: *name, Verify: *verify}, !*dryRun)
	if err != nil {
		return err
	}
	defer s.close()

	nodes, err := core.Plan(s.e, s.jobs)
	if err != nil {
		return err
	}
	if *dryRun {
		return s.printPlan(nodes, "would kit")
	}

	todo := 0
	for _, n := range nodes {
		if nodeState(s.e, n) != stateOK {
			todo++
		}
	}
	fmt.Fprintf(os.Stderr, "%s %s   kit %s   %d of %d job%s to build\n",
		bold("kit:"), host, *name, todo, len(nodes), plural(len(nodes)))
	fmt.Fprintf(os.Stderr, "%s\n", dim(fmt.Sprintf("dist %s   %d worker%s x make -j%d   logs %s",
		s.e.Dist, s.e.Workers(), plural(s.e.Workers()), s.e.MakeJobs(), s.e.Path(core.DirLogs))))

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
	fmt.Fprintf(os.Stderr, "\n%s kit %s in %s\n", green("packed"), *name, humanDur(time.Since(started)))
	if planErr == nil {
		printArtifacts(s.e, s.jobs, after)
	}
	return nil
}
