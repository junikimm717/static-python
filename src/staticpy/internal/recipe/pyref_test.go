package recipe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

func TestWithLTO(t *testing.T) {
	tests := []struct {
		name string
		res  config.Resolved
		want bool
	}{
		{name: "unset is the recipe default", res: config.Resolved{}, want: true},
		{name: "explicit true", res: config.Resolved{LTO: true, LTOSet: true}, want: true},
		{name: "explicit false", res: config.Resolved{LTO: false, LTOSet: true}, want: false},
		{name: "false without LTOSet is unset", res: config.Resolved{LTO: false, LTOSet: false}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := withLTO(tt.res); got != tt.want {
				t.Errorf("withLTO(%+v) = %v, want %v", tt.res, got, tt.want)
			}
		})
	}
}

func TestWithLTOMatchesEmbeddedProfiles(t *testing.T) {
	cfg := loadEmbedded(t)
	for _, tt := range []struct {
		profile string
		want    bool
	}{
		{"reference", true},
		{"reference-mimalloc", true},
		{"reference-nolto", false},
		{"reference-nolto-mimalloc", false},
	} {
		r, err := cfg.Resolve(tt.profile, config.ScopePython)
		if err != nil {
			t.Fatal(err)
		}
		if got := withLTO(r); got != tt.want {
			t.Errorf("%s: withLTO = %v (LTOSet=%v LTO=%v), want %v",
				tt.profile, got, r.LTOSet, r.LTO, tt.want)
		}
	}
}

func TestSysrootObjectsCollectsOnlyDotO(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mimalloc.o", "libz.a", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(lib, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := sysrootObjects(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "mimalloc.o" {
		t.Errorf("sysrootObjects = %v, want only mimalloc.o", got)
	}
}

func TestSysrootObjectsMissingLibIsEmpty(t *testing.T) {
	got, err := sysrootObjects(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("empty rootfs sysrootObjects = %v, want nil/empty", got)
	}
}
