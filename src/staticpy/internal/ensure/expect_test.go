package ensure

import (
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

func hasTest(entries []config.TestEntry, name string) bool {
	for _, e := range entries {
		if e.Test == name {
			return true
		}
	}
	return false
}

func TestLookupExpectOmitsStaticScopeWhenDynamic(t *testing.T) {
	all := map[string]config.TestExpect{
		ExpectAll:    {Skip: []config.TestEntry{{Test: "test_re"}}},
		ExpectStatic: {Skip: []config.TestEntry{{Test: "test_bytes"}}},
	}

	static := LookupExpect(all, "x86_64-linux-musl", "native", true)
	if !hasTest(static.Skip, "test_bytes") {
		t.Fatal("static lookup dropped [expect.static] test_bytes")
	}
	if !hasTest(static.Skip, "test_re") {
		t.Fatal("static lookup dropped [expect.all] test_re")
	}

	dyn := LookupExpect(all, "x86_64-linux-musl", "native", false)
	if hasTest(dyn.Skip, "test_bytes") {
		t.Fatal("dynamic lookup inherited [expect.static]; that is the unexpected-pass failure on reference")
	}
	if !hasTest(dyn.Skip, "test_re") {
		t.Fatal("dynamic lookup dropped [expect.all] test_re")
	}
}

func TestHostPublishTail(t *testing.T) {
	if got := hostPublishTail("/dist/artifacts/pyref_reference_x86_64-linux-musl_aaaabbbbcccc"); got != "_aaaabbbbcccc" {
		t.Fatalf("got %q", got)
	}
	if got := hostPublishTail("/dist/artifacts/pynative_default_x86_64-linux-musl"); got != "" {
		t.Fatalf("static dir got tail %q", got)
	}
}
