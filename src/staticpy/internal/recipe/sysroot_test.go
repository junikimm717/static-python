package recipe

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

const testTriple = "x86_64-linux-musl"

func loadEmbedded(t *testing.T) *config.Config {
	t.Helper()
	c, err := config.Load(config.Options{})
	if err != nil {
		t.Fatalf("load embedded config: %v", err)
	}
	return c
}

func defaultsAssets(t *testing.T) fs.FS {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return os.DirFS(filepath.Join(filepath.Dir(file), "..", "config", "defaults"))
}

func bindFakeToolchain(t *testing.T, triple string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	tools := []string{"gcc", "cc", "g++", "c++", "gcc-ar", "ar", "gcc-ranlib", "ranlib", "gcc-nm", "nm", "objcopy", "ld"}
	for _, n := range tools {
		for _, name := range []string{n, triple + "-" + n} {
			p := filepath.Join(bin, name)
			if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gccfactory.json"), []byte(`{"key":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	Bind(&core.Env{
		Dist:      t.TempDir(),
		Overrides: map[string]string{triple: dir},
		Host:      triple,
	})
	t.Cleanup(func() { Bind(nil) })
}

func sysrootPackages(t *testing.T, cfg *config.Config, profile string) []string {
	t.Helper()
	target, err := cfg.Target(testTriple)
	if err != nil {
		t.Fatal(err)
	}
	j, err := Sysroot(cfg, defaultsAssets(t), target, profile)
	if err != nil {
		t.Fatalf("Sysroot(%s): %v", profile, err)
	}
	s := j.KeyInputs()["packages"]
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestSysrootOmitsSkippedMimalloc(t *testing.T) {
	cfg := loadEmbedded(t)
	bindFakeToolchain(t, testTriple)

	got := sysrootPackages(t, cfg, "nomimalloc")
	if containsName(got, "mimalloc") {
		t.Errorf("nomimalloc sysroot packages include mimalloc: %v", got)
	}

	def := sysrootPackages(t, cfg, "default")
	if !containsName(def, "mimalloc") {
		t.Errorf("default sysroot packages missing mimalloc: %v", def)
	}
}

func TestDepsOmitsSkippedMimalloc(t *testing.T) {
	cfg := loadEmbedded(t)
	bindFakeToolchain(t, testTriple)
	target, err := cfg.Target(testTriple)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := Deps(cfg, defaultsAssets(t), target, "nomimalloc")
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range jobs {
		if strings.HasSuffix(j.Slug(), ":mimalloc") {
			t.Errorf("Deps(nomimalloc) includes %s", j.Slug())
		}
	}
}
