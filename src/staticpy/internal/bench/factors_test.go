package bench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/buildinfo"
	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

func TestDeriveFactorsEmbeddedProfiles(t *testing.T) {
	cfg, err := config.Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		profile, lto, alloc, libc string
		pgo                       bool
		host                      bool
	}{
		{"default", LTOWholeGraph, AllocMimalloc, "musl", true, false},
		{"seplto", LTOPerDep, AllocMimalloc, "musl", true, false},
		{"nolto", LTONone, AllocMimalloc, "musl", true, false},
		{"nomimalloc", LTOWholeGraph, AllocMusl, "musl", true, false},
		{"seplto-nomimalloc", LTOPerDep, AllocMusl, "musl", true, false},
		{"nolto-nomimalloc", LTONone, AllocMusl, "musl", true, false},
		{"reference", LTOWholeGraph, AllocGlibc, "glibc", true, true},
		{"reference-nolto", LTONone, AllocGlibc, "glibc", true, true},
		{"reference-mimalloc", LTOWholeGraph, AllocMimalloc, "glibc", true, true},
		{"reference-nolto-mimalloc", LTONone, AllocMimalloc, "glibc", true, true},
		{"nopgo", LTOWholeGraph, AllocMimalloc, "musl", false, false},
	} {
		py, err := cfg.Resolve(tt.profile, config.ScopePython)
		if err != nil {
			t.Fatalf("%s: %v", tt.profile, err)
		}
		skip, err := cfg.PackageSkipped("mimalloc", tt.profile)
		if err != nil {
			t.Fatalf("%s skip: %v", tt.profile, err)
		}
		f := DeriveFactors(FactorOpts{
			HostBuilt:       py.HostBuilt(),
			LTOMode:         py.LTOMode,
			PythonCFlags:    py.CFlags,
			WithLTO:         !py.LTOSet || py.LTO,
			MimallocSkipped: skip,
			Libc:            tt.libc,
			PGO:             py.PGO,
			ELFLinkage:      map[bool]string{true: "dynamic", false: "static"}[tt.host],
		})
		if f.LTO != tt.lto || f.Allocator != tt.alloc || f.PGO != tt.pgo || f.Linkage != map[bool]string{true: "dynamic", false: "static"}[tt.host] {
			t.Errorf("%s: factors = %+v, want lto=%s alloc=%s pgo=%v", tt.profile, f, tt.lto, tt.alloc, tt.pgo)
		}
		if py.HostBuilt() != tt.host {
			t.Errorf("%s: HostBuilt=%v, want %v", tt.profile, py.HostBuilt(), tt.host)
		}
	}
}

func TestIdentityJSONUsesBinarySHA256(t *testing.T) {
	id := Identity{Label: "default", BinarySHA256: "abc", ArtifactKey: "def", Factors: &Factors{Linkage: "static", LTO: LTOWholeGraph}}
	b, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"binary_sha256":"abc"`, `"artifact_key":"def"`, `"linkage":"static"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
	if strings.Contains(s, `"sha256"`) {
		t.Errorf("legacy sha256 key still present: %s", s)
	}
}

func TestManifestRecordsGitRevision(t *testing.T) {
	prev := buildinfo.GitRevision
	buildinfo.GitRevision = "deadbeefcafebabe"
	defer func() { buildinfo.GitRevision = prev }()
	m := Manifest("stamp", "reference", Pins{}, nil, nil)
	if m["git_revision"] != "deadbeefcafebabe" {
		t.Fatalf("git_revision = %v", m["git_revision"])
	}
}

func TestArtifactKeyNearFindsJobStamp(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, core.ManifestName), []byte(`{"key":"abc123","slug":"pynative:default:x"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	py := filepath.Join(bin, "python3.13")
	if err := os.WriteFile(py, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := artifactKeyNear(py); got != "abc123" {
		t.Fatalf("artifactKeyNear = %q, want abc123", got)
	}
}

type fakeExec struct {
	out string
	err error
}

func (f fakeExec) Run(context.Context, core.Cmd) error { return f.err }
func (f fakeExec) Output(context.Context, core.Cmd) (string, error) {
	return f.out, f.err
}

type recordingExec struct {
	cmds []core.Cmd
}

func (r *recordingExec) Run(_ context.Context, c core.Cmd) error {
	r.cmds = append(r.cmds, c)
	return nil
}

func (r *recordingExec) Output(context.Context, core.Cmd) (string, error) { return "", nil }

func TestInstallRequirementsIsSoftFail(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("pyaes==1.6.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	x := &recordingExec{}
	v := &Venv{Label: "default", Python: "/nonexistent/python", Dir: dir}
	if err := InstallRequirements(context.Background(), x, v, Case{Name: "bm_x", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if len(x.cmds) != 1 || !x.cmds[0].SoftFail {
		t.Fatalf("requirement pip SoftFail = %+v", x.cmds)
	}
}

func TestInventoryPackagesParsesJSON(t *testing.T) {
	x := fakeExec{out: `{"pyperformance": "1.14.0", "pyperf": "2.10.0"}`}
	got, err := InventoryPackages(context.Background(), x, "/nonexistent/python")
	if err != nil {
		t.Fatal(err)
	}
	if got["pyperformance"] != "1.14.0" || got["pyperf"] != "2.10.0" {
		t.Fatalf("got %v", got)
	}
}
