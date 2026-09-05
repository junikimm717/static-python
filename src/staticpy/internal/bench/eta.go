package bench

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

//go:embed eta_weights.json
var defaultWeightsJSON []byte

// cellKey is one measurement in the interleaved suite.
type cellKey struct {
	Arm, Bench string
}

type obs struct {
	cellKey
	Wall float64
}

// DurationPrior is typical wall seconds per pyperformance benchmark,
// relative lengths of the suite rather than a recording of last night's
// kit. A quiet-box kit run has no previous session and a different
// lineup; what transfers is that bm_bpe_tokeniser is ~30x bm_argparse.
//
// The numbers are mean ok-cell walls (wall_s >= 1) from the committed
// benchmarks/ timelines. Rebuild the file when the pyperformance pin
// moves. This run's elapsed / completed-weight is the only scale.
type DurationPrior struct {
	weight map[string]float64
	median float64
}

func DefaultPrior() DurationPrior {
	var m map[string]float64
	if err := json.Unmarshal(defaultWeightsJSON, &m); err != nil {
		return DurationPrior{}
	}
	return newPrior(m)
}

func newPrior(m map[string]float64) DurationPrior {
	xs := make([]float64, 0, len(m))
	for _, v := range m {
		if v > 0 {
			xs = append(xs, v)
		}
	}
	return DurationPrior{weight: m, median: medianFloat(xs)}
}

func (p DurationPrior) empty() bool {
	return len(p.weight) == 0
}

func (p DurationPrior) Describe() string {
	if p.empty() {
		return "none (count-average)"
	}
	return fmt.Sprintf("%d bench weights (embedded)", len(p.weight))
}

func (p DurationPrior) of(bench string) float64 {
	if v := p.weight[bench]; v > 0 {
		return v
	}
	if p.median > 0 {
		return p.median
	}
	return 0
}

// Remaining estimates wall time still to run: this run's seconds-per-
// weight times the weight of every leftover cell. Without weights it
// is elapsed/done × remaining, which is what used to be printed always.
func (p DurationPrior) Remaining(observed []obs, remaining []cellKey, elapsed time.Duration) time.Duration {
	nRem := len(remaining)
	if nRem == 0 {
		return 0
	}
	if p.empty() {
		if n := len(observed); n > 0 {
			return time.Duration(elapsed.Seconds() / float64(n) * float64(nRem) * float64(time.Second))
		}
		return 0
	}
	var wDone float64
	for _, o := range observed {
		wDone += p.of(o.Bench)
	}
	scale := 1.0
	if wDone > 0 && elapsed > 0 {
		scale = elapsed.Seconds() / wDone
	}
	var sum float64
	for _, r := range remaining {
		sum += p.of(r.Bench) * scale
	}
	return time.Duration(sum * float64(time.Second))
}

func medianFloat(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
