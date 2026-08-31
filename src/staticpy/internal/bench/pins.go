package bench

// Must match config/defaults/bench.toml. That file is the source of truth
// once loaded; these exist so tests and an empty BenchConfig still pin the
// same versions rather than installing whatever PyPI calls latest.
const (
	DefaultPyperformance = "1.14.0"
	DefaultPyperf        = "2.10.0"
)

// DefaultAblation is --interp ablation's expansion when cfg.Bench.Ablation
// is empty. Order matches bench.toml so a hardcoded lineup and a
// config-backed one produce the same columns.
var DefaultAblation = []string{
	"reference",
	"reference-nolto",
	"reference-mimalloc",
	"reference-nolto-mimalloc",
	"default",
	"nolto",
	"nomimalloc",
	"nolto-nomimalloc",
}

// AblationSentinel is the --interp value that expands after config load.
// Set cannot expand it: the flag parser has no config.
const AblationSentinel = "ablation"

// Pins are the suite versions recorded on every session manifest.
type Pins struct {
	Pyperformance string   `json:"pyperformance"`
	Pyperf        string   `json:"pyperf"`
	Ablation      []string `json:"ablation,omitempty"`
}

func DefaultPins() Pins {
	return Pins{
		Pyperformance: DefaultPyperformance,
		Pyperf:        DefaultPyperf,
		Ablation:      append([]string(nil), DefaultAblation...),
	}
}

func (p Pins) withDefaults() Pins {
	if p.Pyperformance == "" {
		p.Pyperformance = DefaultPyperformance
	}
	if p.Pyperf == "" {
		p.Pyperf = DefaultPyperf
	}
	if len(p.Ablation) == 0 {
		p.Ablation = append([]string(nil), DefaultAblation...)
	}
	return p
}

// PyperformanceSpecs returns the pip requirement strings. Empty versions
// fall back to the defaults so tests can assert the pin without a config.
func PyperformanceSpecs(pyperformance, pyperf string) []string {
	p := Pins{Pyperformance: pyperformance, Pyperf: pyperf}.withDefaults()
	return []string{
		"pyperformance==" + p.Pyperformance,
		"pyperf==" + p.Pyperf,
	}
}

// PipInstallArgs is the pip argv that installs the suite, still --no-deps:
// pyperformance depends on psutil, a C extension that will not load here.
func PipInstallArgs(pins Pins) []string {
	return append([]string{"install", "--quiet", "--no-deps"},
		PyperformanceSpecs(pins.Pyperformance, pins.Pyperf)...)
}

// ExpandInterps replaces the ablation sentinel with the named lineup, in
// listed order. Other labels pass through. Duplicates are dropped so
// `--interp ablation --interp reference` does not measure reference twice.
func ExpandInterps(labels, ablation []string) []string {
	if len(ablation) == 0 {
		ablation = DefaultAblation
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		names := []string{l}
		if l == AblationSentinel {
			names = ablation
		}
		for _, n := range names {
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}
