package bench

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Every arm of a comparison must share one, or the ratio measures the
// scheduler.
type Pin struct {
	CPU      int
	Siblings []int
	// False when the platform refused, so the caller can report the numbers
	// as unpinned rather than pretending otherwise.
	Applied bool
}

// Affinity is set on ourselves rather than shelled out to taskset so that no
// tool outside the provisioned environment is needed, and so a child cannot
// escape by being launched through a wrapper.
func (t *Topology) Apply() (Pin, error) {
	cpu, sib, err := t.PickCore()
	if err != nil {
		return Pin{}, err
	}
	p := Pin{CPU: cpu, Siblings: sib}
	return p, p.apply()
}

func (p *Pin) apply() error {
	var set unix.CPUSet
	set.Zero()
	set.Set(p.CPU)
	if err := unix.SchedSetaffinity(0, &set); err != nil {
		return fmt.Errorf("pin to cpu%d: %w", p.CPU, err)
	}
	p.Applied = true
	return nil
}

// Names the SMT sibling, because "pinned" reads as "isolated" and is not the
// same thing.
func (p Pin) Describe() string {
	if !p.Applied {
		return "unpinned (results will vary with scheduler placement)"
	}
	if len(p.Siblings) == 0 {
		return fmt.Sprintf("pinned to cpu%d", p.CPU)
	}
	return fmt.Sprintf("pinned to cpu%d (SMT sibling cpu%s shares its execution units)",
		p.CPU, joinInts(p.Siblings, ",cpu"))
}

func joinInts(xs []int, sep string) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, sep)
}

// Fraction of a sampling window a CPU spent doing anything but idling.
type Busy struct {
	CPU  int
	Frac float64
}

// Pinning is not isolation: work on an SMT sibling shares execution units with
// the pinned core and biases the result in a way that reads as a plausible
// measurement rather than as noise.
func (p Pin) CheckQuiet(window time.Duration, threshold float64) ([]Busy, error) {
	if !p.Applied {
		return nil, nil
	}
	watch := append([]int{p.CPU}, p.Siblings...)
	first, err := sampleCPUs(watch)
	if err != nil {
		return nil, err
	}
	time.Sleep(window)
	second, err := sampleCPUs(watch)
	if err != nil {
		return nil, err
	}
	var busy []Busy
	for _, c := range watch {
		a, b := first[c], second[c]
		total := b.total - a.total
		if total <= 0 {
			continue
		}
		// Our sampling sleep is idle time, so a busy reading is someone else.
		frac := 1 - float64(b.idle-a.idle)/float64(total)
		if frac > threshold {
			busy = append(busy, Busy{CPU: c, Frac: frac})
		}
	}
	return busy, nil
}

type cpuTimes struct{ total, idle int64 }

func sampleCPUs(want []int) (map[int]cpuTimes, error) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil, err
	}
	wanted := map[int]bool{}
	for _, c := range want {
		wanted[c] = true
	}
	out := map[int]cpuTimes{}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "cpu") || line == "" {
			continue
		}
		fields := strings.Fields(line)
		id, err := strconv.Atoi(strings.TrimPrefix(fields[0], "cpu"))
		if err != nil || !wanted[id] {
			continue // the bare "cpu" aggregate line, or one we do not watch
		}
		var t cpuTimes
		for i, f := range fields[1:] {
			v, err := strconv.ParseInt(f, 10, 64)
			if err != nil {
				continue
			}
			t.total += v
			// idle and iowait
			if i == 3 || i == 4 {
				t.idle += v
			}
		}
		out[id] = t
	}
	return out, nil
}
