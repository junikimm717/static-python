package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/core"
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
	// The benchmark directory, which is where its requirements live.
	Dir string
	// The required positional sub-benchmark, for the handful of scripts that
	// take one. Empty for the rest.
	Sub string
}

// Matches the line argparse prints under "positional arguments:" for a
// required choice, e.g. {shortest_path,connected_components}.
var positionalChoices = regexp.MustCompile(`\{([A-Za-z0-9_,]+)\}`)

// Label is what the report calls this case.
func (c Case) Label() string {
	if c.Sub == "" {
		return c.Name
	}
	return c.Name + "[" + c.Sub + "]"
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
		s.Cases = append(s.Cases, Case{Name: e.Name(), Script: script, Dir: dir})
	}
	sort.Slice(s.Cases, func(i, j int) bool { return s.Cases[i].Name < s.Cases[j].Name })
	sort.Strings(skipped)
	if len(s.Cases) == 0 {
		return nil, skipped, fmt.Errorf("no runnable benchmarks under %s", root)
	}
	return s, skipped, nil
}

// --affinity is passed on top of the inherited CPU mask: pyperf re-pins its
// worker processes and would otherwise undo the caller's choice.
func (c Case) Args(interp, out string, pin Pin) []string {
	args := []string{interp, c.Script}
	if c.Sub != "" {
		args = append(args, c.Sub)
	}
	args = append(args, "-o", out, "--quiet")
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

// Installing it here is what makes pyperformance the default rather than
// something a caller has to locate and pass in. It needs the network exactly
// once per run; an explicit --pyperformance directory or an offline bench skips
// it, and offline says so instead of failing on a PyPI timeout.
//
// Every arm is installed into, not just the one whose copy supplies the
// benchmark scripts: the scripts are ordinary pyperf programs, so the
// interpreter running one has to be able to import pyperf itself. Installing
// into a single venv leaves every other arm failing at the first import, and
// the ratios are then computed over whichever benchmarks happened to survive.
func Bootstrap(ctx context.Context, x Exec, venvs []*Venv, offline bool) (string, error) {
	if len(venvs) == 0 {
		return "", fmt.Errorf("bootstrap: no venvs to install pyperformance into")
	}
	if offline {
		return "", fmt.Errorf("--offline: pyperformance cannot be installed without the network.\n" +
			"Pass --pyperformance DIR to use a copy already on disk, or --suite micro for the built-in benchmarks")
	}
	for _, v := range venvs {
		if err := installPyperformance(ctx, x, v); err != nil {
			return "", err
		}
	}
	return locateBenchmarks(ctx, x, venvs[0])
}

func installPyperformance(ctx context.Context, x Exec, v *Venv) error {
	if !v.HasPip(ctx, x) {
		return fmt.Errorf("%s: this interpreter's venv has no pip, so pyperformance cannot be installed.\n"+
			"It needs the ensurepip module and its bundled wheel; a build configured with --without-ensurepip still has both", v.Label)
	}
	// --no-deps, and pyperf named explicitly: pyperformance depends on psutil,
	// which is a C extension. It fails to build here and would be unloadable
	// anyway, and nothing in this path needs it -- pyperformance is wanted for
	// its data-files/benchmarks, and pyperf runs fine without psutil. Each
	// benchmark's own requirements are installed separately, where a C
	// extension that genuinely matters fails against the benchmark that needs
	// it rather than against the whole suite.
	if err := v.Pip(ctx, x, "install-pyperformance",
		"install", "--quiet", "--no-deps", "pyperformance", "pyperf"); err != nil {
		return fmt.Errorf("%s: installing pyperformance: %w", v.Label, err)
	}
	return nil
}

func locateBenchmarks(ctx context.Context, x Exec, v *Venv) (string, error) {
	out, err := x.Output(ctx, core.Cmd{
		Dir:  v.Dir,
		Args: []string{v.Python, "-c", "import pyperformance,pathlib;print(pathlib.Path(pyperformance.__file__).parent/'data-files'/'benchmarks')"},
		Name: "locate-pyperformance-" + v.Label,
	})
	if err != nil {
		return "", fmt.Errorf("%s: pyperformance installed but its benchmarks directory could not be located: %w", v.Label, err)
	}
	root := strings.TrimSpace(out)
	if _, err := os.Stat(root); err != nil {
		return "", fmt.Errorf("%s: pyperformance reported %s, which does not exist: %w", v.Label, root, err)
	}
	return root, nil
}

// Long enough for a pure-Python sdist to build, far short of a Rust toolchain
// getting anywhere.
const installTimeout = 90 * time.Second

// pyperformance's own runner installs each benchmark's dependencies per
// benchmark, and not doing the same is why bm_2to3 died on
// `pip install .../bm_2to3/vendor`: it vendors its dependency in a directory
// rather than naming it in requirements.txt, so a scan of that file cannot see
// it coming. Installing for real removes the guesswork -- and a dependency that
// ships a C extension fails here, on the interpreter that cannot load one,
// which is the honest place for it to fail.
func InstallRequirements(ctx context.Context, x Exec, v *Venv, c Case) error {
	// A deadline rather than --only-binary=:all:, which was tried first and
	// costs real benchmarks: pyaes 1.6.1 is sdist-only and markupsafe 2.0.1
	// has no cp313 wheel, yet both are pure Python and run fine. What actually
	// needs stopping is a source build that takes minutes to produce an
	// extension a static interpreter cannot load anyway -- pydantic-core under
	// bm_fastapi. Bounding the time keeps the cheap sdists and drops those.
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	req := filepath.Join(c.Dir, "requirements.txt")
	if _, err := os.Stat(req); err == nil {
		if err := v.Pip(ctx, x, "reqs-"+c.Name,
			"install", "--quiet", "--prefer-binary", "-r", req); err != nil {
			return err
		}
	}
	// A vendored dependency is a directory pyperformance pip-installs by path.
	vendor := filepath.Join(c.Dir, "vendor")
	if fi, err := os.Stat(vendor); err == nil && fi.IsDir() {
		if err := v.Pip(ctx, x, "vendor-"+c.Name, "install", "--quiet", "--prefer-binary", vendor); err != nil {
			return err
		}
	}
	return nil
}

// A hardcoded list of the scripts taking a required positional argument was
// tried first and was wrong within the hour: it named bm_argparse,
// bm_async_tree and bm_pickle, and bm_networkx failed the same way on the next
// run. argparse already prints the answer under "positional arguments:", so
// reading it costs one --help per benchmark and cannot go stale when
// pyperformance adds another.
//
// Choosing the first variant is a choice, so Label reports it: bm_pickle[pickle],
// never a bare bm_pickle standing in for five different measurements.
func DetectSubBenchmarks(ctx context.Context, x Exec, python string, s *Suite) {
	for i := range s.Cases {
		out, err := x.Output(ctx, core.Cmd{
			Dir:  filepath.Dir(s.Cases[i].Script),
			Args: []string{python, s.Cases[i].Script, "--help"},
			Name: "help-" + s.Cases[i].Name,
		})
		if err != nil {
			continue
		}
		_, tail, ok := strings.Cut(out, "positional arguments:")
		if !ok {
			continue
		}
		if m := positionalChoices.FindStringSubmatch(tail); m != nil {
			s.Cases[i].Sub = strings.Split(m[1], ",")[0]
		}
	}
}
