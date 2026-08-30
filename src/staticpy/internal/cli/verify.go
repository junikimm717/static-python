package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/ensure"
	"github.com/junikimm717/static-python/src/staticpy/internal/recipe"
)

var cmdVerify = &command{
	Name:     "verify",
	Short:    "prove an already-built interpreter is what it claims to be",
	Synopsis: "staticpy verify [--level smoke|core|full] [--target TRIPLE]... [--build]",
	Long: `Runs the verification suite against interpreters that are already built:
statically linked, ELF header matching the target, every promised module
importable, and CPython's own tests in agreement with tests.toml.

LEVELS
  smoke  import probes only. Seconds. This is the gate every target must pass.
  core   the language core, the containers and numerics where a miscompiled
         target shows up first, and every extension module staticpy links in by
         hand - those are what a wrong _sysconfigdata or a half-linked library
         breaks.
  full   CPython's entire suite. Hours under qemu.

A non-native target is run under qemu-user. staticpy does not fetch qemu: an
explicit --qemu <triple>=<path> wins, otherwise the binary named by the target
row is looked up on PATH. ` + "`staticpy doctor`" + ` says which targets resolved.

By default this refuses to build anything: it is the command you reach for when
you want to know whether what is on disk is good, not to start an hour of work.
Pass --build to let it build what is missing first.

Verification is content-addressed like everything else, so re-verifying an
unchanged interpreter at the same level reports the stored result rather than
running the suite again. Its report is kept as report.json inside the verify
job's artifact, so a CI run keeps the evidence after the terminal output is
gone. Exit status is non-zero when any check failed.`,
	Run: runVerify,
}

func runVerify(g *Global, args []string) error {
	fs := g.flagSet("verify")
	level := fs.String("level", string(ensure.LevelSmoke), "how much to run: smoke|core|full")
	build := fs.Bool("build", false, "build whatever is missing instead of refusing")
	if err := parse(fs, args); err != nil {
		return finish("verify", err)
	}
	lv, err := ensure.ParseLevel(*level)
	if err != nil {
		return usagef("%v", err)
	}

	s, err := g.session(recipe.PlanOptions{Verify: string(lv)}, true)
	if err != nil {
		return err
	}
	defer s.close()

	nodes, err := core.Plan(s.e, s.jobs)
	if err != nil {
		return err
	}
	if !*build {
		var missing []string
		for _, n := range nodes {
			if n.Job.Name() == "verify" || n.Valid || n.Building {
				continue
			}
			missing = append(missing, n.Job.Slug())
		}
		if len(missing) > 0 {
			return fmt.Errorf("there is nothing to verify yet: %d job%s would have to be built first, starting with %s.\nRun `staticpy build --target %s` first, or pass --build to do it now",
				len(missing), plural(len(missing)), missing[0], strings.Join(s.targets, " --target "))
		}
	}

	ctx, stop := signalContext()
	defer stop()

	runErr := core.Run(ctx, s.e, s.jobs)
	after, planErr := core.Plan(s.e, s.jobs)
	if planErr == nil {
		nodes = after
	}
	failed := printVerifyReports(s.e, nodes)
	if runErr != nil {
		if failed {
			// The report above already lists every failed check by name; a
			// second rendering of the same failure as an error line adds noise
			// and nothing else.
			return quietExit{1}
		}
		return runErr
	}
	if failed {
		return quietExit{1}
	}
	return nil
}

// The Report type marshals but does not unmarshal, so the stored evidence is
// read back into this shape and handed to the same renderer.
type verifyReport struct {
	Subject string `json:"subject"`
	OK      bool   `json:"ok"`
	Ms      int64  `json:"ms"`
	Checks  []struct {
		ensure.Check
		Error string `json:"error"`
		Ms    int64  `json:"ms"`
	} `json:"checks"`
}

func printVerifyReports(e *core.Env, nodes []core.PlanNode) bool {
	anyFail := false
	for _, n := range nodes {
		if n.Job.Name() != "verify" {
			continue
		}
		// A stale artifact survives a failed rebuild; printing its report would
		// announce a pass for a binary that is gone, right underneath the error
		// saying the job failed.
		if !n.Valid {
			fmt.Fprintf(os.Stderr, "%s %s: the report on disk is from an earlier build, so it is not shown\n",
				yellow("warning:"), n.Job.Slug())
			continue
		}
		path := filepath.Join(n.Job.ArtifactDir(e), ensure.ReportName)
		rep, err := readVerifyReport(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(os.Stderr, "%s %s: %v\n", yellow("warning:"), n.Job.Slug(), err)
			}
			continue
		}
		fmt.Println()
		fmt.Print(rep.String())
		if !rep.OK() {
			anyFail = true
		}
	}
	return anyFail
}

func readVerifyReport(path string) (*ensure.Report, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw verifyReport
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	rep := ensure.NewReport(raw.Subject)
	rep.Dur = time.Duration(raw.Ms) * time.Millisecond
	for _, c := range raw.Checks {
		chk := c.Check
		chk.Dur = time.Duration(c.Ms) * time.Millisecond
		if c.Error != "" {
			chk.Err = errors.New(c.Error)
		}
		rep.Add(chk)
	}
	return rep, nil
}
