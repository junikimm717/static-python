package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPyperformanceSpecsArePinned(t *testing.T) {
	joined := strings.Join(PipInstallArgs(Pins{}), " ")
	for _, want := range []string{"--no-deps", "pyperformance==1.14.0", "pyperf==2.10.0"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("PipInstallArgs missing %q: %s", want, joined)
		}
	}
	specs := PyperformanceSpecs("", "")
	if len(specs) != 2 || specs[0] != "pyperformance==1.14.0" || specs[1] != "pyperf==2.10.0" {
		t.Fatalf("PyperformanceSpecs = %v", specs)
	}
}

func TestManifestRecordsProtocol(t *testing.T) {
	m := Manifest("stamp", "reference", Pins{}, nil, nil)
	if m["protocol"] != Protocol {
		t.Fatalf("protocol = %v, want %d", m["protocol"], Protocol)
	}
	if Protocol != 1 {
		t.Fatalf("Protocol = %d, want 1", Protocol)
	}
	suite, ok := m["suite"].(map[string]string)
	if !ok {
		t.Fatalf("suite type %T", m["suite"])
	}
	if suite["pyperformance"] != "1.14.0" || suite["pyperf"] != "2.10.0" {
		t.Fatalf("suite pins = %v", suite)
	}
}

func TestMachineJSONIncludesMemory(t *testing.T) {
	m := Machine{Kernel: "Linux", CPUModel: "cpu", MemoryBytes: 4096, MemoryAvailableBytes: 1024}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"memory_bytes":4096`, `"memory_available_bytes":1024`, `"cpu_model":"cpu"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
}

func TestReadMachineParsesMeminfo(t *testing.T) {
	proc := t.TempDir()
	if err := os.WriteFile(filepath.Join(proc, "meminfo"), []byte("MemTotal:       2048000 kB\nMemAvailable:   1024000 kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "cpuinfo"), []byte("model name\t: Test CPU\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := readMachine(procFS{proc: proc, sys: filepath.Join(proc, "nosys"), dockerenv: filepath.Join(proc, "nope")})
	if m.MemoryBytes != 2048000*1024 {
		t.Fatalf("MemoryBytes = %d", m.MemoryBytes)
	}
	if m.MemoryAvailableBytes != 1024000*1024 {
		t.Fatalf("MemoryAvailableBytes = %d", m.MemoryAvailableBytes)
	}
	if m.CPUModel != "Test CPU" {
		t.Fatalf("CPUModel = %q", m.CPUModel)
	}
	if m.Memory == "" || m.MemoryAvailable == "" {
		t.Fatalf("missing human labels: %+v", m)
	}
}

func TestRenderHTMLContainsChartAndArms(t *testing.T) {
	r := SuiteReport{
		Baseline: "reference",
		Order:    []string{"reference", "default", "nolto"},
		Rows: []Row{
			{Benchmark: "bm_x", Ratio: map[string]float64{"reference": 1, "default": 1.2, "nolto": 0.9}},
		},
		Geomean: map[string]float64{"default": 1.2, "nolto": 0.9},
		Machine: Machine{Kernel: "Linux", CPUModel: "cpu", MemoryBytes: 1, Memory: "1 B"},
		Pins:    DefaultPins(),
		Identities: []Identity{
			{Label: "reference", SHA256: "aaaaaaaaaaaa", Linkage: "dynamic", Size: 10},
			{Label: "default", SHA256: "bbbbbbbbbbbb", Linkage: "static", Size: 20},
		},
		Skipped: 2,
	}
	html := r.HTML()
	for _, want := range []string{"reference", "default", "nolto", "<svg", "skipped.json", "protocol"} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML missing %q:\n%s", want, html)
		}
	}
	md := r.Markdown()
	if !strings.Contains(md, "memory:") || !strings.Contains(md, "protocol:") {
		t.Fatalf("markdown env block too thin:\n%s", md)
	}
}
