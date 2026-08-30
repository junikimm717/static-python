package sources

import (
	"testing"
	"testing/fstest"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

// The whole reason target_patches exists: a fix for one architecture must not
// reach any other architecture's key. If this stops holding, one arch-specific
// diff rebuilds every target -- which is what listing it in patches would do.
func TestTargetPatchesAreScopedToTheirTriple(t *testing.T) {
	src := config.Source{
		Name:    "libffi",
		Version: "1.0",
		TargetPatches: map[string][]string{
			"mips64-linux-musl": {"fix.diff"},
		},
	}
	assets := fstest.MapFS{
		PatchDir(src) + "/fix.diff": {Data: []byte("--- a\n+++ b\n")},
	}

	mine, err := TargetPatchSetHash(assets, src, "mips64-linux-musl")
	if err != nil {
		t.Fatal(err)
	}
	if mine == "none" {
		t.Fatal("the declared target got no patch hash")
	}
	for _, other := range []string{"x86_64-linux-musl", "s390x-linux-musl"} {
		got, err := TargetPatchSetHash(assets, src, other)
		if err != nil {
			t.Fatal(err)
		}
		if got != "none" {
			t.Errorf("%s: got %s, want none", other, got)
		}
	}

	// The shared tree is keyed off Patches alone, so it must not move either.
	shared, err := PatchSetHash(assets, src)
	if err != nil {
		t.Fatal(err)
	}
	if shared != "none" {
		t.Errorf("srctree hash moved to %s; target patches leaked into the shared tree", shared)
	}
}

func TestTargetPatchSetHashTracksContent(t *testing.T) {
	src := config.Source{
		Name:          "libffi",
		Version:       "1.0",
		TargetPatches: map[string][]string{"mips64-linux-musl": {"fix.diff"}},
	}
	hash := func(body string) string {
		h, err := TargetPatchSetHash(fstest.MapFS{
			PatchDir(src) + "/fix.diff": {Data: []byte(body)},
		}, src, "mips64-linux-musl")
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	if hash("one") == hash("two") {
		t.Error("editing a diff in place left the key where it was")
	}
}
