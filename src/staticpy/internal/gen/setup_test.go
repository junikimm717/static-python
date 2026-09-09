package gen

import (
	"strings"
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

// makesetup carries the last mode tag forward, and the test section ends in a
// *disabled* block, so an appended line inherits it. Getting this wrong disables
// staticapi and breaks ctypes at runtime rather than at build time.
func TestStaticAPIStaysStatic(t *testing.T) {
	for _, testModules := range []bool{false, true} {
		b, err := SetupLocal(config.Resolved{Modules: "full", TestModules: testModules}, nil)
		if err != nil {
			t.Fatalf("test_modules=%t: %v", testModules, err)
		}
		mode := ""
		found := false
		for _, line := range strings.Split(string(b), "\n") {
			switch s := strings.TrimSpace(line); {
			case s == "*static*", s == "*shared*", s == "*disabled*":
				mode = s
			case strings.HasPrefix(s, "staticapi "):
				found = true
				if mode != "*static*" {
					t.Errorf("test_modules=%t: staticapi is under %s, want *static*", testModules, mode)
				}
			}
		}
		if !found {
			t.Errorf("test_modules=%t: no staticapi line at all", testModules)
		}
	}
}

func TestSetupLocalBundledModulesStayStatic(t *testing.T) {
	b, err := SetupLocal(config.Resolved{Modules: "full", TestModules: true}, []config.PyModule{
		{Name: "markupsafe._speedups", Sources: []string{"_bundle/markupsafe/src/markupsafe/_speedups.c"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mode := ""
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		switch s := strings.TrimSpace(line); {
		case s == "*static*", s == "*shared*", s == "*disabled*":
			mode = s
		case strings.HasPrefix(s, "_markupsafe__speedups "):
			found = true
			if mode != "*static*" {
				t.Fatalf("bundled module is under %s, want *static*", mode)
			}
		}
	}
	if !found {
		t.Fatal("bundled module line missing")
	}
}

func TestRewriteModuleRenamesDottedInit(t *testing.T) {
	got, files, err := RewriteModule("markupsafe", config.PyModule{
		Name:    "markupsafe._speedups",
		Sources: []string{"src/markupsafe/_speedups.c"},
		CFlags:  []string{"-DMARKUPSAFE=1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 1 || got.Sources[0] != "_bundle/markupsafe/_w_src_markupsafe__speedups.c" {
		t.Fatalf("sources = %v", got.Sources)
	}
	if len(got.CFlags) != 1 || got.CFlags[0] != "-I$(srcdir)/Modules/_bundle/markupsafe/src/markupsafe" {
		t.Fatalf("cflags = %v", got.CFlags)
	}
	if len(files) != 1 {
		t.Fatalf("wrappers = %d", len(files))
	}
	body := string(files[0].Content)
	for _, want := range []string{
		"#define PyInit__speedups PyInit__markupsafe__speedups",
		"#define MARKUPSAFE 1",
		`#include "src/markupsafe/_speedups.c"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("wrapper missing %q:\n%s", want, body)
		}
	}
	line, err := moduleLine(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(line, "=") {
		t.Fatalf("Setup line still has =: %q", line)
	}
	if !strings.HasPrefix(line, "_markupsafe__speedups ") {
		t.Fatalf("line = %q", line)
	}
}

func TestRewriteModuleLeavesMatchingInit(t *testing.T) {
	got, files, err := RewriteModule("ciso8601", config.PyModule{
		Name:    "ciso8601",
		Sources: []string{"module.c", "timezone.c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("unexpected wrappers %#v", files)
	}
	if got.Sources[0] != "_bundle/ciso8601/module.c" {
		t.Fatalf("sources = %v", got.Sources)
	}
}

func TestModuleLineRejectsEquals(t *testing.T) {
	_, err := moduleLine(config.PyModule{
		Name: "x", Sources: []string{"x.c"}, CFlags: []string{"-DFOO=1"},
	})
	if err == nil {
		t.Fatal("accepted = in Setup line")
	}
}

func TestRenderSymbolsUndefsMacroWrappedFuncs(t *testing.T) {
	got, err := renderSymbols([]abiItem{
		{Name: "Py_GetVersion", Kind: "function", Declared: true},
		{Name: "Py_PACK_FULL_VERSION", Kind: "function", Declared: true, MacroHidesFunc: true},
	}, "3.14.7", "deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "#undef Py_PACK_FULL_VERSION\n") {
		t.Fatalf("missing #undef:\n%s", s)
	}
	if strings.Contains(s, "#undef Py_GetVersion") {
		t.Fatal("undef'd a name that is not a hiding macro")
	}
}
