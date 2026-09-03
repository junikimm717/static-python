package bench

// Must match config/defaults/bench.toml. That file is the source of truth
// once loaded; these exist so tests and an empty BenchConfig still pin the
// same versions rather than installing whatever PyPI calls latest.
const (
	DefaultPyperformance = "1.14.0"
	DefaultPyperf        = "2.10.0"
)

// Pins are the suite versions recorded on every session manifest.
type Pins struct {
	Pyperformance string `json:"pyperformance"`
	Pyperf        string `json:"pyperf"`
}

func DefaultPins() Pins {
	return Pins{
		Pyperformance: DefaultPyperformance,
		Pyperf:        DefaultPyperf,
	}
}

func (p Pins) withDefaults() Pins {
	if p.Pyperformance == "" {
		p.Pyperformance = DefaultPyperformance
	}
	if p.Pyperf == "" {
		p.Pyperf = DefaultPyperf
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
	return PipInstallArgsFrom(pins, "")
}

func PipInstallArgsFrom(pins Pins, findLinks string) []string {
	args := []string{"install", "--quiet", "--no-deps"}
	if findLinks != "" {
		args = append(args, "--no-index", "--find-links", findLinks)
	}
	return append(args, PyperformanceSpecs(pins.Pyperformance, pins.Pyperf)...)
}
