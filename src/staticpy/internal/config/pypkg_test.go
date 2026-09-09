package config

import (
	"strings"
	"testing"
)

func TestEmbeddedBundlesLoad(t *testing.T) {
	c := loadEmbedded(t)
	if _, ok := c.Bundles["demo"]; !ok {
		t.Fatal("embedded config has no [bundle.demo]")
	}
	for _, name := range []string{"six", "idna", "charset_normalizer", "markupsafe", "ciso8601"} {
		p, ok := c.PyPackages[name]
		if !ok {
			t.Fatalf("missing [pkg.%s]", name)
		}
		if p.AsSource().SHA256 != p.SdistSHA256 {
			t.Errorf("%s: AsSource sha256 mismatch", name)
		}
	}
}

func TestPyPackageImportNamesDefault(t *testing.T) {
	p := PyPackage{
		Name: "markupsafe",
		Modules: []PyModule{
			{Name: "markupsafe._speedups"},
		},
	}
	got := strings.Join(p.ImportNames(), ",")
	if got != "markupsafe,markupsafe._speedups" {
		t.Fatalf("ImportNames = %q", got)
	}
	p.Imports = []string{"markupsafe"}
	if got := strings.Join(p.ImportNames(), ","); got != "markupsafe" {
		t.Fatalf("explicit Imports = %q", got)
	}
}

func TestRelPathRejectsEscapes(t *testing.T) {
	for _, p := range []string{"", "/etc/passwd", "../x", "foo/../../etc", `..\x`} {
		if err := RelPath(p); err == nil {
			t.Errorf("RelPath(%q) accepted", p)
		}
	}
	for _, p := range []string{"six.py", "src/idna", "src/markupsafe/_speedups.c"} {
		if err := RelPath(p); err != nil {
			t.Errorf("RelPath(%q) = %v", p, err)
		}
	}
}

func TestValidatePyPackageErrors(t *testing.T) {
	base := func() *Config {
		c := loadEmbedded(t)
		c.PyPackages = map[string]PyPackage{
			"six": {
				Name: "six", Version: "1", File: "six.tgz", TopDir: "six-1",
				SdistSHA256: strings.Repeat("a", 64),
				URLs:        []string{"https://example.test/six.tgz"},
				PurePaths:   []string{"six.py"},
			},
		}
		c.Bundles = map[string]Bundle{}
		return c
	}

	t.Run("empty", func(t *testing.T) {
		c := base()
		p := c.PyPackages["six"]
		p.PurePaths = nil
		c.PyPackages["six"] = p
		if err := c.validatePyPackages(); err == nil {
			t.Fatal("empty package accepted")
		}
	})
	t.Run("bad hash", func(t *testing.T) {
		c := base()
		p := c.PyPackages["six"]
		p.SdistSHA256 = "nope"
		c.PyPackages["six"] = p
		if err := c.validatePyPackages(); err == nil {
			t.Fatal("bad hash accepted")
		}
	})
	t.Run("escape", func(t *testing.T) {
		c := base()
		p := c.PyPackages["six"]
		p.PurePaths = []string{"../etc"}
		c.PyPackages["six"] = p
		if err := c.validatePyPackages(); err == nil {
			t.Fatal("escaped pure_paths accepted")
		}
	})
	t.Run("unknown need", func(t *testing.T) {
		c := base()
		p := c.PyPackages["six"]
		p.Needs = []string{"not-a-lib"}
		c.PyPackages["six"] = p
		if err := c.validatePyPackages(); err == nil {
			t.Fatal("unknown needs accepted")
		}
	})
	t.Run("source collision", func(t *testing.T) {
		c := base()
		p := c.PyPackages["six"]
		p.Name = "python"
		c.PyPackages = map[string]PyPackage{"python": p}
		if err := c.validatePyPackages(); err == nil {
			t.Fatal("collision with [source.python] accepted")
		}
	})
	t.Run("duplicate bundle package", func(t *testing.T) {
		c := base()
		c.Bundles["x"] = Bundle{Packages: []string{"six", "six"}}
		if err := c.validateBundles(); err == nil {
			t.Fatal("duplicate package accepted")
		}
	})
}
