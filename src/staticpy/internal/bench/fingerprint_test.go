package bench

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFingerprintRecordsCPUMicrocodeSMTAndVulns(t *testing.T) {
	proc := t.TempDir()
	sys := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(proc, "cpuinfo"), ""+
		"vendor_id\t: GenuineIntel\n"+
		"cpu family\t: 6\n"+
		"model\t\t: 183\n"+
		"model name\t: Test CPU\n"+
		"stepping\t: 2\n"+
		"microcode\t: 0xabcd\n"+
		"flags\t\t: fpu avx2 hypervisor\n"+
		"bugs\t\t: spectre_v1 spectre_v2\n")
	write(filepath.Join(proc, "meminfo"), "MemTotal: 2048000 kB\nMemAvailable: 1024000 kB\nSwapTotal: 0 kB\n")
	write(filepath.Join(proc, "cmdline"), "BOOT_IMAGE=/vmlinuz mitigations=off isolcpus=3")
	write(filepath.Join(proc, "version"), "Linux version 6.12.0 (gcc)")
	write(filepath.Join(proc, "loadavg"), "0.10 0.20 0.30 1/100 1")
	write(filepath.Join(sys, "devices/system/cpu/smt/active"), "1\n")
	write(filepath.Join(sys, "devices/system/cpu/smt/control"), "on\n")
	write(filepath.Join(sys, "devices/system/cpu/cpu0/microcode/version"), "0xabcd\n")
	write(filepath.Join(sys, "devices/system/cpu/cpu0/topology/thread_siblings_list"), "0-1\n")
	write(filepath.Join(sys, "devices/system/cpu/cpu0/cache/index0/level"), "1\n")
	write(filepath.Join(sys, "devices/system/cpu/cpu0/cache/index0/type"), "Data\n")
	write(filepath.Join(sys, "devices/system/cpu/cpu0/cache/index0/size"), "48K\n")
	write(filepath.Join(sys, "devices/system/cpu/vulnerabilities/spectre_v2"), "Mitigation: Enhanced IBRS\n")
	write(filepath.Join(sys, "devices/system/cpu/vulnerabilities/meltdown"), "Not affected\n")
	write(filepath.Join(sys, "devices/system/clocksource/clocksource0/current_clocksource"), "tsc\n")

	m := readMachine(procFS{proc: proc, sys: sys, dockerenv: filepath.Join(proc, "nope")})
	if m.CPUModel != "Test CPU" {
		t.Fatalf("CPUModel = %q", m.CPUModel)
	}
	if m.Fingerprint == nil {
		t.Fatal("nil fingerprint")
	}
	fp := m.Fingerprint
	if fp.CPU.Microcode != "0xabcd" || fp.CPU.MicrocodeSysfs != "0xabcd" {
		t.Fatalf("microcode cpuinfo=%q sysfs=%q", fp.CPU.Microcode, fp.CPU.MicrocodeSysfs)
	}
	if fp.CPU.Bugs != "spectre_v1 spectre_v2" {
		t.Fatalf("bugs = %q", fp.CPU.Bugs)
	}
	if !strings.Contains(fp.CPU.Flags, "avx2") {
		t.Fatalf("flags = %q", fp.CPU.Flags)
	}
	if fp.SMT.Active != "1" || fp.SMT.Control != "on" || fp.SMT.ThreadsPerCore != 2 {
		t.Fatalf("smt = %+v", fp.SMT)
	}
	if fp.Vulnerabilities["spectre_v2"] != "Mitigation: Enhanced IBRS" {
		t.Fatalf("vulns = %v", fp.Vulnerabilities)
	}
	if !strings.Contains(fp.Kernel.Cmdline, "mitigations=off") {
		t.Fatalf("cmdline = %q", fp.Kernel.Cmdline)
	}
	if fp.Kernel.Clocksource != "tsc" {
		t.Fatalf("clocksource = %q", fp.Kernel.Clocksource)
	}
	if fp.SHA256 == "" {
		t.Fatal("empty fingerprint sha256")
	}

	man := Manifest("stamp", "reference", Pins{}, nil, nil)
	m.SetRunPlacement("pinned to cpu1", "4 logical cpus, uniform")
	m.AttachToManifest(man)
	if man["fingerprint_sha256"] != m.Fingerprint.SHA256 {
		t.Fatalf("manifest digest %v vs %s", man["fingerprint_sha256"], m.Fingerprint.SHA256)
	}
	fp2, ok := man["fingerprint"].(*Fingerprint)
	if !ok || fp2.Telemetry == nil || fp2.Telemetry.Affinity != "pinned to cpu1" {
		t.Fatalf("manifest fingerprint = %#v", man["fingerprint"])
	}
	before := m.Fingerprint.SHA256
	m.SetRunPlacement("pinned to cpu2", "moved")
	if m.Fingerprint.SHA256 != before {
		t.Fatalf("pin change moved fingerprint sha %s -> %s", before, m.Fingerprint.SHA256)
	}
	if vulnSummary(fp.Vulnerabilities) == "" {
		t.Fatal("empty vuln summary")
	}
}

func TestFingerprintSealStableAcrossReseal(t *testing.T) {
	f := &Fingerprint{CPU: CPUInfo{ModelName: "x"}, Kernel: KernelInfo{Release: "1"}}
	f.seal()
	a := f.SHA256
	f.seal()
	if f.SHA256 != a || a == "" {
		t.Fatalf("digest moved %q -> %q", a, f.SHA256)
	}
}

func TestFingerprintHashIgnoresTelemetry(t *testing.T) {
	f := &Fingerprint{
		CPU:    CPUInfo{ModelName: "x", Microcode: "0x1"},
		Kernel: KernelInfo{Release: "1"},
		Telemetry: &Telemetry{
			CapturedUTC: "2020-01-01T00:00:00Z",
			CPUMHz:      "1000",
			Loadavg1:    "0.10",
		},
	}
	f.seal()
	a := f.SHA256
	f.Telemetry.CapturedUTC = "2026-08-31T00:00:00Z"
	f.Telemetry.CPUMHz = "5400"
	f.Telemetry.Loadavg1 = "12.0"
	f.Telemetry.Affinity = "pinned to cpu7"
	f.seal()
	if f.SHA256 != a {
		t.Fatalf("telemetry moved digest %s -> %s", a, f.SHA256)
	}
	f.CPU.Microcode = "0x2"
	f.seal()
	if f.SHA256 == a {
		t.Fatal("identity change left digest unmoved")
	}
}

func TestReadMachineLiveLinuxFingerprint(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fingerprint sources are Linux sysfs")
	}
	m := ReadMachine()
	if m.Fingerprint == nil || m.Fingerprint.SHA256 == "" {
		t.Fatal("live fingerprint missing sha256")
	}
	if m.Fingerprint.CPU.ModelName == "" && m.CPUModel == "unknown" {
		t.Fatal("live cpu model empty")
	}
	if len(m.Fingerprint.Vulnerabilities) == 0 {
		t.Fatal("live vulnerabilities map empty; /sys/devices/system/cpu/vulnerabilities should exist")
	}
}
