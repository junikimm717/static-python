package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestKitSessionKeepsExperimentIdentity(t *testing.T) {
	prev := buildinfo.GitRevision
	t.Cleanup(func() { buildinfo.GitRevision = prev })
	buildinfo.GitRevision = ""

	kitDir := t.TempDir()
	doc := KitDoc{
		Protocol:      Protocol,
		KitVersion:    "1",
		GitRevision:   "cafebabedeadbeefcafebabedeadbeefcafebabe",
		PythonVersion: "3.99.1",
		Triple:        "riscv64-linux-musl",
		Baseline:      "reference",
		Suite:         SuitePyperformance,
		Pins:          Pins{Pyperformance: "9.9.9", Pyperf: "8.8.8"},
		Arms: []KitArm{
			{Label: "default", Path: "python/default/bin/python3.99", BinarySHA256: "feedfacefeedfacefeedfacefeedfacefeedfacefeedfacefeedfacefeedface"},
			{Label: "reference", Path: "python/reference/bin/python3.99", BinarySHA256: "b10b00b5b10b00b5b10b00b5b10b00b5b10b00b5b10b00b5b10b00b5b10b00b5"},
		},
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kitDir, "kit.json"), append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	kit, err := LoadKit(kitDir)
	if err != nil {
		t.Fatal(err)
	}
	AdoptKitRevision(kit)

	parent := t.TempDir()
	sess, err := NewSessionIn(parent, "riscv64", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	md, _, err := sess.WriteReports(Reports{
		Accounting: Accounting{
			Baseline:   kit.Baseline,
			SuiteName:  SuitePyperformance,
			Pins:       kit.Pins,
			Kit:        kit,
			Identities: []Identity{{Label: "default", BinarySHA256: "aa"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	man, err := os.ReadFile(filepath.Join(sess.Dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(filepath.Join(sess.Dir, "report.html"))
	if err != nil {
		t.Fatal(err)
	}
	blob := string(man) + "\n" + md + "\n" + string(html)
	for _, want := range []string{
		kit.PythonVersion,
		kit.GitRevision,
		kit.Triple,
		kit.Pins.Pyperformance,
		kit.Pins.Pyperf,
		"feedfacefeedface",
	} {
		if !strings.Contains(blob, want) {
			t.Errorf("session lost %q", want)
		}
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
