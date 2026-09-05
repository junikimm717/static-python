package bench

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRemainingFallsBackToCountAverage(t *testing.T) {
	var p DurationPrior
	observed := []obs{{cellKey{"a", "x"}, 10}}
	remaining := []cellKey{{"a", "y"}, {"a", "z"}}
	got := p.Remaining(observed, remaining, 10*time.Second)
	if got != 20*time.Second {
		t.Fatalf("Remaining = %s, want 20s", got)
	}
}

func TestRemainingUsesWeightsNotTheAverageSoFar(t *testing.T) {
	// The failure mode: a long early bench inflates elapsed/done, then
	// the rest of the suite is short. Count-average says 30s left after
	// the first cell; the weights know the leftover bench is 2s.
	p := newPrior(map[string]float64{"long": 30, "short": 2})
	observed := []obs{{cellKey{"default", "long"}, 30}}
	remaining := []cellKey{{"default", "short"}}
	got := p.Remaining(observed, remaining, 30*time.Second)
	if math.Abs(got.Seconds()-2) > 0.01 {
		t.Fatalf("Remaining = %s, want 2s", got)
	}
}

func TestRemainingScalesToThisRun(t *testing.T) {
	p := newPrior(map[string]float64{"a": 10, "b": 20, "c": 40})
	// Same relative shape, twice as slow as the embedded seconds.
	observed := []obs{
		{cellKey{"static", "a"}, 20},
		{cellKey{"static", "b"}, 40},
	}
	remaining := []cellKey{{"static", "c"}}
	got := p.Remaining(observed, remaining, 60*time.Second)
	if math.Abs(got.Seconds()-80) > 0.01 {
		t.Fatalf("Remaining = %s, want 80s (40 weight * 2 scale)", got)
	}
}

func TestRemainingDoesNotNeedMatchingArms(t *testing.T) {
	p := newPrior(map[string]float64{"one": 10, "two": 30})
	// Lineup this run has never appeared in a prior session.
	observed := []obs{{cellKey{"kit-foo", "one"}, 10}}
	remaining := []cellKey{
		{Arm: "kit-foo", Bench: "two"},
		{Arm: "kit-bar", Bench: "two"},
	}
	got := p.Remaining(observed, remaining, 10*time.Second)
	if math.Abs(got.Seconds()-60) > 0.01 {
		t.Fatalf("Remaining = %s, want 60s (2 × 30)", got)
	}
}

func TestRemainingUnknownBenchUsesMedianWeight(t *testing.T) {
	p := newPrior(map[string]float64{"a": 10, "b": 20, "c": 30})
	observed := []obs{{cellKey{"x", "a"}, 10}}
	remaining := []cellKey{{"x", "brand_new"}}
	got := p.Remaining(observed, remaining, 10*time.Second)
	if math.Abs(got.Seconds()-20) > 0.01 {
		t.Fatalf("Remaining = %s, want 20s (median weight)", got)
	}
}

func TestDefaultPriorReplaysCommittedTimeline(t *testing.T) {
	dir, err := filepath.Abs("../../../../benchmarks")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "20260905T015400Z-amd64", "timeline.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Skip("committed timeline not present")
	}
	evs, err := readTimeline(path)
	if err != nil {
		t.Fatal(err)
	}
	if mae := replayMAE(DefaultPrior(), evs); mae > 10 {
		t.Fatalf("full-lineup MAE = %.1f min, want < 10", mae)
	}
	// A kit is not last night's ten-arm session. Two arms, new labels
	// relative to the weight table, still have to stay on the clock.
	subset := filterArms(evs, "default", "reference")
	if mae := replayMAE(DefaultPrior(), subset); mae > 10 {
		t.Fatalf("two-arm MAE = %.1f min, want < 10", mae)
	}
}

func replayMAE(p DurationPrior, evs []Event) float64 {
	keys := make([]cellKey, len(evs))
	for i, e := range evs {
		keys[i] = cellKey{e.Arm, e.Benchmark}
	}
	var (
		observed []obs
		elapsed  float64
		absErr   float64
		n        int
	)
	for i, e := range evs {
		elapsed += e.WallSec
		observed = append(observed, obs{cellKey{e.Arm, e.Benchmark}, e.WallSec})
		if i+1 == len(evs) {
			break
		}
		pred := p.Remaining(observed, keys[i+1:], time.Duration(elapsed*float64(time.Second)))
		var trueRem float64
		for _, r := range evs[i+1:] {
			trueRem += r.WallSec
		}
		absErr += math.Abs(pred.Seconds() - trueRem)
		n++
	}
	return absErr / float64(n) / 60
}

func filterArms(evs []Event, arms ...string) []Event {
	want := map[string]bool{}
	for _, a := range arms {
		want[a] = true
	}
	var out []Event
	for _, e := range evs {
		if want[e.Arm] {
			out = append(out, e)
		}
	}
	return out
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
