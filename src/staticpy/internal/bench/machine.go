package bench

import (
	"fmt"
	"runtime"
	"strings"
)

// Machine is the hardware provenance written to every suite's env.json.
// Summary fields stay at the top so reports and fixtures stay readable;
// Fingerprint is the full host record, also copied onto manifest.json.
type Machine struct {
	Kernel               string       `json:"kernel"`
	CPUModel             string       `json:"cpu_model"`
	Cores                int          `json:"logical_cores"`
	MemoryBytes          int64        `json:"memory_bytes"`
	Memory               string       `json:"memory,omitempty"`
	MemoryAvailableBytes int64        `json:"memory_available_bytes,omitempty"`
	MemoryAvailable      string       `json:"memory_available,omitempty"`
	CacheL1d             string       `json:"cache_l1d"`
	CacheL1i             string       `json:"cache_l1i"`
	CacheL2              string       `json:"cache_l2"`
	CacheL3              string       `json:"cache_l3"`
	Container            bool         `json:"container"`
	Topology             string       `json:"topology,omitempty"`
	Affinity             string       `json:"affinity,omitempty"`
	Fingerprint          *Fingerprint `json:"fingerprint,omitempty"`
}

type procFS struct {
	proc      string
	sys       string
	dockerenv string
}

func ReadMachine() Machine {
	return readMachine(procFS{
		proc:      "/proc",
		sys:       "/sys",
		dockerenv: "/.dockerenv",
	})
}

// Missing /proc files yield "unknown" / 0 rather than failing the run:
// a measurement on a strange host is still a measurement.
func readMachine(fs procFS) Machine {
	fp := readFingerprint(fs)
	m := Machine{
		Cores:       runtime.NumCPU(),
		CPUModel:    "unknown",
		Kernel:      "unknown",
		CacheL1d:    "?",
		CacheL1i:    "?",
		CacheL2:     "?",
		CacheL3:     "?",
		Fingerprint: fp,
	}
	if fp != nil {
		if fp.CPU.ModelName != "" {
			m.CPUModel = fp.CPU.ModelName
		}
		if fp.Kernel.Sysname != "" && fp.Kernel.Release != "" {
			m.Kernel = fp.Kernel.Sysname + " " + fp.Kernel.Release
		} else if fp.Kernel.Uname != "" {
			m.Kernel = fp.Kernel.Uname
		}
		m.MemoryBytes = fp.Memory.TotalBytes
		m.Memory = fp.Memory.Total
		if fp.Telemetry != nil {
			m.MemoryAvailableBytes = fp.Telemetry.MemoryAvailableBytes
			m.MemoryAvailable = fp.Telemetry.MemoryAvailable
		}
		if s := cacheByType(fp.Caches, "1", "Data"); s != "" {
			m.CacheL1d = s
		}
		if s := cacheByType(fp.Caches, "1", "Instruction"); s != "" {
			m.CacheL1i = s
		}
		if s := cacheByType(fp.Caches, "2", "Unified"); s != "" {
			m.CacheL2 = s
		} else if s := cacheByType(fp.Caches, "2", "Data"); s != "" {
			m.CacheL2 = s
		}
		if s := cacheByType(fp.Caches, "3", "Unified"); s != "" {
			m.CacheL3 = s
		}
		m.Container = fp.Virtualization.Container
		if fp.CPU.LogicalCPUs > 0 {
			m.Cores = fp.CPU.LogicalCPUs
		}
	}
	// cpu0 indexN layout is still the common case when type files are missing.
	if m.CacheL1d == "?" {
		cpu0 := fs.sys + "/devices/system/cpu/cpu0/cache/"
		m.CacheL1d = cacheSizeAt(cpu0 + "index0/size")
		m.CacheL1i = cacheSizeAt(cpu0 + "index1/size")
		m.CacheL2 = cacheSizeAt(cpu0 + "index2/size")
		m.CacheL3 = cacheSizeAt(cpu0 + "index3/size")
	}
	return m
}

func cpuinfoField(text, key string) string {
	for _, line := range strings.Split(text, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func cacheSizeAt(path string) string {
	s := readTrim(path)
	if s == "" {
		return "?"
	}
	return s
}

func formatBytes(n int64) string {
	if n <= 0 {
		return "unknown"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit && exp < 3; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
