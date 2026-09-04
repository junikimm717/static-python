package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

func TestDestDirTree(t *testing.T) {
	stage := "/tmp/stage"
	got := destDirTree(stage, "/usr")
	want := filepath.Join(stage, "usr")
	if got != want {
		t.Fatalf("destDirTree(%q, /usr) = %q, want %q", stage, got, want)
	}
	if got == "/usr" {
		t.Fatal("destDirTree must never return the host /usr")
	}
	if destDirTree(stage, "usr") != want {
		t.Fatalf("relative usr should join the same way")
	}
}

func TestConfigurePrefix(t *testing.T) {
	art := "/tmp/artifact"
	perDep := &depJob{res: config.Resolved{LTOMode: config.LTOModePerDep}}
	if got := perDep.configurePrefix(art); got != keepablePrefix {
		t.Fatalf("per-dep = %q, want %q", got, keepablePrefix)
	}
	host := &depJob{res: config.Resolved{LTOMode: config.LTOModePerDep, Toolchain: config.ToolchainHost}}
	if got := host.configurePrefix(art); got != art {
		t.Fatalf("host per-dep = %q, want artifact", got)
	}
	whole := &depJob{res: config.Resolved{LTOMode: config.LTOModeWholeGraph}}
	if got := whole.configurePrefix(art); got != art {
		t.Fatalf("whole-graph = %q, want artifact", got)
	}
}

func TestStripRecipeOwnedFlags(t *testing.T) {
	in := []string{"no-shared", "--prefix=/usr", "--openssldir=/etc/ssl", "--enable-static"}
	got := stripRecipeOwnedFlags(in)
	for _, a := range got {
		if strings.HasPrefix(a, "--prefix=") || strings.HasPrefix(a, "--openssldir=") {
			t.Fatalf("left recipe-owned flag %q", a)
		}
	}
	if strings.Join(got, " ") != "no-shared --enable-static" {
		t.Fatalf("got %v", got)
	}
}

func TestHoistDestdirFromUsr(t *testing.T) {
	stage := t.TempDir()
	lib := destDirTree(stage, "/usr")
	if err := os.MkdirAll(filepath.Join(lib, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lib, "lib", "libfoo.a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := hoistDestdir(stage, "/usr", "foo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stage, "lib", "libfoo.a")); err != nil {
		t.Fatalf("hoisted file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stage, "usr")); !os.IsNotExist(err) {
		t.Fatalf("stage/usr should have been removed, err=%v", err)
	}
}

func TestRewriteKeepableMetadata(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "lib", "pkgconfig")
	if err := os.MkdirAll(pc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "libfoo.pc"), []byte("prefix=/usr\nlibdir=/usr/lib\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ar := filepath.Join(root, "lib", "libfoo.a")
	if err := os.WriteFile(ar, []byte("binary /usr path"), 0o644); err != nil {
		t.Fatal(err)
	}
	art := "/workspace/dist/artifacts/dep_example"
	if err := rewriteKeepableMetadata(root, keepablePrefix, art); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(pc, "libfoo.pc"))
	if err != nil {
		t.Fatal(err)
	}
	want := "prefix=" + art + "\nlibdir=" + art + "/lib\n"
	if string(got) != want {
		t.Fatalf("pc = %q, want %q", got, want)
	}
	bin, err := os.ReadFile(ar)
	if err != nil {
		t.Fatal(err)
	}
	if string(bin) != "binary /usr path" {
		t.Fatalf(".a was rewritten: %q", bin)
	}
}

func TestSepltoPackagesDoNotSetPrefix(t *testing.T) {
	c, err := config.Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"openssl", "ncurses"} {
		pkg, err := c.PackageFor(name, "seplto")
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range pkg.Configure {
			if strings.HasPrefix(a, "--prefix=") ||
				strings.HasPrefix(a, "--exec-prefix=") ||
				strings.HasPrefix(a, "--openssldir=") {
				t.Errorf("%s seplto configure still has %s", name, a)
			}
		}
	}
	nc, err := c.PackageFor("ncurses", "default")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range nc.Configure {
		if a == "--disable-database" {
			found = true
		}
	}
	if !found {
		t.Error("default ncurses is missing --disable-database")
	}
}

func TestDepKeyInputsRecordKeepablePrefixAndOpenssldir(t *testing.T) {
	j := &depJob{
		name:         "openssl",
		pkg:          config.Package{Name: "openssl", Build: buildOpenSSL},
		src:          config.Source{Name: "openssl", Version: "1", SHA256: "abc"},
		target:       config.Target{Triple: "x86_64-linux-musl"},
		res:          config.Resolved{LTOMode: config.LTOModePerDep},
		patchHash:    "none",
		tgtPatchHash: "none",
	}
	in := j.KeyInputs()
	if in["configure_prefix"] != keepablePrefix {
		t.Errorf("configure_prefix = %q, want %q", in["configure_prefix"], keepablePrefix)
	}
	if in["openssldir"] != opensslCertDir {
		t.Errorf("openssldir = %q, want %q", in["openssldir"], opensslCertDir)
	}
	host := *j
	host.res.Toolchain = config.ToolchainHost
	hin := host.KeyInputs()
	if _, ok := hin["configure_prefix"]; ok {
		t.Error("host-built per-dep should not record keepable prefix")
	}
	if _, ok := hin["openssldir"]; ok {
		t.Error("host-built openssl should not record /etc/ssl")
	}
}
