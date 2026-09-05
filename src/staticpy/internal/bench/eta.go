package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"
)

// cellKey is one measurement in the interleaved suite.
type cellKey struct {
	Arm, Bench string
}

type obs struct {
	cellKey
	Wall float64
}

// DurationPrior is wall-clock seconds for (arm, benchmark) cells from
// earlier sessions. The suite order is alphabetical and the cells are
// stable to a couple of percent run-to-run, so summing the remaining
// cells is a much better ETA than assuming every measurement takes the
// average so far — a handful of early benches take ten minutes and most
// of the rest take fifteen seconds.
type DurationPrior struct {
	cell    map[cellKey]float64
	bench   map[string]float64
	Sources []string
}

func (p DurationPrior) empty() bool {
	return len(p.cell) == 0
}

func (p DurationPrior) Describe() string {
	if p.empty() {
		return "none (count-average)"
	}
	n := len(p.Sources)
	src := "session"
	if n != 1 {
		src = "sessions"
	}
	return fmt.Sprintf("%d cells from %d %s", len(p.cell), n, src)
}

// FindTimelineFiles lists timeline.jsonl under each session tree, oldest
// first so a later LoadDurationPrior merge lets the newest cell win.
// skipDir is the session being written now — using it as its own prior
// would read a partial file that grows as we go.
func FindTimelineFiles(dirs []string, skipDir string) []string {
	skipDir = filepath.Clean(skipDir)
	seen := map[string]bool{}
	var out []string
	for _, dir := range dirs {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() || e.Name() == "latest" {
				continue
			}
			sess := filepath.Join(dir, e.Name())
			if filepath.Clean(sess) == skipDir {
				continue
			}
			p := filepath.Join(sess, "timeline.jsonl")
			st, err := os.Stat(p)
			if err != nil || st.Size() == 0 {
				continue
			}
			if seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// LoadDurationPrior merges timeline files, oldest first. A newer file
// replaces a cell; bench means are taken from the merged map so a cell
// is never counted twice.
func LoadDurationPrior(files []string) DurationPrior {
	p := DurationPrior{
		cell:  map[cellKey]float64{},
		bench: map[string]float64{},
	}
	for _, f := range files {
		evs, err := readTimeline(f)
		if err != nil {
			continue
		}
		p.Sources = append(p.Sources, filepath.Base(filepath.Dir(f)))
		for _, e := range evs {
			if e.Arm == "" || e.Benchmark == "" || e.WallSec <= 0 {
				continue
			}
			p.cell[cellKey{e.Arm, e.Benchmark}] = e.WallSec
		}
	}
	type acc struct {
		sum float64
		n   int
	}
	benches := map[string]acc{}
	for k, w := range p.cell {
		a := benches[k.Bench]
		a.sum += w
		a.n++
		benches[k.Bench] = a
	}
	for b, a := range benches {
		p.bench[b] = a.sum / float64(a.n)
	}
	return p
}

func readTimeline(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var evs []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		evs = append(evs, e)
	}
	return evs, sc.Err()
}

// Remaining estimates wall time still to run.
//
// For each leftover cell: the prior (arm, bench) duration, else this
// run's mean for that bench so far, else the prior bench mean, else
// elapsed/done. A median this/prior scale absorbs a faster or slower
// machine without letting one short-bench outlier (the mean would)
// drag the whole remaining sum.
func (p DurationPrior) Remaining(observed []obs, remaining []cellKey, elapsed time.Duration) time.Duration {
	nRem := len(remaining)
	if nRem == 0 {
		return 0
	}
	fallback := 0.0
	if n := len(observed); n > 0 {
		fallback = elapsed.Seconds() / float64(n)
	}

	var ratios []float64
	byBench := map[string][]float64{}
	for _, o := range observed {
		byBench[o.Bench] = append(byBench[o.Bench], o.Wall)
		if prior, ok := p.cell[o.cellKey]; ok && prior > 0 {
			ratios = append(ratios, o.Wall/prior)
		}
	}
	scale := 1.0
	if m := medianFloat(ratios); m > 0 {
		scale = m
	}

	var sum float64
	for _, r := range remaining {
		switch {
		case p.cell[r] > 0:
			sum += p.cell[r] * scale
		case len(byBench[r.Bench]) > 0:
			sum += meanFloat(byBench[r.Bench])
		case p.bench[r.Bench] > 0:
			sum += p.bench[r.Bench] * scale
		default:
			sum += fallback
		}
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
