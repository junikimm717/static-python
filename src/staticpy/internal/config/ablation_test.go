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
		deps, err := c.Resolve(profile, ScopeDeps)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := deps.KeyInputs()["lto"]; ok {
			t.Errorf("%s deps keyInputs includes lto", profile)
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

func TestAblationListIsExactlyTheEight(t *testing.T) {
	c := loadEmbedded(t)
	if !slices.Equal(c.Bench.Ablation, requiredAblation) {
		t.Errorf("bench.ablation = %v, want %v", c.Bench.Ablation, requiredAblation)
	}
	if c.Bench.Pyperformance == "" || c.Bench.Pyperf == "" {
		t.Errorf("bench pins empty: pyperformance=%q pyperf=%q", c.Bench.Pyperformance, c.Bench.Pyperf)
	}
	for _, name := range c.Bench.Ablation {
		if _, ok := c.Profiles[name]; !ok {
			t.Errorf("ablation names %q, which is not a profile", name)
		}
	}
}

// Mirrors recipe.withLTO so the config tests do not import recipe.
func withLTOForTest(r Resolved) bool {
	return !r.LTOSet || r.LTO
}
