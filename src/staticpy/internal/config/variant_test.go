package config

import "testing"

func base() *Config {
	return &Config{
		Sources:  map[string]Source{"libffi": {Name: "libffi"}},
		Profiles: map[string]Profile{"default": {}, "reference": {Inherit: "default"}, "refchild": {Inherit: "reference"}},
		Packages: map[string]Package{"libffi": {
			Name:      "libffi",
			Configure: []string{"--enable-static", "--disable-shared"},
			Provides:  []string{"lib/libffi.a"},
		}},
	}
}

func TestPackageForWithoutVariantIsUnchanged(t *testing.T) {
	c := base()
	got, err := c.PackageFor("libffi", "reference")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Configure) != 2 || got.Configure[0] != "--enable-static" {
		t.Errorf("configure = %v, want the package as written", got.Configure)
	}
}

func TestPackageForAppliesVariant(t *testing.T) {
	c := base()
	p := c.Packages["libffi"]
	p.Variants = map[string]PackageVariant{"reference": {
		Configure: []string{"--disable-static", "--enable-shared"},
		Provides:  []string{"lib/libffi.so"},
	}}
	c.Packages["libffi"] = p

	ref, err := c.PackageFor("libffi", "reference")
	if err != nil {
		t.Fatal(err)
	}
	if got := ref.Configure; len(got) != 2 || got[0] != "--disable-static" {
		t.Errorf("reference configure = %v, want the variant", got)
	}
	if got := ref.Provides; len(got) != 1 || got[0] != "lib/libffi.so" {
		t.Errorf("reference provides = %v, want the variant", got)
	}
	// The variant table itself must not reach the job, or every other
	// profile's overrides would land in this job's key.
	if ref.Variants != nil {
		t.Errorf("variants leaked into the resolved package")
	}

	// A profile with no variant is untouched, so adding one cannot move an
	// existing artifact's key.
	def, err := c.PackageFor("libffi", "default")
	if err != nil {
		t.Fatal(err)
	}
	if def.Configure[0] != "--enable-static" {
		t.Errorf("default configure = %v, want the original", def.Configure)
	}
}

func TestPackageForInheritsVariantFromParentProfile(t *testing.T) {
	c := base()
	p := c.Packages["libffi"]
	p.Variants = map[string]PackageVariant{"reference": {Configure: []string{"--enable-shared"}}}
	c.Packages["libffi"] = p

	got, err := c.PackageFor("libffi", "refchild")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Configure) != 1 || got.Configure[0] != "--enable-shared" {
		t.Errorf("configure = %v, want the parent profile's variant", got.Configure)
	}
}

func TestValidateRejectsVariantOnUnknownProfile(t *testing.T) {
	c := base()
	p := c.Packages["libffi"]
	p.Variants = map[string]PackageVariant{"refrence": {Configure: []string{"-x"}}}
	c.Packages["libffi"] = p
	if err := c.validatePackages(); err == nil {
		t.Fatal("a variant naming a misspelled profile was accepted; it would silently do nothing")
	}
}

func TestValidateRejectsEmptyVariant(t *testing.T) {
	c := base()
	p := c.Packages["libffi"]
	p.Variants = map[string]PackageVariant{"reference": {}}
	c.Packages["libffi"] = p
	if err := c.validatePackages(); err == nil {
		t.Fatal("a variant overriding nothing was accepted")
	}
}

func TestValidateRejectsUnknownToolchain(t *testing.T) {
	c := base()
	c.Profiles["reference"] = Profile{Inherit: "default", Toolchain: "hsot"}
	if err := c.validateProfiles(); err == nil {
		t.Fatal("an unknown toolchain source was accepted")
	}
}

func TestResolveDefaultsToProvisionedAndHonoursHost(t *testing.T) {
	c := base()
	r, err := c.Resolve("default", ScopeDeps)
	if err != nil {
		t.Fatal(err)
	}
	if r.Toolchain != ToolchainProvisioned {
		t.Errorf("toolchain = %q, want %q by default", r.Toolchain, ToolchainProvisioned)
	}
	if r.HostBuilt() {
		t.Errorf("default profile reports host-built")
	}
	c.Profiles["reference"] = Profile{Inherit: "default", Toolchain: ToolchainHost}
	if r, err = c.Resolve("reference", ScopeDeps); err != nil {
		t.Fatal(err)
	}
	if !r.HostBuilt() {
		t.Errorf("toolchain = %q, want host to be inherited by Resolve", r.Toolchain)
	}
}
