package cli

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/bench"
	"github.com/junikimm717/static-python/src/staticpy/internal/tui"
)

// cpuMenu renders the whole topology, not just the cores worth picking: the
// slow ones are shown disabled so the shape of the machine -- and the reason
// half of it is unsuitable -- is visible at the moment of choosing.
func cpuMenu(t *bench.Topology, recommended int) tui.Menu {
	fastest := map[int]bool{}
	for _, c := range t.Fastest() {
		fastest[c.ID] = true
	}
	byClass := map[int][]bench.CPU{}
	var classOrder []int
	for _, c := range t.CPUs {
		if _, seen := byClass[c.Class]; !seen {
			classOrder = append(classOrder, c.Class)
		}
		byClass[c.Class] = append(byClass[c.Class], c)
	}
	sortDescInt(classOrder)

	m := tui.Menu{
		Title:   "Which CPU should the benchmark run on?",
		Help:    "Every arm of the comparison is pinned to this one core.",
		Headers: []string{"cpu", "core", "class", "rank", "smt sibling"},
		Flag:    "--cpu",
		Default: strconv.Itoa(recommended),
	}
	for _, class := range classOrder {
		cpus := byClass[class]
		title := fmt.Sprintf("%s  (%d logical cpus)", bench.ClassLabel(class), len(cpus))
		g := tui.Group{Title: title}
		for _, c := range cpus {
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
			case !fastest[c.ID]:
				ch.Disabled = true
				ch.Why = "slower core type"
			case c.ID == recommended:
				ch.Note = "recommended"
			case c.ID == 0:
				ch.Note = "takes the most interrupts"
			}
			g.Choices = append(g.Choices, ch)
		}
		m.Groups = append(m.Groups, g)
	}
	return m
}

// choosePin resolves the CPU to pin to: the flag if given, else the menu, else
// the recommendation.
func choosePin(disabled bool, want int) (bench.Pin, *bench.Topology) {
	topo, err := bench.ReadTopology()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s cannot read cpu topology (%v); running unpinned\n", yellow("note:"), err)
		return bench.Pin{}, nil
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", bold("machine:"), topo.Describe())
	if disabled {
		if topo.Hybrid {
			fmt.Fprintf(os.Stderr, "%s --no-pin on a hybrid cpu: runs that migrate between core types are not comparable\n", yellow("warning:"))
		}
		return bench.Pin{}, topo
	}

	recommended, _, err := topo.PickCore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %v; running unpinned\n", yellow("note:"), err)
		return bench.Pin{}, topo
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
	}

	pin, err := topo.ApplyCPU(want)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %v; running unpinned\n", yellow("note:"), err)
		return pin, topo
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", bold("affinity:"), pin.Describe())
	if c, ok := topo.ByID(want); ok && c.Class != topo.Fastest()[0].Class {
		fmt.Fprintf(os.Stderr, "%s cpu%d is not in the fastest core class; absolute numbers will not be comparable to runs on one that is\n",
			yellow("warning:"), want)
	}
	warnIfBusy(pin)
	return pin, topo
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

func sortDescInt(xs []int) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] > xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
