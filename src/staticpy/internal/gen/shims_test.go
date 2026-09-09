package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

func TestShimsSkipNonDotted(t *testing.T) {
	got, err := Shims([]config.PyModule{{Name: "ciso8601"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("top-level module produced shims: %#v", got)
	}
}

func TestShimsDottedWritesPackageAndFile(t *testing.T) {
	got, err := Shims([]config.PyModule{{Name: "markupsafe._speedups"}})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]Shim{}
	for _, s := range got {
		byPath[s.Path] = s
	}
	if _, ok := byPath["markupsafe/__init__.py"]; !ok {
		t.Fatalf("missing package init: %v", keys(byPath))
	}
	file, ok := byPath["markupsafe/_speedups.py"]
	if !ok {
		t.Fatalf("missing shim file: %v", keys(byPath))
	}
	src := string(file.Content)
	if !strings.Contains(src, "import sys, _markupsafe__speedups") {
		t.Fatalf("shim does not import flattened builtin:\n%s", src)
	}
	if !strings.Contains(src, `sys.modules["markupsafe._speedups"] = _markupsafe__speedups`) {
		t.Fatalf("shim does not rebind sys.modules:\n%s", src)
	}
}

func TestWriteShimsKeepsRealPackageInit(t *testing.T) {
	dir := t.TempDir()
	shims, err := Shims([]config.PyModule{{Name: "markupsafe._speedups"}})
	if err != nil {
		t.Fatal(err)
	}
	real := []byte("from . import _native\n")
	if err := WriteShims(dir, []Shim{{
		Path: "markupsafe/__init__.py", Content: real, Package: false,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := WriteShims(dir, shims); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "markupsafe", "__init__.py"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(real) {
		t.Fatalf("package init overwritten: %q", got)
	}
	if _, err := os.ReadFile(filepath.Join(dir, "markupsafe", "_speedups.py")); err != nil {
		t.Fatal(err)
	}
}

func TestShimsRejectDuplicate(t *testing.T) {
	_, err := Shims([]config.PyModule{
		{Name: "pkg.mod"},
		{Name: "pkg.mod"},
	})
	if err == nil {
		t.Fatal("duplicate accepted")
	}
}

func keys(m map[string]Shim) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
