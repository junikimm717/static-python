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
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// runPyperfSuite drives the pyperformance suite across the selected arms and
// writes a session directory that can be re-read long after the run.
func runPyperfSuite(e *core.Env, order []string, paths map[string]string,
	baseline, suiteRoot, pyperfHint string, useVenv bool, noPin bool, cpu int, timeout time.Duration) error {

	suite, skipped, err := bench.DiscoverSuite(suiteRoot)
	if err != nil {
		return err
	}
	pin, topo := choosePin(noPin, cpu)

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
	arms := make([]bench.Arm, 0, len(order))
	for _, label := range order {
		id, err := bench.Identify(label, paths[label])
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		ids = append(ids, id)

		a := bench.Arm{Label: label, Python: paths[label]}
		if useVenv {
			runner.Step("venv-" + label)
			pyperfSrc, err := bench.FindPyperf(pyperfHint)
			if err != nil {
				return err
			}
			v, err := bench.MakeVenv(ctx, runner, label, paths[label],
				filepath.Join(sess.Dir, "venv"), pyperfSrc)
			if err != nil {
				return err
			}
			a.Python, a.Env = v.Python, v.Env()
		}
		arms = append(arms, a)
	}

	if err := sess.WriteJSON("manifest.json", map[string]any{
		"stamp": sess.Stamp, "baseline": baseline, "suite_root": suiteRoot,
		"venv": useVenv, "interpreters": ids, "skipped": skipped,
	}); err != nil {
		return err
	}
	if err := sess.WriteJSON("env.json", map[string]any{
		"topology": describeTopo(topo), "affinity": pin.Describe(),
		"benchmarks_found": len(suite.Cases), "benchmarks_skipped": len(skipped),
		"machine": gatherBenchEnv(),
	}); err != nil {
		return err
	}
	if len(skipped) > 0 {
		if err := sess.WriteJSON("skipped.json", skipped); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s %d benchmarks need wheels this interpreter cannot install; see skipped.json\n",
			yellow("note:"), len(skipped))
	}

	runner.Step("measure")
	fmt.Fprintf(os.Stderr, "%s %d benchmarks x %d interpreters, interleaved\n",
		bold("running:"), len(suite.Cases), len(arms))
	res, err := bench.RunSuite(ctx, runner, sess, pin, arms, suite.Cases, timeout)
	if err != nil {
		return err
	}

	rows, geo := bench.Compare(res, baseline, order)
	if err := sess.WriteJSON("report.json", map[string]any{
		"baseline": baseline, "rows": rows, "geomean_vs_baseline": geo,
	}); err != nil {
		return err
	}
	md := renderSuiteReport(baseline, order, rows, geo, pin, topo)
	if err := os.WriteFile(filepath.Join(sess.Dir, "report.md"), []byte(md), 0o644); err != nil {
		return err
	}
	fmt.Print(md)
	fmt.Fprintf(os.Stderr, "\n%s %s\n", bold("session:"), sess.Dir)
	return nil
}

func describeTopo(t *bench.Topology) string {
	if t == nil {
		return "unknown"
	}
	return t.Describe()
}

func renderSuiteReport(baseline string, order []string, rows []bench.Row,
	geo map[string]float64, pin bench.Pin, topo *bench.Topology) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# pyperformance comparison\n\n")
	fmt.Fprintf(&b, "- baseline: %s\n- %s\n- %s\n- rows: %d\n\n",
		baseline, describeTopo(topo), pin.Describe(), len(rows))

	fmt.Fprintf(&b, "| benchmark |")
	for _, a := range order {
		fmt.Fprintf(&b, " %s |", a)
	}
	fmt.Fprintf(&b, "\n|---|")
	for range order {
		fmt.Fprintf(&b, "---:|")
	}
	b.WriteString("\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s |", r.Benchmark)
		for _, a := range order {
			if v, ok := r.Ratio[a]; ok {
				fmt.Fprintf(&b, " %.2fx |", v)
			} else {
				b.WriteString(" - |")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("\nGeomean vs baseline (>1 is faster):\n\n")
	for _, a := range order {
		if a == baseline {
			continue
		}
		fmt.Fprintf(&b, "- %s: %.3fx\n", a, geo[a])
	}
	return b.String()
}
