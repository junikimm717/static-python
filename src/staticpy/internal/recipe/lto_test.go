package recipe

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

func TestFindStaticArchives(t *testing.T) {
	stage := t.TempDir()
	mustWrite := func(rel string) {
		t.Helper()
		p := filepath.Join(stage, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("!"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("lib/libz.a")
	mustWrite("lib64/libcrypto.a")
	mustWrite("lib/libfoo.so")
	mustWrite("include/foo.h")

	got, err := findStaticArchives(stage)
	if err != nil {
		t.Fatal(err)
	}
	var bases []string
	for _, p := range got {
		bases = append(bases, filepath.Base(p))
	}
	slices.Sort(bases)
	want := []string{"libcrypto.a", "libz.a"}
	if !slices.Equal(bases, want) {
		t.Errorf("archives = %v, want %v", bases, want)
	}
}

func TestLTORelocArgs(t *testing.T) {
	flags := ltoRelocFlags(
		[]string{"-O3", "-flto=auto", "-flto-partition=none", "-fuse-linker-plugin"},
		[]string{"-static", "-flto=auto", "-Wl,--gc-sections"},
	)
	for _, bad := range []string{"-static", "-Wl,--gc-sections", "-O3"} {
		if slices.Contains(flags, bad) {
			t.Errorf("ltoRelocFlags kept %q: %v", bad, flags)
		}
	}
	got := ltoRelocArgs("gcc", "/tmp/out.o", "/stage/lib/libz.a", flags)
	joined := strings.Join(got, " ")
	for _, want := range []string{"-r", "-nostdlib", "-flto=auto", "-flinker-output=nolto-rel", "-Wl,--whole-archive"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q: %v", want, got)
		}
	}
}

func TestHasLTOCompile(t *testing.T) {
	if !hasLTOCompile([]string{"-O3", "-flto=auto"}) {
		t.Fatal("want true for -flto=auto")
	}
	if hasLTOCompile([]string{"-O3", "-fuse-linker-plugin"}) {
		t.Fatal("plugin alone is not compile-time LTO")
	}
}

func TestMaterializeArchivesNoopsWithoutMode(t *testing.T) {
	j := &depJob{res: config.Resolved{CFlags: []string{"-flto=auto"}}}
	if err := j.materializeArchives(nil, nil, nil, t.TempDir(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
