package recipe

import (
	"context"
	"debug/elf"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

func TestHostPublishPathsDoNotCollide(t *testing.T) {
	a := ToolchainID{Source: toolchainHost, Key: "aaaabbbbccccdddd"}
	b := ToolchainID{Source: toolchainHost, Key: "ffffeeee11112222"}
	if hostPublishSuffix(a) == hostPublishSuffix(b) {
		t.Fatal("different host keys share a suffix")
	}
	if hostPublishSuffix(ToolchainID{Source: toolchainVerified, Key: a.Key}) != "" {
		t.Fatal("provisioned toolchain got a host suffix")
	}
	e := &core.Env{Dist: t.TempDir()}
	ja := &pyRef{profile: "reference", target: config.Target{Triple: "x86_64-linux-musl"}, tc: a}
	jb := &pyRef{profile: "reference", target: config.Target{Triple: "x86_64-linux-musl"}, tc: b}
	if ja.ArtifactDir(e) == jb.ArtifactDir(e) {
		t.Fatal("two hostccs share ArtifactDir")
	}
	if !strings.HasSuffix(ja.ArtifactDir(e), hostPublishSuffix(a)) {
		t.Fatalf("ArtifactDir missing host suffix: %s", ja.ArtifactDir(e))
	}
}

func TestRejectForeignHostPrefix(t *testing.T) {
	dir := t.TempDir()
	if err := rejectForeignHostPrefix(dir, "aaaabbbbccccdddd"); err != nil {
		t.Fatalf("empty dir: %v", err)
	}
	m := &core.Manifest{Inputs: map[string]string{"toolchain": "ffffeeee11112222"}}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, core.ManifestName), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rejectForeignHostPrefix(dir, "aaaabbbbccccdddd"); err == nil {
		t.Fatal("accepted a prefix from another hostcc")
	}
	if err := rejectForeignHostPrefix(dir, "ffffeeee11112222"); err != nil {
		t.Fatalf("same hostcc: %v", err)
	}
}

func TestHostBuiltRpathUsesViewNotPublished(t *testing.T) {
	te := &toolenv{
		res:       config.Resolved{Toolchain: config.ToolchainHost},
		prefix:    "/published/rootfs",
		view:      []string{"/shadow/rootfs"},
		pcSysroot: "/shadow",
	}
	got := strings.Join(te.ldflags(), "\n")
	if !strings.Contains(got, "-Wl,-rpath,/shadow/rootfs/lib") {
		t.Fatalf("missing view rpath:\n%s", got)
	}
	if strings.Contains(got, "-Wl,-rpath,/published/") {
		t.Fatalf("rpath still names the published prefix:\n%s", got)
	}
}

func TestHostBuiltRpathFallsBackToPrefixWithoutView(t *testing.T) {
	te := &toolenv{
		res:    config.Resolved{Toolchain: config.ToolchainHost},
		prefix: "/published/rootfs",
	}
	got := strings.Join(te.ldflags(), "\n")
	if !strings.Contains(got, "-Wl,-rpath,/published/rootfs/lib") {
		t.Fatalf("missing prefix fallback rpath:\n%s", got)
	}
}

func TestOriginRunpath(t *testing.T) {
	for _, tt := range []struct {
		rel, want string
	}{
		{"bin/python3.13", "$ORIGIN/../lib:$ORIGIN/../lib64"},
		{"lib/libssl.so.3", "$ORIGIN:$ORIGIN/../lib64"},
		{"lib64/libffi.so.8", "$ORIGIN/../lib:$ORIGIN"},
		{"lib/python3.13/lib-dynload/_ssl.so", "$ORIGIN/../..:$ORIGIN/../../../lib64"},
	} {
		if got := originRunpath(tt.rel); got != tt.want {
			t.Errorf("originRunpath(%q) = %q, want %q", tt.rel, got, tt.want)
		}
	}
}

