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

// DurationPrior is typical wall seconds for the pyperformance benchmarks
// we actually measure. It is not the full upstream suite — several
// benches never install, and some die at import — and a quiet-box kit
// has no previous session anyway. What transfers is the shape of the
// names we do run: bm_bpe_tokeniser is ~30x bm_argparse.
//
// A name that is not in the table is a new or newly-runnable bench, not
// a missing scale factor. It must not enter elapsed/weight, or one
// surprise ten-minute script would blow up every remaining estimate.
// Rebuild eta_weights.json from a timeline when the pin moves and the
// newly-run names have been timed.
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
	return fmt.Sprintf("%d known benches (not all of pyperformance)", len(p.weight))
}

func (p DurationPrior) known(bench string) (float64, bool) {
	v, ok := p.weight[bench]
	return v, ok && v > 0
}

// Remaining estimates wall time still to run. Scale is elapsed/weight
// over cells whose names are in the table only. An unknown leftover
// cell uses this run's mean for that bench if we have started it,
// otherwise the median known weight — so a new benchmark is a local
// guess, not a rewrite of the pace.
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

	var wKnown, elapsedKnown float64
	byBench := map[string][]float64{}
	for _, o := range observed {
		byBench[o.Bench] = append(byBench[o.Bench], o.Wall)
		if w, ok := p.known(o.Bench); ok {
			wKnown += w
			elapsedKnown += o.Wall
		}
	}
	scale := 1.0
	if wKnown > 0 && elapsedKnown > 0 {
		scale = elapsedKnown / wKnown
	}

	var sum float64
	for _, r := range remaining {
		if w, ok := p.known(r.Bench); ok {
			sum += w * scale
			continue
		}
		if xs := byBench[r.Bench]; len(xs) > 0 {
			sum += meanFloat(xs)
			continue
		}
		sum += p.median * scale
	}
	return time.Duration(sum * float64(time.Second))
}

func meanFloat(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
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
