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
