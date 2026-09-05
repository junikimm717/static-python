package bench

import (
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

func TestRemainingUsesPriorCellsNotTheAverageSoFar(t *testing.T) {
	// The failure mode: two long early cells inflate elapsed/done, then
	// the rest of the suite is short. Count-average says 30s left after
	// the first cell; the prior knows the leftover cell is 2s.
	p := DurationPrior{
		cell: map[cellKey]float64{
			{"default", "long"}:  30,
			{"default", "short"}: 2,
		},
		bench: map[string]float64{"long": 30, "short": 2},
	}
	observed := []obs{{cellKey{"default", "long"}, 30}}
	remaining := []cellKey{{"default", "short"}}
	got := p.Remaining(observed, remaining, 30*time.Second)
	if math.Abs(got.Seconds()-2) > 0.01 {
		t.Fatalf("Remaining = %s, want 2s", got)
	}
	avg := 30 * time.Second // elapsed/done * remaining count
	if avg == got {
		t.Fatal("prior ETA collapsed to the count-average")
	}
}

func TestRemainingScalesByMedianPace(t *testing.T) {
	p := DurationPrior{
		cell: map[cellKey]float64{
			{"default", "a"}: 10,
			{"default", "b"}: 20,
			{"default", "c"}: 40,
		},
	}
	// This machine is 2x the prior on every completed cell.
	observed := []obs{
		{cellKey{"default", "a"}, 20},
		{cellKey{"default", "b"}, 40},
	}
	remaining := []cellKey{{"default", "c"}}
	got := p.Remaining(observed, remaining, 60*time.Second)
	if math.Abs(got.Seconds()-80) > 0.01 {
		t.Fatalf("Remaining = %s, want 80s (40 prior * 2 scale)", got)
	}
}

func TestRemainingFallsBackToThisRunBenchMean(t *testing.T) {
	var p DurationPrior
	observed := []obs{
		{cellKey{"default", "bm"}, 12},
		{cellKey{"nolto", "bm"}, 14},
	}
	remaining := []cellKey{{"reference", "bm"}}
	got := p.Remaining(observed, remaining, 26*time.Second)
	if math.Abs(got.Seconds()-13) > 0.01 {
		t.Fatalf("Remaining = %s, want 13s (mean of this-run bm)", got)
	}
}

func TestFindTimelineFilesSkipsCurrentAndLatest(t *testing.T) {
	root := t.TempDir()
	writeTL := func(name string, body string) {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "timeline.jsonl"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeTL("20260101T000000Z-amd64", `{"arm":"a","benchmark":"x","wall_s":1}`+"\n")
	writeTL("20260102T000000Z-amd64", `{"arm":"a","benchmark":"x","wall_s":2}`+"\n")
	current := filepath.Join(root, "20260103T000000Z-amd64")
	writeTL("20260103T000000Z-amd64", `{"arm":"a","benchmark":"x","wall_s":3}`+"\n")
	if err := os.Symlink("20260103T000000Z-amd64", filepath.Join(root, "latest")); err != nil {
		t.Fatal(err)
	}

	got := FindTimelineFiles([]string{root}, current)
	if len(got) != 2 {
		t.Fatalf("files = %v, want 2 older sessions", got)
	}
	p := LoadDurationPrior(got)
	if p.cell[cellKey{"a", "x"}] != 2 {
		t.Fatalf("merged cell = %v, want newest-wins 2", p.cell[cellKey{"a", "x"}])
	}
	if len(p.Sources) != 2 {
		t.Fatalf("Sources = %v", p.Sources)
	}
}

func TestPriorCrossRunMatchesCommittedTimelines(t *testing.T) {
	dir, err := filepath.Abs("../../../../benchmarks")
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "20260904T143118Z-amd64", "timeline.jsonl")
	neu := filepath.Join(dir, "20260905T015400Z-amd64", "timeline.jsonl")
	if _, err := os.Stat(old); err != nil {
		t.Skip("committed timelines not present")
	}
	if _, err := os.Stat(neu); err != nil {
		t.Skip("committed timelines not present")
	}

	prior := LoadDurationPrior([]string{old})
	evs, err := readTimeline(neu)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) < 100 {
		t.Fatalf("timeline too short: %d", len(evs))
	}

	var (
		observed []obs
		elapsed  float64
		absErr   float64
		n        int
	)
	keys := make([]cellKey, len(evs))
	for i, e := range evs {
		keys[i] = cellKey{e.Arm, e.Benchmark}
	}
	for i, e := range evs {
		elapsed += e.WallSec
		observed = append(observed, obs{cellKey{e.Arm, e.Benchmark}, e.WallSec})
		if i+1 == len(evs) {
			break
		}
		pred := prior.Remaining(observed, keys[i+1:], time.Duration(elapsed*float64(time.Second)))
		var trueRem float64
		for _, r := range evs[i+1:] {
			trueRem += r.WallSec
		}
		absErr += math.Abs(pred.Seconds() - trueRem)
		n++
	}
	mae := absErr / float64(n) / 60
	// Cross-run MAE on these two sessions is ~0.8 min. A regression
	// back to count-average is ~200 min.
	if mae > 3 {
		t.Fatalf("cross-run MAE = %.1f min, want < 3", mae)
	}
}
