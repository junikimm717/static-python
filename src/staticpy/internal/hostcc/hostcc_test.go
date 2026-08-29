package hostcc

import (
	"context"
	"testing"
)

// The key is what a job caches on, so an unstable one would rebuild the
// reference interpreter on every invocation and a too-stable one would serve
// an artifact built by a compiler that is no longer installed.
func TestIdentifyIsStableAndPopulated(t *testing.T) {
	if _, err := SupportedArch(); err != nil {
		t.Skip(err)
	}
	cc, err := Find()
	if err != nil {
		t.Skip(err)
	}
	ctx := context.Background()
	a, err := Identify(ctx, cc)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	b, err := Identify(ctx, cc)
	if err != nil {
		t.Fatalf("Identify (second): %v", err)
	}
	if a.Key != b.Key {
		t.Errorf("key is not stable across calls: %s != %s", a.Key, b.Key)
	}
	if len(a.Key) != 64 {
		t.Errorf("key %q is not a sha256", a.Key)
	}
	for _, f := range []struct{ name, got string }{
		{"Version", a.Version}, {"Machine", a.Machine},
		{"Triple", a.Triple}, {"Libc", a.Libc},
	} {
		if f.got == "" {
			t.Errorf("%s is empty", f.name)
		}
	}
	if a.Libc == "unknown libc" {
		t.Errorf("libc not identified from the macro dump")
	}
	t.Logf("%s", a.Describe())
	t.Logf("triple = %s", a.Triple)
}

func TestLibcOf(t *testing.T) {
	for _, tc := range []struct {
		name   string
		macros []string
		want   string
	}{
		{"glibc", []string{"#define __GLIBC__ 2", "#define __GLIBC_MINOR__ 43"}, "glibc 2.43"},
		{"musl", []string{"#define __musl__ 1"}, "musl"},
		{"neither", []string{"#define __linux__ 1"}, "unknown libc"},
		// A major with no minor is not a version; reporting "glibc 2." would
		// put a malformed string in the manifest.
		{"partial", []string{"#define __GLIBC__ 2"}, "unknown libc"},
	} {
		if got := libcOf(tc.macros); got != tc.want {
			t.Errorf("%s: libcOf = %q, want %q", tc.name, got, tc.want)
		}
	}
}
