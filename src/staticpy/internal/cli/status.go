package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/ensure"
	"github.com/junikimm717/static-python/src/staticpy/internal/recipe"
)

var cmdStatus = &command{
	Name:     "status",
	Short:    "what exists, what is stale, what is building right now",
	Synopsis: "staticpy status [--target TRIPLE]... [--todo] [--verify LEVEL] [--pack]",
	Long: `Answers the question "what would a build actually do?". It resolves exactly the
plan ` + "`staticpy build`" + ` would resolve for the same flags, then reports each job's
state against dist/.

  ok        the artifact exists and its content key matches; a build skips it
  stale     an artifact exists but its inputs changed; a build replaces it
  building  something holds a live heartbeat for it, in this process or in
            another one sharing this dist/ - possibly on another machine
  missing   nothing published yet

Because the state is a key comparison and not a timestamp, "stale" here means
the recipe really did change: a flag, a pinned version, a patch, or a dependency
that itself changed. Pass the same --verify/--pack you would pass to build, or
the plan you are looking at is not the plan you would run.

Safe at any time, including mid-build and before anything has ever been built.

FLAGS
  --todo    list only the jobs that are not up to date
  --verify  include verification jobs at this level, as build would
  --pack    include the packaging jobs, as build would`,
	Run: runStatus,
}

func runStatus(g *Global, args []string) error {
	fs := g.flagSet("status")
	todoOnly := fs.Bool("todo", false, "list only jobs that are not up to date")
	verify := fs.String("verify", "", "include verification jobs at this level: smoke|core|full")
	pack := fs.Bool("pack", false, "include the packaging jobs")
	if err := parse(fs, args); err != nil {
		return finish("status", err)
	}
	if *verify != "" {
		if _, err := ensure.ParseLevel(*verify); err != nil {
			return usagef("%v", err)
		}
	}

	s, err := g.session(recipe.PlanOptions{Verify: *verify, Pack: *pack}, false)
	if err != nil {
		return err
	}
	defer s.close()

	nodes, err := core.Plan(s.e, s.jobs)
	if err != nil {
		return err
	}
	rows := s.planRows(nodes)
	if g.JSON {
		return emitJSON(map[string]any{
			"dist": s.e.Dist, "host": s.host, "targets": s.targets,
			"profile": s.g.Profile, "jobs": rows,
		})
	}

	counts := map[string]int{}
	for _, r := range rows {
		counts[r.State]++
	}
	fmt.Printf("%s %s\n", bold("dist:"), s.e.Dist)
	fmt.Printf("%s\n", dim(fmt.Sprintf("host %s   targets %s   profile %s",
		s.host, strings.Join(s.targets, " "), s.g.Profile)))
	fmt.Printf("  %s %d   %s %d   %s %d   %s %d   (of %d job%s)\n\n",
		green(stateOK), counts[stateOK], yellow(stateStale), counts[stateStale],
		blue(stateBuilding), counts[stateBuilding], dim(stateMissing), counts[stateMissing],
		len(rows), plural(len(rows)))

	t := newTable("STATE", "SLUG", "KEY", "SIZE", "BUILT", "BY")
	for i, r := range rows {
		if *todoOnly && r.State == stateOK {
			continue
		}
		n := nodes[i]
		size, built, by := "-", "-", "-"
		if r.State == stateOK || r.State == stateStale {
			size = humanBytes(dirSize(r.Artifact))
			if m, ok := artifactManifest(r.Artifact); ok {
				built = humanAgo(m.CompletedAt)
				by = m.BuiltBy
			}
		}
		if h := liveHeartbeat(s.e, n.Job.Slug()); h != nil {
			by = who(h)
			built = h.Step
			if built == "" {
				built = "..."
			}
		}
		t.add(colorState(r.State), r.Slug, dim(shortKey(r.Key)), size, built, dim(by))
	}
	if !t.empty() {
		t.rightAlign(3).render(os.Stdout)
		fmt.Println()
	}

	todo := len(rows) - counts[stateOK]
	switch {
	case len(rows) == 0:
		fmt.Printf("%s\n", dim("the plan is empty; nothing was requested"))
	case todo == 0:
		fmt.Printf("%s everything requested is up to date; `staticpy build` would publish nothing new.\n", green("ok:"))
	default:
		fmt.Printf("%s would build %d of %d job%s.\n", bold("build:"), todo, len(rows), plural(len(rows)))
	}
	if counts[stateBuilding] > 0 {
		fmt.Printf("%s\n", dim("a build is in flight; `staticpy logs <slug> --follow` watches one of its jobs"))
	}
	if counts[stateStale] > 0 {
		fmt.Printf("%s\n", dim("stale means an input changed, not that the artifact is old; `staticpy logs <slug>` shows how it was built last time"))
	}
	return nil
}
