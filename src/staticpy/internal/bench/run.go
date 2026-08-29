package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// Arm is one interpreter under comparison.
type Arm struct {
	Label  string
	Python string
	Env    map[string]string
}

// Results are keyed arm -> benchmark -> samples.
type Results map[string]map[string][]float64

// RunSuite measures every case on every arm, interleaved.
//
// Interleaving is per benchmark rather than per arm so that machine drift over
// a long run hits all arms alike and cancels in the ratio. Running one arm to
// completion and then the next folds hours of drift straight into the result.
func RunSuite(ctx context.Context, x Exec, s *Session, pin Pin, arms []Arm, cases []Case, timeout time.Duration) (Results, error) {
	res := Results{}
	for _, a := range arms {
		res[a.Label] = map[string][]float64{}
		// Per-command stdout/stderr is the Runner's, under dist/logs.
		if err := os.MkdirAll(filepath.Join(s.Dir, "raw", a.Label), 0o755); err != nil {
			return nil, err
		}
	}
	watch := append([]int{pin.CPU}, pin.Siblings...)
	total := len(cases) * len(arms)
	done := 0
	suiteStart := time.Now()

	for _, c := range cases {
		for _, a := range arms {
			out := filepath.Join(s.Dir, "raw", a.Label, c.Name+".json")

			before, _ := sampleCPUs(watch)
			start := time.Now()
			runErr := x.Run(ctx, core.Cmd{
				Dir:    s.Dir,
				Args:   c.Args(a.Python, out, pin),
				EnvAdd: a.Env,
				Name:   fmt.Sprintf("%s-%s", a.Label, c.Name),
			})
			wall := time.Since(start)
			after, _ := sampleCPUs(watch)

			ev := Event{
				UTC:       start.UTC().Format(time.RFC3339),
				Arm:       a.Label,
				Benchmark: c.Name,
				WallSec:   wall.Seconds(),
				OK:        runErr == nil,
				Load1:     loadAvg1(),
			}
			// The pinned core is busy with our own work, so it says nothing.
			// A busy SIBLING is the contamination signal: it shares execution
			// units with the core we are measuring on.
			ev.PinBusy = busyFrac(before, after, pin.CPU)
			for _, sib := range pin.Siblings {
				if f := busyFrac(before, after, sib); f > ev.SibBusy {
					ev.SibBusy = f
				}
			}
			if runErr != nil {
				ev.Err = runErr.Error()
				os.Remove(out) // a partial pyperf file would parse as real data
			}
			s.Record(ev)

			done++
			progress(done, total, suiteStart, c.Name, a.Label, wall, runErr, ev.SibBusy)

			if runErr == nil {
				if vals, err := ParseResult(out); err == nil {
					for name, v := range vals {
						res[a.Label][name] = append(res[a.Label][name], v...)
					}
				} else {
					ev.Err = err.Error()
					s.Record(ev)
				}
			}
		}
	}
	fmt.Fprintf(os.Stderr, "  done in %s\n", time.Since(suiteStart).Round(time.Second))
	return res, nil
}

// A suite run is hours long, so every measurement reports as it lands: what
// ran, how long it took, and how far off the end is. The sibling-busy figure
// is printed inline rather than left for the postmortem -- it is the signal
// that the numbers being produced right now are contaminated.
func progress(done, total int, start time.Time, bench, arm string, wall time.Duration, err error, sibBusy float64) {
	elapsed := time.Since(start)
	var eta string
	if done > 0 && done < total {
		per := elapsed / time.Duration(done)
		eta = " eta " + (per * time.Duration(total-done)).Round(time.Second).String()
	}
	status := "ok"
	if err != nil {
		status = "FAILED"
	}
	warn := ""
	if sibBusy > 0.20 {
		warn = fmt.Sprintf("  [sibling cpu %.0f%% busy]", sibBusy*100)
	}
	fmt.Fprintf(os.Stderr, "[%*d/%d] %-26s %-10s %6s %s%s%s\n",
		len(fmt.Sprint(total)), done, total, bench, arm,
		wall.Round(100*time.Millisecond), status, eta, warn)
}

func busyFrac(before, after map[int]cpuTimes, cpu int) float64 {
	a, ok1 := before[cpu]
	b, ok2 := after[cpu]
	if !ok1 || !ok2 {
		return 0
	}
	total := b.total - a.total
	if total <= 0 {
		return 0
	}
	return 1 - float64(b.idle-a.idle)/float64(total)
}

// Compare reduces raw samples to a ratio table against one baseline arm.
type Row struct {
	Benchmark string             `json:"benchmark"`
	Min       map[string]float64 `json:"min_s"`
	Spread    map[string]float64 `json:"spread"`
	Ratio     map[string]float64 `json:"ratio_vs_baseline"`
}

func Compare(res Results, baseline string, arms []string) ([]Row, map[string]float64) {
	names := map[string]bool{}
	for _, a := range arms {
		for n := range res[a] {
			names[n] = true
		}
	}
	var ordered []string
	for n := range names {
		ordered = append(ordered, n)
	}
	sortStrings(ordered)

	var rows []Row
	ratios := map[string][]float64{}
	for _, n := range ordered {
		base, ok := Reduce(res[baseline][n])
		if !ok {
			continue
		}
		r := Row{Benchmark: n, Min: map[string]float64{}, Spread: map[string]float64{}, Ratio: map[string]float64{}}
		for _, a := range arms {
			agg, ok := Reduce(res[a][n])
			if !ok {
				continue
			}
			r.Min[a] = agg.Min
			r.Spread[a] = agg.Spread()
			r.Ratio[a] = Ratio(base, agg)
			if a != baseline {
				ratios[a] = append(ratios[a], r.Ratio[a])
			}
		}
		rows = append(rows, r)
	}
	geo := map[string]float64{}
	for a, rs := range ratios {
		geo[a] = Geomean(rs)
	}
	return rows, geo
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
