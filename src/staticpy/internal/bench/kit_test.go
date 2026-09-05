package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestPipInstallArgsFromVendorsFindLinks(t *testing.T) {
	args := PipInstallArgsFrom(DefaultPins(), "/kit/vendor")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--no-deps", "--no-index", "--find-links", "/kit/vendor", "pyperformance==1.14.0", "pyperf==2.10.0"} {
		if !strings.Contains(joined, want) {
			t.Errorf("PipInstallArgsFrom missing %q: %v", want, args)
		}
	}
}
