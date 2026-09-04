package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/bench"
	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// runPyperfSuite drives the pyperformance suite across the selected arms and
// writes a session directory that can be re-read long after the run.
func runPyperfSuite(g *Global, cfg *config.Config, e *core.Env, order []string, paths map[string]string,
	baseline, suiteRoot, pyperfHint string, useVenv bool, noPin bool, cpu int, timeout time.Duration, offline bool, pins bench.Pins, outPath string) error {

	var skipped []string
	pin, topo, err := choosePin(noPin, cpu)
	if err != nil {
		return err
	}

	machine := bench.ReadMachine()
	topoDesc := ""
	if topo != nil {
		topoDesc = describeTopo(topo)
	}
	machine.SetRunPlacement(pin.Describe(), topoDesc)

	sess, err := bench.NewSession(e.Dist, runtime.GOARCH, time.Now())
	if err != nil {
		return err
	}
	defer sess.Close()

	runner, err := core.NewRunner(e, "bench:"+sess.Stamp)
	if err != nil {
		return err
	}
	defer runner.Close()

	ctx := context.Background()
	var ids []bench.Identity
	venvs := map[string]*bench.Venv{}
	arms := make([]bench.Arm, 0, len(order))
	for _, label := range order {
		id, err := bench.Identify(label, paths[label])
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		enrichIdentity(g, cfg, e, &id)
		ids = append(ids, id)

		a := bench.Arm{Label: label, Python: paths[label]}
		if useVenv {
			runner.Step("venv-" + label)
			// A copied pyperf tree is now the override, not the requirement:
			// installing pyperformance brings its own, pinned to the version
			// its benchmarks were written against.
			var pyperfSrc string
			if pyperfHint != "" {
				if pyperfSrc, err = bench.FindPyperf(pyperfHint); err != nil {
					return err
				}
			}
			v, err := bench.MakeVenv(ctx, runner, label, paths[label],
				filepath.Join(sess.Dir, "venv"), pyperfSrc)
			if err != nil {
				return err
			}
			a.Python, a.Env = v.Python, v.Env()
			venvs[label] = v
		}
		arms = append(arms, a)
	}

	// pyperformance is the default suite, so with no --pyperformance directory
	// it is installed rather than demanded. The benchmarks are data files, so
	// one arm's copy serves every arm.
	if suiteRoot == "" {
		if !useVenv {
			return fmt.Errorf("--no-venv leaves nowhere to install pyperformance.\n" +
				"Pass --pyperformance DIR to point at a copy on disk, or drop --no-venv")
		}
		runner.Step("install pyperformance")
		all := make([]*bench.Venv, 0, len(order))
		for _, label := range order {
			all = append(all, venvs[label])
		}
		if suiteRoot, err = bench.Bootstrap(ctx, runner, all, offline, pins); err != nil {
			return err
		}
	}
	suite, discovered, err := bench.DiscoverSuite(suiteRoot)
	if err != nil {
		return err
	}
	skipped = append(skipped, discovered...)

	// Each benchmark's dependencies go into every arm, before anything is
	// measured. A benchmark whose requirement will not install is dropped here
	// with the reason, rather than failing mid-run and leaving a geomean over
	// a set nobody chose.
	if useVenv {
		runner.Step("requirements")
		runnable := suite.Cases[:0:0]
		for _, c := range suite.Cases {
			failed := ""
			for _, label := range order {
				if err := bench.InstallRequirements(ctx, runner, venvs[label], c); err != nil {
					failed = label
					break
				}
			}
			if failed == "" {
				runnable = append(runnable, c)
				continue
			}
			skipped = append(skipped, c.Label()+" (dependencies would not install for "+failed+")")
		}
		suite.Cases = runnable
		if len(suite.Cases) == 0 {
			return fmt.Errorf("every benchmark's dependencies failed to install; see skipped.json")
		}
		// After the requirements, never before: --help executes the script, so
		// a benchmark that imports its dependency at module level cannot answer
		// until that dependency is installed.
		runner.Step("detect sub-benchmarks")
		bench.DetectSubBenchmarks(ctx, runner, venvs[order[0]].Python, suite)
	}

	runner.Step("inventory")
	for i, label := range order {
		py := paths[label]
		if v := venvs[label]; v != nil {
			py = v.Python
		}
		pkgs, err := bench.InventoryPackages(ctx, runner, py)
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		ids[i].Packages = pkgs
	}

	// Written before the measurements and again after them. The early copy is
	// what a run killed halfway still leaves behind; the late one is the only
	// place a runtime failure can appear, since it is not known until then.
	writeAccounting := func() error {
		return sess.WriteAccounting(bench.Accounting{
			Baseline:   baseline,
			SuiteName:  bench.SuitePyperformance,
			Pins:       pins,
			Identities: ids,
			Skipped:    skipped,
			Machine:    machine,
			Extra: map[string]any{
				"suite_root":       suiteRoot,
				"venv":             useVenv,
				"benchmarks_found": len(suite.Cases),
			},
		})
	}
	if err := writeAccounting(); err != nil {
		return err
	}

	runner.Step("measure")
	fmt.Fprintf(os.Stderr, "%s %d benchmarks x %d interpreters, interleaved\n",
		bold("running:"), len(suite.Cases), len(arms))
	res, failures, err := bench.RunSuite(ctx, runner, sess, pin, arms, suite.Cases, timeout)
	if err != nil {
		return err
	}
	skipped = append(skipped, summarizeFailures(failures)...)
	if err := writeAccounting(); err != nil {
		return err
	}
	if len(skipped) > 0 {
		fmt.Fprintf(os.Stderr, "%s %d benchmarks missing from the table; see skipped.json\n",
			yellow("note:"), len(skipped))
	}

	rows, geo := bench.Compare(res, baseline, order)
	md, report, err := sess.WriteReports(bench.Reports{
		Accounting: bench.Accounting{
			Baseline:   baseline,
			SuiteName:  bench.SuitePyperformance,
			Pins:       pins,
			Identities: ids,
			Skipped:    skipped,
			Machine:    machine,
			Extra: map[string]any{
				"suite_root":       suiteRoot,
				"venv":             useVenv,
				"benchmarks_found": len(suite.Cases),
			},
		},
		Order:   order,
		Rows:    rows,
		Geomean: geo,
	})
	if err != nil {
		return err
	}
	return publishSession(sess, md, report, outPath, g.JSON)
}

// One line per benchmark, naming every arm that lost it. Which arms failed is
// the part that matters: all of them is a suite problem, one of them is a
// difference between the interpreters under test.
func summarizeFailures(fs []bench.Failure) []string {
	var order []string
	arms := map[string][]string{}
	reason := map[string]string{}
	for _, f := range fs {
		if _, seen := arms[f.Benchmark]; !seen {
			order = append(order, f.Benchmark)
		}
		arms[f.Benchmark] = append(arms[f.Benchmark], f.Arm)
		if reason[f.Benchmark] == "" {
			reason[f.Benchmark] = f.Reason
		}
	}
	out := make([]string, 0, len(order))
	for _, b := range order {
		out = append(out, fmt.Sprintf("%s (failed at runtime on %s: %s)",
			b, strings.Join(arms[b], ", "), reason[b]))
	}
	return out
}

func describeTopo(t *bench.Topology) string {
	if t == nil {
		return "unknown"
	}
	return t.Describe()
}
