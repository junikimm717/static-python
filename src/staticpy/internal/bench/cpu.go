// Package bench holds the measurement machinery behind `staticpy bench`:
// CPU topology discovery, run pinning, and aggregation. See the
// staticpy-traps skill for why each rule here exists.
package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type CPU struct {
	ID int
	// Class is the core TYPE: equal values mean interchangeable cores. Taken
	// from the scheduler's capacity view, which separates Intel P/E and AMD
	// Zen/Zen-c alike without naming either.
	Class int
	// Rank orders cores WITHIN a class -- CPPC preferred-core ordering, where
	// the firmware reports which silicon clocks highest. Conflating it with
	// Class splits one core type into several phantom ones.
	Rank int
	// Every logical CPU sharing this physical core, self included.
	Siblings []int
	// CoreID identifies the physical core, so callers can avoid handing two
	// arms the two threads of one core.
	CoreID int
}

type Topology struct {
	CPUs []CPU
	// An unpinned benchmark that migrates between core classes swings by the
	// ratio between them, which is larger than most effects under test.
	Hybrid bool
}

const cpuRoot = "/sys/devices/system/cpu"

// A missing sysfs entry is not an error: it degrades to a flat, non-hybrid
// topology, so the caller falls back to not pinning rather than pinning to
// something wrong.
func readTopology(root string) (*Topology, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var t Topology
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, "cpu") {
			continue
		}
		id, err := strconv.Atoi(strings.TrimPrefix(name, "cpu"))
		if err != nil {
			continue // "cpufreq", "cpuidle" and friends
		}
		dir := filepath.Join(root, name)
		coreID, _ := strconv.Atoi(readFile(filepath.Join(dir, "topology", "core_id")))
		t.CPUs = append(t.CPUs, CPU{
			ID:       id,
			Class:    readClass(dir),
			Rank:     readRank(dir),
			CoreID:   coreID,
			Siblings: parseCPUList(readFile(filepath.Join(dir, "topology", "thread_siblings_list"))),
		})
	}
	if len(t.CPUs) == 0 {
		return nil, fmt.Errorf("no cpus under %s", root)
	}
	sort.Slice(t.CPUs, func(i, j int) bool { return t.CPUs[i].ID < t.CPUs[j].ID })
	for _, c := range t.CPUs {
		if c.Class != t.CPUs[0].Class {
			t.Hybrid = true
			break
		}
	}
	return &t, nil
}

func ReadTopology() (*Topology, error) { return readTopology(cpuRoot) }

// Cores of one type report an identical ceiling here, which is what makes this
// the class and not acpi_cppc: on Zen 5 + Zen 5c the four fast cores all read
// 5157895 while their CPPC values spread across 196-208.
func readClass(dir string) int {
	for _, rel := range []string{"cpu_capacity", "cpufreq/cpuinfo_max_freq"} {
		if v, err := strconv.Atoi(readFile(filepath.Join(dir, rel))); err == nil && v > 0 {
			return v
		}
	}
	return 0
}

func readRank(dir string) int {
	v, err := strconv.Atoi(readFile(filepath.Join(dir, "acpi_cppc", "highest_perf")))
	if err != nil {
		return 0
	}
	return v
}

func readFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// "0,3-5" is 0,3,4,5.
func parseCPUList(s string) []int {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, ok := strings.Cut(part, "-")
		a, err := strconv.Atoi(lo)
		if err != nil {
			continue
		}
		b := a
		if ok {
			if b, err = strconv.Atoi(hi); err != nil {
				continue
			}
		}
		for ; a <= b; a++ {
			out = append(out, a)
		}
	}
	return out
}

func (t *Topology) Fastest() []CPU {
	best := 0
	for _, c := range t.CPUs {
		if c.Class > best {
			best = c.Class
		}
	}
	var out []CPU
	for _, c := range t.CPUs {
		if c.Class == best {
			out = append(out, c)
		}
	}
	return out
}

// Within the fastest class, prefer the highest CPPC rank -- the core the
// firmware says clocks highest -- and avoid cpu0, which takes the most
// interrupts. What matters most is only that every arm of a comparison lands
// on the same one.
func (t *Topology) PickCore() (cpu int, siblings []int, err error) {
	cands := t.Fastest()
	if len(cands) == 0 {
		return 0, nil, fmt.Errorf("no cpus to choose from")
	}
	pick := cands[0]
	for _, c := range cands {
		if c.ID == 0 {
			continue
		}
		if pick.ID == 0 || c.Rank > pick.Rank {
			pick = c
		}
	}
	var sib []int
	for _, s := range pick.Siblings {
		if s != pick.ID {
			sib = append(sib, s)
		}
	}
	return pick.ID, sib, nil
}

// For the report's provenance block, so a number can be traced back to the
// machine shape it was taken on.
func (t *Topology) Describe() string {
	classes := map[int]int{}
	for _, c := range t.CPUs {
		classes[c.Class]++
	}
	perfs := make([]int, 0, len(classes))
	for p := range classes {
		perfs = append(perfs, p)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(perfs)))
	parts := make([]string, 0, len(perfs))
	for _, p := range perfs {
		parts = append(parts, fmt.Sprintf("%d×%s", classes[p], ClassLabel(p)))
	}
	kind := "uniform"
	if t.Hybrid {
		kind = "hybrid"
	}
	return fmt.Sprintf("%d logical cpus, %s (%s)", len(t.CPUs), kind, strings.Join(parts, ", "))
}

// classLabel renders a class as a clock where the value looks like a kHz
// ceiling, which is what cpuinfo_max_freq reports.
func ClassLabel(class int) string {
	if class > 100000 {
		return fmt.Sprintf("%.2fGHz", float64(class)/1e6)
	}
	return fmt.Sprintf("capacity=%d", class)
}

// ByID finds one logical CPU.
func (t *Topology) ByID(id int) (CPU, bool) {
	for _, c := range t.CPUs {
		if c.ID == id {
			return c, true
		}
	}
	return CPU{}, false
}

// ApplyCPU pins to a caller-chosen logical CPU.
func (t *Topology) ApplyCPU(id int) (Pin, error) {
	c, ok := t.ByID(id)
	if !ok {
		return Pin{}, fmt.Errorf("no cpu%d on this machine", id)
	}
	var sib []int
	for _, s := range c.Siblings {
		if s != id {
			sib = append(sib, s)
		}
	}
	p := Pin{CPU: id, Siblings: sib}
	return p, p.apply()
}
