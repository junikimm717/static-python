package bench

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestReduceKeepsMinimumAndDropsFailures(t *testing.T) {
	a, ok := Reduce([]float64{3, 1.5, 0, 2, math.Inf(1)})
	if !ok {
		t.Fatal("want usable aggregate")
	}
	if a.Min != 1.5 || a.Max != 3 || a.N != 3 {
		t.Fatalf("min/max/n = %v/%v/%d, want 1.5/3/3", a.Min, a.Max, a.N)
	}
	if _, ok := Reduce([]float64{0, -1}); ok {
		t.Fatal("all-invalid series must not produce an aggregate")
	}
}

// The ratio must come from each side's own minimum, not from per-run ratios:
// the second is a different and more flattering statistic.
func TestRatioUsesPerSideMinima(t *testing.T) {
	base, _ := Reduce([]float64{10, 20})
	want, _ := Reduce([]float64{5, 40})
	if got := Ratio(base, want); math.Abs(got-2) > 1e-9 {
		t.Fatalf("Ratio = %v, want 2", got)
	}
}

func TestGeomeanCancelsInverses(t *testing.T) {
	if got := Geomean([]float64{2, 0.5}); math.Abs(got-1) > 1e-9 {
		t.Fatalf("Geomean = %v, want 1", got)
	}
}

func TestParseCPUList(t *testing.T) {
	got := parseCPUList("0,3-5,9")
	want := []int{0, 3, 4, 5, 9}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestTopologyDetectsHybridAndPicksFastest(t *testing.T) {
	root := t.TempDir()
	// Three fast cores that differ only in CPPC rank, plus one slow core. The
	// rank spread must NOT split the fast cores into separate classes: that is
	// the Zen 5 / Zen 5c shape, where four fast cores report 196-208 while
	// sharing one clock ceiling.
	for _, c := range []struct {
		id, class, rank int
		sibs            string
	}{
		{0, 5157895, 196, "0,2"},
		{1, 3289474, 125, "1,3"},
		{2, 5157895, 208, "0,2"},
		{4, 5157895, 202, "4,5"},
	} {
		dir := filepath.Join(root, "cpu"+itoa(c.id))
		os.MkdirAll(filepath.Join(dir, "acpi_cppc"), 0o755)
		os.MkdirAll(filepath.Join(dir, "cpufreq"), 0o755)
		os.MkdirAll(filepath.Join(dir, "topology"), 0o755)
		os.WriteFile(filepath.Join(dir, "acpi_cppc", "highest_perf"), []byte(itoa(c.rank)), 0o644)
		os.WriteFile(filepath.Join(dir, "cpufreq", "cpuinfo_max_freq"), []byte(itoa(c.class)), 0o644)
		os.WriteFile(filepath.Join(dir, "topology", "thread_siblings_list"), []byte(c.sibs), 0o644)
	}
	top, err := readTopology(root)
	if err != nil {
		t.Fatal(err)
	}
	if !top.Hybrid {
		t.Fatal("differing clock ceilings must read as hybrid")
	}
	if n := len(top.Fastest()); n != 3 {
		t.Fatalf("fastest class has %d cpus, want 3 (rank must not split a class)", n)
	}
	cpu, sibs, err := top.PickCore()
	if err != nil {
		t.Fatal(err)
	}
	// cpu0 takes the most interrupts and is skipped; among the rest the
	// highest CPPC rank wins, which is cpu2 at 208 over cpu4 at 202.
	if cpu != 2 {
		t.Fatalf("picked cpu%d, want cpu2", cpu)
	}
	if len(sibs) != 1 || sibs[0] != 0 {
		t.Fatalf("siblings = %v, want [0]", sibs)
	}
}

func TestNeedsWheels(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "a.txt")
	os.WriteFile(plain, []byte("# comment\n\npyperf\n"), 0o644)
	if _, ok := needsWheels(plain); ok {
		t.Fatal("a pyperf-only requirements file must not be skipped")
	}
	heavy := filepath.Join(dir, "b.txt")
	os.WriteFile(heavy, []byte("pyperf\ndjango==5.0\n"), 0o644)
	if what, ok := needsWheels(heavy); !ok || what == "" {
		t.Fatalf("django must be reported as unavailable, got %q %v", what, ok)
	}
}

func TestCompareRatiosAgainstBaseline(t *testing.T) {
	res := Results{
		"base": {"x": {10}, "y": {4}},
		"cand": {"x": {5}, "y": {8}},
	}
	rows, geo := Compare(res, "base", []string{"base", "cand"})
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if r := rows[0].Ratio["cand"]; math.Abs(r-2) > 1e-9 {
		t.Fatalf("x ratio = %v, want 2", r)
	}
	if r := rows[1].Ratio["cand"]; math.Abs(r-0.5) > 1e-9 {
		t.Fatalf("y ratio = %v, want 0.5", r)
	}
	if math.Abs(geo["cand"]-1) > 1e-9 {
		t.Fatalf("geomean = %v, want 1", geo["cand"])
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
