package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if Protocol != 2 {
		t.Fatalf("Protocol = %d, want 2", Protocol)
	}
	suite, ok := m["suite"].(map[string]string)
	if !ok {
		t.Fatalf("suite type %T", m["suite"])
	}
	if suite["name"] != SuitePyperformance {
		t.Fatalf("suite name = %q", suite["name"])
	}
	if suite["pyperformance"] != "1.14.0" || suite["pyperf"] != "2.10.0" {
		t.Fatalf("suite pins = %v", suite)
	}
}

func TestSuiteMapOmitsPinsForMicro(t *testing.T) {
	m := SuiteMap(SuiteMicro, Pins{})
	if m["name"] != SuiteMicro {
		t.Fatalf("name = %q", m["name"])
	}
	if _, ok := m["pyperformance"]; ok {
		t.Fatalf("micro must not claim pyperformance pins: %v", m)
	}
}

func TestEnvMarkdownNamesTheSuite(t *testing.T) {
	micro := EnvMarkdown(Machine{}, Protocol, DefaultPins(), SuiteMicro)
	if strings.Contains(micro, "pyperformance") {
		t.Fatalf("micro env claimed pyperformance:\n%s", micro)
	}
	if !strings.Contains(micro, "- suite: micro\n") {
		t.Fatalf("missing suite: micro\n%s", micro)
	}
	py := EnvMarkdown(Machine{}, Protocol, DefaultPins(), SuitePyperformance)
	if !strings.Contains(py, "pyperformance 1.14.0") {
		t.Fatalf("pyperformance env:\n%s", py)
	}
}

func TestWriteReportsRecordsKitAndPythonVersion(t *testing.T) {
	parent := t.TempDir()
	sess, err := NewSession(parent, "x86_64", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	kit := &KitDoc{
		Protocol:      Protocol,
		KitVersion:    "1",
		GitRevision:   "70987be221b480dd5d9c969edcb59a5cf8203546",
		PythonVersion: "3.13.13",
		Triple:        "x86_64-linux-musl",
		Baseline:      "reference",
		Suite:         SuiteMicro,
		Pins:          DefaultPins(),
		Arms:          []KitArm{{Label: "reference", Path: "python/reference/bin/python3.13"}},
	}
	md, _, err := sess.WriteReports(Reports{
		Accounting: Accounting{
			Baseline:      "reference",
			SuiteName:     SuiteMicro,
			Machine:       Machine{Kernel: "Linux"},
			Kit:           kit,
			PythonVersion: "3.13.13",
			Identities:    []Identity{{Label: "reference", BinarySHA256: "aa"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(sess.Dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{`"python_version": "3.13.13"`, `"kit_version": "1"`, `"triple": "x86_64-linux-musl"`, `"git_revision": "70987be221b480dd5d9c969edcb59a5cf8203546"`, `"kit"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("manifest missing %s:\n%s", want, s)
		}
	}
	for _, want := range []string{"python_version: 3.13.13", "git_revision: 70987be221b480dd5d9c969edcb59a5cf8203546", "kit_version: 1", "triple: x86_64-linux-musl"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %s:\n%s", want, md)
		}
	}
}

func TestWriteReportsEmitsTheStandardFiles(t *testing.T) {
	parent := t.TempDir()
	sess, err := NewSession(parent, "x86_64", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	md, report, err := sess.WriteReports(Reports{
		Accounting: Accounting{
			Baseline:  "reference",
			SuiteName: SuiteMicro,
			Machine:   Machine{Kernel: "Linux"},
			Identities: []Identity{
				{Label: "reference", BinarySHA256: "aa"},
			},
		},
		Order: []string{"reference", "default"},
		Rows: []Row{
			{Benchmark: "fib_iter", Min: map[string]float64{"reference": 2e-8, "default": 1e-8},
				Ratio: map[string]float64{"reference": 1, "default": 2}},
		},
		Geomean: map[string]float64{"default": 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range SessionFiles {
		p := filepath.Join(sess.Dir, name)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if !strings.Contains(md, "# micro comparison") || strings.Contains(md, "pyperformance") {
		t.Fatalf("markdown:\n%s", md)
	}
	suite, _ := report["suite"].(map[string]string)
	if suite["name"] != SuiteMicro {
		t.Fatalf("report suite = %v", report["suite"])
	}
	raw, err := os.ReadFile(filepath.Join(sess.Dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"name": "micro"`) {
		t.Fatalf("manifest:\n%s", raw)
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
			{Label: "reference", BinarySHA256: "aaaaaaaaaaaa", Size: 10, Factors: &Factors{Linkage: "dynamic"}},
			{Label: "default", BinarySHA256: "bbbbbbbbbbbb", Size: 20, Factors: &Factors{Linkage: "static"}},
		},
		Skipped: 2,
	}
	html := r.HTML()
	for _, want := range []string{"reference", "default", "nolto", "<svg", "skipped.json", "protocol", "pyperformance comparison"} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML missing %q:\n%s", want, html)
		}
	}
	md := r.Markdown()
	if !strings.Contains(md, "memory:") || !strings.Contains(md, "protocol:") {
		t.Fatalf("markdown env block too thin:\n%s", md)
	}
}

func TestManifestGainsFingerprintWhenAttached(t *testing.T) {
	m := Machine{Fingerprint: &Fingerprint{CPU: CPUInfo{ModelName: "x"}}}
	m.Fingerprint.seal()
	man := Manifest("s", "reference", Pins{}, nil, nil)
	m.AttachToManifest(man)
	if man["fingerprint_sha256"] != m.Fingerprint.SHA256 || man["fingerprint"] == nil {
		t.Fatalf("manifest = %#v", man)
	}
}
