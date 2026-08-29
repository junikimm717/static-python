package cli

import (
	"strings"
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/bench"
)

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