func TestPatchRpathShrinksBakedPrefix(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		cc, err = exec.LookPath("gcc")
	}
	if err != nil {
		t.Skip("no C compiler")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "x.c")
	if err := os.WriteFile(src, []byte("int x = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	so := filepath.Join(dir, "lib", "libx.so")
	if err := os.MkdirAll(filepath.Dir(so), 0o755); err != nil {
		t.Fatal(err)
	}
	baked := "/workspace/dist/artifacts/pyref_reference_x86_64-linux-gnu_0123456789abcdef/rootfs/lib"
	cmd := exec.Command(cc, "-shared", "-fPIC", "-o", so, src, "-Wl,-rpath,"+baked)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cc: %v\n%s", err, out)
	}
	if err := patchRpath(so, originRunpath("lib/libx.so")); err != nil {
		t.Fatal(err)
	}
	f, err := elf.Open(so)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got := ""
	for _, tag := range []elf.DynTag{elf.DT_RUNPATH, elf.DT_RPATH} {
		ss, err := f.DynString(tag)
		if err == nil && len(ss) > 0 {
			got = ss[0]
			break
		}
	}
	want := originRunpath("lib/libx.so")
	if got != want {
		t.Fatalf("runpath = %q, want %q", got, want)
	}
}

func TestRewriteRootfsRpathsWalksTree(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		cc, err = exec.LookPath("gcc")
	}
	if err != nil {
		t.Skip("no C compiler")
	}
	root := t.TempDir()
	src := filepath.Join(root, "x.c")
	if err := os.WriteFile(src, []byte("int x = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	so := filepath.Join(root, "bin", "tool")
	if err := os.MkdirAll(filepath.Dir(so), 0o755); err != nil {
		t.Fatal(err)
	}
	baked := "/opt/staticpy-very-long-prefix/rootfs/lib"
	cmd := exec.Command(cc, "-shared", "-fPIC", "-o", so, src, "-Wl,-rpath,"+baked)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cc: %v\n%s", err, out)
	}
	if err := rewriteRootfsRpaths(root); err != nil {
		t.Fatal(err)
	}
	f, err := elf.Open(so)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got := ""
	for _, tag := range []elf.DynTag{elf.DT_RUNPATH, elf.DT_RPATH} {
		ss, err := f.DynString(tag)
		if err == nil && len(ss) > 0 {
			got = ss[0]
			break
		}
	}
	if got != originRunpath("bin/tool") {
		t.Fatalf("runpath = %q, want %q", got, originRunpath("bin/tool"))
	}
}

func TestPackContentRootUsesPyrefRootfs(t *testing.T) {
	e := &core.Env{Dist: t.TempDir()}
	art := filepath.Join(e.Dist, "artifacts", "pyref_x")
	if err := os.MkdirAll(filepath.Join(art, "rootfs", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := packContentRoot(&fakeInterp{dir: art, name: "pyref"}, e)
	if got != filepath.Join(art, "rootfs") {
		t.Fatalf("packContentRoot = %q, want rootfs", got)
	}
	plain := t.TempDir()
	if got := packContentRoot(&fakeInterp{dir: plain, name: "pynative"}, e); got != plain {
		t.Fatalf("pynative packContentRoot = %q, want %q", got, plain)
	}
}

func TestPlanPacksHostBuilt(t *testing.T) {
	cfg := loadEmbedded(t)
	jobs, err := Plan(cfg, defaultsAssets(t), PlanOptions{
		Profile: config.ProfileReference,
		Host:    testTriple,
		Targets: []string{testTriple},
		Pack:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, j := range jobs {
		if j.Name() == "pack" {
			found = true
		}
	}
	if !found {
		t.Fatalf("plan for reference --pack has no pack job: %v", slugs(jobs))
	}
}

type fakeInterp struct {
	dir, name string
}

func (f *fakeInterp) Name() string                 { return f.name }
func (f *fakeInterp) Slug() string                 { return f.name }
func (f *fakeInterp) Deps() []core.Job             { return nil }
func (f *fakeInterp) KeyInputs() map[string]string { return nil }
func (f *fakeInterp) ArtifactDir(*core.Env) string { return f.dir }
func (f *fakeInterp) Build(context.Context, *core.Env, *core.Runner, string, string) error {
	return nil
}

func slugs(jobs []core.Job) []string {
	var out []string
	for _, j := range jobs {
		out = append(out, j.Slug())
	}
	return out
}
