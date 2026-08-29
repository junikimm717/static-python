package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Suite struct {
	// The pyperformance data-files/benchmarks directory.
	Root  string
	Cases []Case
}

type Case struct {
	Name string
	// Absolute path to the benchmark's run_benchmark.py.
	Script string
}

// `pyperformance run` is deliberately not used: it builds a venv per benchmark
// and pip-installs its requirements, neither of which a --with-ensurepip=no
// static interpreter can do. The benchmark scripts are ordinary pyperf
// programs, so running them directly needs none of that.
//
// Skipped names are returned so the report can say what was left out instead
// of quietly narrowing its own scope.
func DiscoverSuite(root string) (*Suite, []string, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, fmt.Errorf("pyperformance benchmarks not found at %s: %w", root, err)
	}
	s := &Suite{Root: root}
	var skipped []string
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "bm_") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		script := filepath.Join(dir, "run_benchmark.py")
		if _, err := os.Stat(script); err != nil {
			continue
		}
		if extra, ok := needsWheels(filepath.Join(dir, "requirements.txt")); ok {
			skipped = append(skipped, e.Name()+" (needs "+extra+")")
			continue
		}
		s.Cases = append(s.Cases, Case{Name: e.Name(), Script: script})
	}
	sort.Slice(s.Cases, func(i, j int) bool { return s.Cases[i].Name < s.Cases[j].Name })
	sort.Strings(skipped)
	if len(s.Cases) == 0 {
		return nil, skipped, fmt.Errorf("no runnable benchmarks under %s", root)
	}
	return s, skipped, nil
}

// Names the first requirement we cannot provide, if any.
func needsWheels(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false // no requirements at all is the common case
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := strings.ToLower(strings.FieldsFunc(line, func(r rune) bool {
			return r == '=' || r == '>' || r == '<' || r == '[' || r == ';' || r == ' '
		})[0])
		if name != "pyperf" {
			return line, true
		}
	}
	return "", false
}

// --affinity is passed on top of the inherited CPU mask: pyperf re-pins its
// worker processes and would otherwise undo the caller's choice.
func (c Case) Args(interp, out string, pin Pin) []string {
	args := []string{interp, c.Script, "-o", out, "--quiet"}
	if pin.Applied {
		args = append(args, fmt.Sprintf("--affinity=%d", pin.CPU))
	}
	return args
}

type pyperfFile struct {
	Benchmarks []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Runs []struct {
			Values []float64 `json:"values"`
		} `json:"runs"`
	} `json:"benchmarks"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
}

// Keyed by pyperf's own benchmark name, not the file's: one file may hold
// several.
func ParseResult(path string) (map[string][]float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f pyperfFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	out := map[string][]float64{}
	for _, bm := range f.Benchmarks {
		name := bm.Metadata.Name
		if name == "" {
			name = f.Metadata.Name
		}
		if name == "" {
			continue
		}
		for _, r := range bm.Runs {
			out[name] = append(out[name], r.Values...)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no benchmark values", filepath.Base(path))
	}
	return out, nil
}
