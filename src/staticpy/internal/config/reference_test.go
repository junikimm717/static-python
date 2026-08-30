package config

import "strings"

import "testing"

func loadEmbedded(t *testing.T) *Config {
	t.Helper()
	c, err := Load(Options{})
	if err != nil {
		t.Fatalf("load embedded config: %v", err)
	}
	return c
}

// The reference profile is the one host-built profile, and the whole point of
// it is that it is not the static build with a flag flipped.
func TestReferenceProfileIsHostBuiltAndNotStatic(t *testing.T) {
	c := loadEmbedded(t)
	r, err := c.Resolve("reference", ScopePython)
	if err != nil {
		t.Fatal(err)
	}
	if !r.HostBuilt() {
		t.Fatalf("reference toolchain = %q, want %q", r.Toolchain, ToolchainHost)
	}
	for _, bad := range []string{"-static", "--static", "-no-pie"} {
		for _, f := range r.LDFlags {
			if f == bad {
				t.Errorf("reference ldflags carry %q; it would stop being a dynamic build", bad)
			}
		}
	}
}

// A variant must not disturb the profile it does not name: that is what keeps
// adding one from rebuilding the world.
func TestReferenceVariantsDoNotTouchDefault(t *testing.T) {
	c := loadEmbedded(t)
	for _, name := range []string{"openssl", "libffi", "xz", "zlib", "ncurses", "readline", "sqlite"} {
		def, err := c.PackageFor(name, "default")
		if err != nil {
			t.Fatal(err)
		}
		orig := c.Packages[name]
		if strings.Join(def.Configure, " ") != strings.Join(orig.Configure, " ") {
			t.Errorf("%s: default configure changed to %v", name, def.Configure)
		}
		if strings.Join(def.Provides, " ") != strings.Join(orig.Provides, " ") {
			t.Errorf("%s: default provides changed to %v", name, def.Provides)
		}
	}
}

// Every variant has to actually produce a shared object, or the reference
// interpreter links the static one and stops being dynamic without saying so.
func TestReferenceVariantsAreShared(t *testing.T) {
	c := loadEmbedded(t)
	for _, name := range []string{"libffi", "xz", "zlib", "ncurses", "readline", "sqlite"} {
		ref, err := c.PackageFor(name, "reference")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ref.Provides {
			if !strings.Contains(p, ".so") {
				t.Errorf("%s: reference provides %q, which is not a shared object", name, p)
			}
		}
		for _, f := range ref.Configure {
			if f == "--disable-shared" || f == "--without-shared" || f == "no-shared" {
				t.Errorf("%s: reference configure carries %q", name, f)
			}
		}
	}
	// openssl states its linkage as a bare word rather than a --flag.
	ssl, err := c.PackageFor("openssl", "reference")
	if err != nil {
		t.Fatal(err)
	}
	var shared bool
	for _, f := range ssl.Configure {
		if f == "no-shared" {
			t.Errorf("openssl reference configure still says no-shared")
		}
		if f == "shared" {
			shared = true
		}
	}
	if !shared {
		t.Errorf("openssl reference configure does not ask for shared")
	}
}

// The two hand-rolled build shapes cannot produce a .so yet. This test is the
// reminder: when a variant is added for either, the recipe has to grow a
// shared path first, and this expectation is what will fail.
func TestHandRolledShapesHaveNoReferenceVariantYet(t *testing.T) {
	c := loadEmbedded(t)
	for _, name := range []string{"bzip2", "libuuid"} {
		p, ok := c.Packages[name]
		if !ok {
			t.Fatalf("%s is gone from packages.toml", name)
		}
		if _, has := p.Variants["reference"]; has {
			t.Errorf("%s declares a reference variant, but build = %q archives by construction; "+
				"plainMake/fromSources need a shared output path first", name, p.Build)
		}
	}
}
