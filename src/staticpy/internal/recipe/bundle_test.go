package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/gen"
)

func TestPlanBundleUnknown(t *testing.T) {
	cfg := loadEmbedded(t)
	if _, err := planBundle(cfg, "does-not-exist"); err == nil {
		t.Fatal("unknown bundle accepted")
	}
}

func TestPlanBundleDemoRewritesModules(t *testing.T) {
	cfg := loadEmbedded(t)
	p, err := planBundle(cfg, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "demo" || len(p.Packages) != 5 {
		t.Fatalf("plan = %+v", p)
	}
	var names []string
	for _, m := range p.Modules {
		names = append(names, m.Name)
		for _, src := range m.Sources {
			if !strings.HasPrefix(src, "_bundle/") {
				t.Errorf("source %q was not rewritten", src)
			}
		}
	}
	joined := strings.Join(names, " ")
	if !strings.Contains(joined, "markupsafe._speedups") || !strings.Contains(joined, "ciso8601") {
		t.Fatalf("modules = %s", joined)
	}
	if !containsAll(p.Imports, "six", "idna", "charset_normalizer", "markupsafe", "markupsafe._speedups", "ciso8601") {
		t.Fatalf("imports = %v", p.Imports)
	}
	if len(p.Trees) != len(p.Packages) {
		t.Fatalf("trees %d != packages %d", len(p.Trees), len(p.Packages))
	}
}

func TestEmptyBundleDoesNotChangeSlug(t *testing.T) {
	cfg := loadEmbedded(t)
	bindFakeToolchain(t, testTriple)
	jobs, err := Plan(cfg, defaultsAssets(t), PlanOptions{
		Host: testTriple, Targets: []string{testTriple},
	})
	if err != nil {
		t.Fatal(err)
	}
	if jobs[0].Slug() != "pynative:default:"+testTriple {
		t.Fatalf("empty bundle slug = %s", jobs[0].Slug())
	}
}

func TestBundledPlanUsesDistinctSlugs(t *testing.T) {
	cfg := loadEmbedded(t)
	bindFakeToolchain(t, testTriple)
	plain, err := Plan(cfg, defaultsAssets(t), PlanOptions{
		Host: testTriple, Targets: []string{testTriple},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundled, err := Plan(cfg, defaultsAssets(t), PlanOptions{
		Host: testTriple, Targets: []string{testTriple},
		Bundle: "demo", Verify: "smoke", Pack: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundled[0].Slug() != "pack:default:"+testTriple+":demo" {
		t.Fatalf("pack slug = %s", bundled[0].Slug())
	}
	var interp, verify core.Job
	for _, d := range bundled[0].Deps() {
		switch d.Name() {
		case "pynative":
			interp = d
		case "verify":
			verify = d
		}
	}
	if interp == nil || interp.Slug() != "pynative:default:"+testTriple+":demo" {
		t.Fatalf("interp slug = %v", slugOf(interp))
	}
	if verify == nil || verify.Slug() != "verify:default:"+testTriple+":smoke:demo" {
		t.Fatalf("verify slug = %v", slugOf(verify))
	}
	if plain[0].Slug() == interp.Slug() {
		t.Fatal("bundled interpreter reused the unbundled slug")
	}
	foundTree := false
	for _, d := range interp.Deps() {
		if strings.HasPrefix(d.Slug(), "srctree:six-") {
			foundTree = true
		}
	}
	if !foundTree {
		t.Fatalf("pynative deps missing six srctree: %s", strings.Join(slugs(interp.Deps()), " "))
	}
}

func TestInstallPurePathAndShims(t *testing.T) {
	tree := t.TempDir()
	if err := os.WriteFile(filepath.Join(tree, "six.py"), []byte("x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tree, "src", "markupsafe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "src", "markupsafe", "__init__.py"), []byte("from ._speedups import x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stage := t.TempDir()
	site := filepath.Join(stage, "lib", "python3.14", "site-packages")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installPurePath(tree, site, "six.py"); err != nil {
		t.Fatal(err)
	}
	if err := installPurePath(tree, site, "src/markupsafe"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(site, "six.py")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(site, "markupsafe", "__init__.py")); err != nil {
		t.Fatal(err)
	}

	cfg := loadEmbedded(t)
	p, err := planBundle(cfg, "speedups")
	if err != nil {
		t.Fatal(err)
	}
	shims, err := gen.Shims(p.Modules)
	if err != nil {
		t.Fatal(err)
	}
	if err := gen.WriteShims(site, shims); err != nil {
		t.Fatal(err)
	}
	shim, err := os.ReadFile(filepath.Join(site, "markupsafe", "_speedups.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shim), "_markupsafe__speedups") {
		t.Fatalf("shim = %s", shim)
	}
	init, err := os.ReadFile(filepath.Join(site, "markupsafe", "__init__.py"))
	if err != nil {
		t.Fatal(err)
	}
	if string(init) != "from ._speedups import x\n" {
		t.Fatalf("real __init__.py overwritten: %q", init)
	}
}

func containsAll(have []string, want ...string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

func slugOf(j core.Job) string {
	if j == nil {
		return "<nil>"
	}
	return j.Slug()
}
