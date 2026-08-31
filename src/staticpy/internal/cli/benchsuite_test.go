package cli

import (
	"strings"
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/bench"
	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

func TestInterpFlagAcceptsAblationAndProfiles(t *testing.T) {
	var entries []interpEntry
	f := interpFlag{&entries}
	for _, name := range []string{"ablation", "static", "nomimalloc", "reference-nolto"} {
		if err := f.Set(name); err != nil {
			t.Fatalf("Set(%q): %v", name, err)
		}
	}
	if len(entries) != 4 || entries[0].Label != "ablation" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestPickBaselineDefaultsToReference(t *testing.T) {
	got, err := pickBaseline([]string{"static", "reference"}, "")
	if err != nil || got != "reference" {
		t.Fatalf("got %q err %v, want reference", got, err)
	}
	got, err = pickBaseline([]string{"static", "system"}, "")
	if err != nil || got != "static" {
		t.Fatalf("without reference, first interp wins: got %q err %v", got, err)
	}
	got, err = pickBaseline([]string{"static", "reference"}, "static")
	if err != nil || got != "static" {
		t.Fatalf("--baseline must win: got %q err %v", got, err)
	}
}

func TestPinsOfUsesConfigBench(t *testing.T) {
	cfg := &config.Config{Bench: config.BenchConfig{
		Pyperformance: "1.14.0",
		Pyperf:        "2.10.0",
		Ablation:      []string{"reference", "default"},
	}}
	p := pinsOf(cfg)
	if p.Pyperformance != "1.14.0" || p.Pyperf != "2.10.0" {
		t.Fatalf("pins = %+v", p)
	}
	if len(p.Ablation) != 2 || p.Ablation[0] != "reference" {
		t.Fatalf("ablation = %v", p.Ablation)
	}
}

// One line per benchmark naming every arm that lost it: all arms means the
// suite is at fault, one arm means the interpreters actually differ, and that
// distinction is the only thing making skipped.json worth reading.
func TestSummarizeFailuresGroupsArmsPerBenchmark(t *testing.T) {
	got := summarizeFailures([]bench.Failure{
		{Benchmark: "bm_sympy", Arm: "static", Reason: "No module named 'distutils'"},
		{Benchmark: "bm_sympy", Arm: "reference", Reason: "No module named 'distutils'"},
		{Benchmark: "bm_ctypes", Arm: "static", Reason: "dlopen unsupported"},
	})
	if len(got) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(got), got)
	}
	if !strings.HasPrefix(got[0], "bm_sympy (failed at runtime on static, reference:") {
		t.Fatalf("line 0 = %q", got[0])
	}
	if !strings.Contains(got[1], "on static:") || strings.Contains(got[1], "reference") {
		t.Fatalf("line 1 must name only the arm that failed: %q", got[1])
	}
	if len(summarizeFailures(nil)) != 0 {
		t.Fatal("no failures must produce no lines")
	}
}
