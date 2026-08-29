package bench

import (
	"math"
	"sort"
)

// Repeats reduce to the MINIMUM, not the mean or median: benchmark noise is
// one-sided, so the minimum is the best estimate of the machine's true speed
// and an average folds in the contamination the repeats exist to remove.
type Aggregate struct {
	Min, Max float64
	N        int
}

// (max-min)/min. Above a few percent, something else was running.
func (a Aggregate) Spread() float64 {
	if a.Min <= 0 {
		return 0
	}
	return (a.Max - a.Min) / a.Min
}

// Non-positive samples are dropped: a zero timing is a failed measurement, and
// letting it through would make it the minimum and silently win.
func Reduce(samples []float64) (Aggregate, bool) {
	var vals []float64
	for _, s := range samples {
		if s > 0 && !math.IsInf(s, 0) && !math.IsNaN(s) {
			vals = append(vals, s)
		}
	}
	if len(vals) == 0 {
		return Aggregate{}, false
	}
	sort.Float64s(vals)
	return Aggregate{Min: vals[0], Max: vals[len(vals)-1], N: len(vals)}, true
}

// A speedup: >1 means want is faster than base. Each side reduces to its own
// minimum before the division -- taking the minimum of per-run ratios instead
// picks whichever repeat happened to favour the numerator.
func Ratio(base, want Aggregate) float64 {
	if want.Min <= 0 {
		return 0
	}
	return base.Min / want.Min
}

// The right average for ratios: a 2x win and a 2x loss cancel, which they do
// not under an arithmetic mean.
func Geomean(ratios []float64) float64 {
	var sum float64
	var n int
	for _, r := range ratios {
		if r > 0 {
			sum += math.Log(r)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return math.Exp(sum / float64(n))
}
