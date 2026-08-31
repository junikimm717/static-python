package config

import (
	"slices"
	"strings"
	"testing"
)

func hasFlag(flags []string, want string) bool {
	return slices.Contains(flags, want)
}

func TestNomimallocKeepsPythonLTO(t *testing.T) {
	c := loadEmbedded(t)
	for _, profile := range []string{"nomimalloc", "nolto-nomimalloc"} {
		skip, err := c.PackageSkipped("mimalloc", profile)
		if err != nil {
			t.Fatal(err)
		}
		if !skip {
			t.Errorf("%s: mimalloc should be skipped", profile)
		}
	}

	lto, err := c.Resolve("nomimalloc", ScopePython)
	if err != nil {
		t.Fatal(err)
	}
	if !hasFlag(lto.CFlags, "-flto=auto") {
		t.Errorf("nomimalloc python cflags missing -flto=auto: %v", lto.CFlags)
	}

	nolto, err := c.Resolve("nolto-nomimalloc", ScopePython)
	if err != nil {
		t.Fatal(err)
	}
	if hasFlag(nolto.CFlags, "-flto=auto") {
		t.Errorf("nolto-nomimalloc python cflags still have -flto=auto: %v", nolto.CFlags)
	}
}

func TestReferenceMimallocIsHostBuiltSharedAndUnskipped(t *testing.T) {
	c := loadEmbedded(t)
	for _, profile := range []string{"reference-mimalloc", "reference-nolto-mimalloc"} {
		skip, err := c.PackageSkipped("mimalloc", profile)
		if err != nil {
			t.Fatal(err)
		}
		if skip {
			t.Errorf("%s: mimalloc should not be skipped", profile)
		}
		r, err := c.Resolve(profile, ScopePython)
		if err != nil {
			t.Fatal(err)
		}
		if !r.HostBuilt() {
			t.Errorf("%s toolchain = %q, want host", profile, r.Toolchain)
		}
		for _, bad := range []string{"-static", "--static"} {
			if hasFlag(r.LDFlags, bad) {
				t.Errorf("%s ldflags carry %q", profile, bad)
			}
		}
		for _, name := range []string{"openssl", "libffi"} {
			pkg, err := c.PackageFor(name, profile)
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(pkg.Configure, " ")
			if name == "openssl" && !strings.Contains(joined, "shared") {
				t.Errorf("%s openssl configure = %v, want shared", profile, pkg.Configure)
			}
			if name == "libffi" && strings.Contains(joined, "--disable-shared") {
				t.Errorf("%s libffi configure still disables shared: %v", profile, pkg.Configure)
			}
		}
	}
}

func TestReferenceNoltoDropsWithLTOKeepsMimallocSkip(t *testing.T) {
	c := loadEmbedded(t)
	r, err := c.Resolve("reference-nolto", ScopePython)
	if err != nil {
		t.Fatal(err)
	}
	if !r.HostBuilt() {
		t.Fatalf("reference-nolto toolchain = %q, want host", r.Toolchain)
	}
	if withLTOForTest(r) {
		t.Errorf("reference-nolto: LTOSet=%v LTO=%v, want withLTO false", r.LTOSet, r.LTO)
	}
	skip, err := c.PackageSkipped("mimalloc", "reference-nolto")
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Error("reference-nolto: mimalloc should still be skipped")
	}
}

func TestReferenceKeepsDefaultLTOAndSkipsMimalloc(t *testing.T) {
	c := loadEmbedded(t)
	r, err := c.Resolve("reference", ScopePython)
	if err != nil {
		t.Fatal(err)
	}
	if !withLTOForTest(r) {
		t.Errorf("reference: LTOSet=%v LTO=%v, want withLTO true (unset or true)", r.LTOSet, r.LTO)
	}
	skip, err := c.PackageSkipped("mimalloc", "reference")
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Error("reference: mimalloc should be skipped")
	}
}

