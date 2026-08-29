package bench

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// Exec is the subset of core.Runner that bench needs, so every command it
// spawns lands in dist/logs and in commands.sh.
type Exec interface {
	Run(ctx context.Context, c core.Cmd) error
	Output(ctx context.Context, c core.Cmd) (string, error)
}

// Venv is one interpreter's normalised environment.
type Venv struct {
	Label  string
	Python string
	Dir    string
}

// A venv equalises what the arms disagree on: the pyperf version they import,
// whether distro site-packages (and the .pth files that execute at import) are
// on the path, and the length of sys.path itself, which is a real cost in
// import-heavy benchmarks. --without-pip because a --with-ensurepip=no
// interpreter has none, and pyperf is pure Python so it needs no installer.
func MakeVenv(ctx context.Context, x Exec, label, interp, root, pyperfSrc string) (*Venv, error) {
	dir := filepath.Join(root, label)
	if err := x.Run(ctx, core.Cmd{
		Dir:  root,
		Args: []string{interp, "-m", "venv", "--without-pip", dir},
		Name: "venv-" + label,
	}); err != nil {
		return nil, fmt.Errorf("%s: cannot create a venv (needs the venv module): %w", label, err)
	}
	py := filepath.Join(dir, "bin", "python3")
	if _, err := os.Stat(py); err != nil {
		return nil, fmt.Errorf("%s: venv produced no bin/python3", label)
	}
	site, err := sitePackages(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	if pyperfSrc != "" {
		if err := copyTree(pyperfSrc, filepath.Join(site, filepath.Base(pyperfSrc))); err != nil {
			return nil, fmt.Errorf("%s: install pyperf: %w", label, err)
		}
	}
	return &Venv{Label: label, Python: py, Dir: dir}, nil
}

// Env is the environment every measurement runs under. Cleared rather than
// inherited: PYTHONPATH or PYTHONHOME leaking in from the caller's shell would
// apply to some arms and not others.
func (v *Venv) Env() map[string]string {
	return map[string]string{
		"PYTHONNOUSERSITE":        "1",
		"PYTHONPATH":              "",
		"PYTHONHOME":              "",
		"PYTHONDONTWRITEBYTECODE": "",
	}
}

func sitePackages(venvDir string) (string, error) {
	libDir := filepath.Join(venvDir, "lib")
	ents, err := os.ReadDir(libDir)
	if err != nil {
		return "", err
	}
	for _, e := range ents {
		if e.IsDir() && strings.HasPrefix(e.Name(), "python") {
			p := filepath.Join(libDir, e.Name(), "site-packages")
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("no site-packages under %s", libDir)
}

// FindPyperf locates an importable pyperf package to copy into each venv.
func FindPyperf(hint string) (string, error) {
	if hint != "" {
		if filepath.Base(hint) == "pyperf" {
			if _, err := os.Stat(filepath.Join(hint, "__init__.py")); err == nil {
				return hint, nil
			}
		}
		p := filepath.Join(hint, "pyperf")
		if _, err := os.Stat(filepath.Join(p, "__init__.py")); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("no pyperf package under %s", hint)
	}
	return "", fmt.Errorf("no pyperf package given; pass --pyperf <site-packages dir>")
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
