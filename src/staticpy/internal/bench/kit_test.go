package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/buildinfo"
)

func TestLoadKitRoundTrip(t *testing.T) {
	dir := t.TempDir()
	doc := KitDoc{
		Protocol:      Protocol,
		KitVersion:    "1",
		PythonVersion: "3.14.0",
		Triple:        "x86_64-linux-musl",
		Baseline:      "reference",
		Pins:          DefaultPins(),
		Arms: []KitArm{
			{Label: "default", Path: "python/default/bin/python3.14"},
			{Label: "reference", Path: "python/reference/bin/python3.14"},
		},
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kit.json"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadKit(dir)
	if err != nil {
		t.Fatal(err)
	}
	order, paths, err := got.ResolveArms(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "default" || got.Baseline != "reference" {
		t.Fatalf("order=%v baseline=%s", order, got.Baseline)
	}
	if !strings.HasSuffix(paths["default"], "python/default/bin/python3.14") {
		t.Fatalf("path = %s", paths["default"])
	}
}

func TestLoadKitRejectsWrongProtocol(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kit.json"), []byte(`{"protocol":1,"baseline":"x","arms":[{"label":"x","path":"p"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKit(dir); err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("err = %v, want protocol mismatch", err)
	}
}

func TestAdoptKitRevisionFillsEmptyStamp(t *testing.T) {
	prev := buildinfo.GitRevision
	t.Cleanup(func() { buildinfo.GitRevision = prev })
	buildinfo.GitRevision = ""
	AdoptKitRevision(&KitDoc{GitRevision: "70987be221b480dd5d9c969edcb59a5cf8203546"})
	if buildinfo.GitRevision != "70987be221b480dd5d9c969edcb59a5cf8203546" {
		t.Fatalf("got %q", buildinfo.GitRevision)
	}
}

func TestAdoptKitRevisionLeavesExplicitStamp(t *testing.T) {
	prev := buildinfo.GitRevision
	t.Cleanup(func() { buildinfo.GitRevision = prev })
	buildinfo.GitRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	AdoptKitRevision(&KitDoc{GitRevision: "70987be221b480dd5d9c969edcb59a5cf8203546"})
	if buildinfo.GitRevision != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("overwrote stamp: %q", buildinfo.GitRevision)
	}
}

func TestAdoptKitRevisionIgnoresEmptyKit(t *testing.T) {
	prev := buildinfo.GitRevision
	t.Cleanup(func() { buildinfo.GitRevision = prev })
	buildinfo.GitRevision = ""
	AdoptKitRevision(&KitDoc{})
	AdoptKitRevision(nil)
	if buildinfo.GitRevision != "" {
		t.Fatalf("got %q", buildinfo.GitRevision)
	}
}

func TestApplyKitToManifestCopiesAllFields(t *testing.T) {
	kit := &KitDoc{
		Protocol:      Protocol,
		KitVersion:    "1",
		GitRevision:   "70987be221b480dd5d9c969edcb59a5cf8203546",
		PythonVersion: "3.13.13",
		Triple:        "x86_64-linux-musl",
		Baseline:      "reference",
		Suite:         SuitePyperformance,
		Pins:          DefaultPins(),
		Arms: []KitArm{
			{Label: "default", Path: "python/default/bin/python3.13", BinarySHA256: "aa"},
			{Label: "reference", Path: "python/reference/bin/python3.13", BinarySHA256: "bb"},
		},
	}
	man := Manifest("stamp", "reference", DefaultPins(), nil, nil)
	man["git_revision"] = "already-set"
	ApplyKitToManifest(man, kit)
	got, ok := man["kit"].(*KitDoc)
	if !ok || got.PythonVersion != "3.13.13" || len(got.Arms) != 2 {
		t.Fatalf("kit = %#v", man["kit"])
	}
	if man["python_version"] != "3.13.13" || man["kit_version"] != "1" || man["triple"] != "x86_64-linux-musl" {
		t.Fatalf("promoted = %v %v %v", man["python_version"], man["kit_version"], man["triple"])
	}
	if man["git_revision"] != "already-set" {
		t.Fatalf("overwrote git_revision: %v", man["git_revision"])
	}

	empty := Manifest("stamp", "reference", DefaultPins(), nil, nil)
	ApplyKitToManifest(empty, kit)
	if empty["git_revision"] != kit.GitRevision {
		t.Fatalf("git_revision = %v", empty["git_revision"])
	}
}

func TestPipInstallArgsFromVendorsFindLinks(t *testing.T) {
	args := PipInstallArgsFrom(DefaultPins(), "/kit/vendor")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--no-deps", "--no-index", "--find-links", "/kit/vendor", "pyperformance==1.14.0", "pyperf==2.10.0"} {
		if !strings.Contains(joined, want) {
			t.Errorf("PipInstallArgsFrom missing %q: %v", want, args)
		}
	}
}