func TestLTOUnsetIsOmittedFromKey(t *testing.T) {
	c := loadEmbedded(t)
	for _, profile := range []string{"default", "reference"} {
		r, err := c.Resolve(profile, ScopePython)
		if err != nil {
			t.Fatal(err)
		}
		if r.LTOSet {
			t.Errorf("%s: LTOSet true; hashing lto would move existing keys", profile)
		}
		if _, ok := r.KeyInputs()["lto"]; ok {
			t.Errorf("%s python keyInputs includes lto, which would move existing keys", profile)
		}
		if _, ok := r.KeyInputs()["lto_mode"]; ok {
			t.Errorf("%s python keyInputs includes lto_mode, which would move existing keys", profile)
		}
		deps, err := c.Resolve(profile, ScopeDeps)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := deps.KeyInputs()["lto"]; ok {
			t.Errorf("%s deps keyInputs includes lto", profile)
		}
		if _, ok := deps.KeyInputs()["lto_mode"]; ok {
			t.Errorf("%s deps keyInputs includes lto_mode, which would move existing keys", profile)
		}
	}
	r, err := c.Resolve("reference-nolto", ScopePython)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := r.KeyInputs()["lto"]; !ok || got != "false" {
		t.Errorf("reference-nolto python keyInputs lto = %q ok=%v, want false", got, ok)
	}
}

func TestBenchPinsArePresent(t *testing.T) {
	c := loadEmbedded(t)
	if c.Bench.Pyperformance == "" || c.Bench.Pyperf == "" {
		t.Errorf("bench pins empty: pyperformance=%q pyperf=%q", c.Bench.Pyperformance, c.Bench.Pyperf)
	}
}

func TestSepltoKeepsPythonLTOAndHashesModeOnDeps(t *testing.T) {
	c := loadEmbedded(t)
	py, err := c.Resolve("seplto", ScopePython)
	if err != nil {
		t.Fatal(err)
	}
	if !hasFlag(py.CFlags, "-flto=auto") {
		t.Errorf("seplto python cflags missing -flto=auto: %v", py.CFlags)
	}
	if py.LTOMode != LTOModePerDep {
		t.Errorf("seplto python LTOMode = %q, want %s", py.LTOMode, LTOModePerDep)
	}
	def, err := c.Resolve("default", ScopePython)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(py.CFlags, " ") != strings.Join(def.CFlags, " ") {
		t.Errorf("seplto python cflags diverged from default:\n  seplto %v\n  default %v", py.CFlags, def.CFlags)
	}

	deps, err := c.Resolve("seplto", ScopeDeps)
	if err != nil {
		t.Fatal(err)
	}
	if got := deps.KeyInputs()["lto_mode"]; got != LTOModePerDep {
		t.Errorf("seplto deps keyInputs lto_mode = %q, want %s", got, LTOModePerDep)
	}
	mi, err := c.Resolve("seplto", ScopeDeps+".mimalloc")
	if err != nil {
		t.Fatal(err)
	}
	if hasFlag(mi.CFlags, "-flto=auto") {
		t.Errorf("seplto mimalloc still has -flto=auto: %v", mi.CFlags)
	}

	skip, err := c.PackageSkipped("mimalloc", "seplto")
	if err != nil {
		t.Fatal(err)
	}
	if skip {
		t.Error("seplto: mimalloc should not be skipped")
	}
	skip, err = c.PackageSkipped("mimalloc", "seplto-nomimalloc")
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Error("seplto-nomimalloc: mimalloc should be skipped")
	}
}

func TestValidateRejectsUnknownLTOMode(t *testing.T) {
	c := loadEmbedded(t)
	p := c.Profiles["default"]
	p.LTOMode = "wpa"
	c.Profiles["default"] = p
	if err := c.Validate(); err == nil {
		t.Fatal("want error for unknown lto_mode")
	}
}

func TestValidateRejectsLTOModeOnHostBuilt(t *testing.T) {
	c := loadEmbedded(t)
	p := c.Profiles["reference"]
	p.LTOMode = LTOModePerDep
	c.Profiles["reference"] = p
	if err := c.Validate(); err == nil {
		t.Fatal("want error for lto_mode on a host-built profile")
	}
}

// Mirrors recipe.withLTO so the config tests do not import recipe.
func withLTOForTest(r Resolved) bool {
	return !r.LTOSet || r.LTO
}
