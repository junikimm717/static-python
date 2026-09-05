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

// 3.14's Py_PACK_FULL_VERSION is a PyAPI_FUNC the public header then #defines
// as a macro. &name is a compile error unless we #undef first.
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
