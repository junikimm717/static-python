package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/bench"
	"github.com/junikimm717/static-python/src/staticpy/internal/tui"
)

// cpuMenu offers one row per physical core, fastest type first.
//
// The two SMT threads of a core are the same silicon, so listing both doubles
// the menu without adding a decision; --cpu still accepts any logical cpu for
// the rare case that the specific thread matters. Slower core types are
// offered rather than withheld: which core a run used is recorded in the
// report and warned about at the time, so refusing the choice buys nothing.
func cpuMenu(t *bench.Topology, recommended int) tui.Menu {
	fastest := t.Fastest()[0].Class
	seen := map[int]bool{}
	var cores []bench.CPU
	for _, c := range t.CPUs {
		if seen[c.CoreID] {
			continue
		}
		seen[c.CoreID] = true
		cores = append(cores, c)
	}
	sort.SliceStable(cores, func(i, j int) bool { return cores[i].Class > cores[j].Class })

	m := tui.Menu{
		Title: "Which CPU should the benchmark run on?",
		Help: "Every arm is pinned to this one core, so the choice moves absolute\n" +
			"times, not the ratio between arms. Prefer the fastest type; what\n" +
			"matters most is that one run uses one core.\n" +
			"One row per physical core -- its second thread is in the last column,\n" +
			"and --cpu takes either.",
		Headers: []string{"cpu", "core", "max clock", "boost rank", "2nd thread"},
		Flag:    "--cpu",
		Default: strconv.Itoa(recommended),
	}
	g := tui.Group{}
	for _, c := range cores {
		sib := "none"
		for _, s := range c.Siblings {
			if s != c.ID {
				sib = fmt.Sprintf("cpu%d", s)
			}
		}
		ch := tui.Choice{
			Value: strconv.Itoa(c.ID),
			Cells: []string{
				fmt.Sprintf("cpu%d", c.ID),
				fmt.Sprintf("core%d", c.CoreID),
				bench.ClassLabel(c.Class),
				strconv.Itoa(c.Rank),
				sib,
			},
		}
		switch {
		case c.ID == recommended:
			ch.Note = "recommended: fastest type, highest boost rank"
		case c.Class != fastest:
			ch.Note = "slower core type"
		case c.ID == 0:
			ch.Note = "handles the most interrupts"
		}
		g.Choices = append(g.Choices, ch)
	}
	m.Groups = []tui.Group{g}
	return m
}

// choosePin resolves the CPU to pin to: the flag if given, else the menu, else
// the recommendation.
//
// Anything that would leave the run unpinned after the caller asked for a
// specific core is an error rather than a downgrade. Unpinned numbers look
// exactly like pinned ones and are not comparable to them, so --no-pin is the
// only way to ask for them.
func choosePin(disabled bool, want int) (bench.Pin, *bench.Topology, error) {
	topo, err := bench.ReadTopology()
	if err != nil {
		if want >= 0 {
			return bench.Pin{}, nil, fmt.Errorf("--cpu %d: cannot read cpu topology: %w\n"+
				"  pass --no-pin to measure without pinning", want, err)
		}
		fmt.Fprintf(os.Stderr, "%s cannot read cpu topology (%v); running unpinned\n", yellow("note:"), err)
		return bench.Pin{}, nil, nil
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", bold("machine:"), topo.Describe())
	if disabled {
		if topo.Hybrid {
			fmt.Fprintf(os.Stderr, "%s --no-pin on a hybrid cpu: runs that migrate between core types are not comparable\n", yellow("warning:"))
		}
		return bench.Pin{}, topo, nil
	}

	recommended, _, err := topo.PickCore()
	if err != nil {
		return bench.Pin{}, topo, fmt.Errorf("no cpu to pin to: %w\n"+
			"  pass --no-pin to measure without pinning", err)
	}
	if want < 0 {
		v, err := tui.SelectOr(cpuMenu(topo, recommended))
		if err != nil {
			// Aborting the menu aborts the run: silently benchmarking on a
			// core the user declined would be worse than stopping.
			fmt.Fprintf(os.Stderr, "%s %v\n", red("aborted:"), err)
			os.Exit(1)
		}
		if want, err = strconv.Atoi(v); err != nil {
			want = recommended
		}
	} else if _, ok := topo.ByID(want); !ok {
		return bench.Pin{}, topo, fmt.Errorf("--cpu %d: this machine has no cpu%d\n%s",
			want, want, pinAdvice(topo))
	}

	pin, err := topo.ApplyCPU(want)
	if err != nil {
		return bench.Pin{}, topo, fmt.Errorf("%w\n"+
			"  pass --no-pin to measure without pinning", err)
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", bold("affinity:"), pin.Describe())
	if c, ok := topo.ByID(want); ok && c.Class != topo.Fastest()[0].Class {
		fmt.Fprintf(os.Stderr, "%s cpu%d is %s, not the fastest %s on this machine; absolute numbers will not be comparable to runs on one that is\n",
			yellow("warning:"), want, bench.ClassLabel(c.Class), bench.ClassLabel(topo.Fastest()[0].Class))
	}
	warnIfBusy(pin)
	return pin, topo, nil
}

// Names the cores worth passing, since "no cpu999" alone does not say what to
// use instead.
func pinAdvice(t *bench.Topology) string {
	all := make([]int, 0, len(t.CPUs))
	for _, c := range t.CPUs {
		all = append(all, c.ID)
	}
	fast := make([]int, 0, len(t.CPUs))
	for _, c := range t.Fastest() {
		fast = append(fast, c.ID)
	}
	s := fmt.Sprintf("  this machine has %s\n", compactCPUs(all))
	if len(fast) < len(all) {
		s += fmt.Sprintf("  fastest core type (%s): %s\n",
			bench.ClassLabel(t.Fastest()[0].Class), compactCPUs(fast))
	}
	return s + "  or pass --no-pin to measure without pinning"
}

// "cpu0-3, cpu12-15" rather than sixteen comma-separated ids.
func compactCPUs(ids []int) string {
	if len(ids) == 0 {
		return "no cpus"
	}
	var parts []string
	start, prev := ids[0], ids[0]
	flush := func() {
		if start == prev {
			parts = append(parts, fmt.Sprintf("cpu%d", start))
			return
		}
		parts = append(parts, fmt.Sprintf("cpu%d-%d", start, prev))
	}
	for _, id := range ids[1:] {
		if id == prev+1 {
			prev = id
			continue
		}
		flush()
		start, prev = id, id
	}
	flush()
	return strings.Join(parts, ", ")
}

// Long enough to see a neighbouring compile, short enough not to pad every run.
const (
	quietWindow    = 300 * time.Millisecond
	quietThreshold = 0.20
)

func warnIfBusy(pin bench.Pin) {
	busy, err := pin.CheckQuiet(quietWindow, quietThreshold)
	if err != nil {
		return
	}
	for _, b := range busy {
		what := "the pinned cpu"
		if b.CPU != pin.CPU {
			what = "an SMT sibling of the pinned cpu"
		}
		fmt.Fprintf(os.Stderr, "%s cpu%d (%s) is %.0f%% busy; measurements taken now will be biased\n",
			yellow("warning:"), b.CPU, what, b.Frac*100)
	}
}
