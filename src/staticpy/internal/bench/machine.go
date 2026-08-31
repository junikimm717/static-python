package bench

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Machine is the hardware provenance written to every suite's env.json.
// The two suites used to gather this independently and drifted; one struct
// is the point.
type Machine struct {
	Kernel               string `json:"kernel"`
	CPUModel             string `json:"cpu_model"`
	Cores                int    `json:"logical_cores"`
	MemoryBytes          int64  `json:"memory_bytes"`
	Memory               string `json:"memory,omitempty"`
	MemoryAvailableBytes int64  `json:"memory_available_bytes,omitempty"`
	MemoryAvailable      string `json:"memory_available,omitempty"`
	CacheL1d             string `json:"cache_l1d"`
	CacheL1i             string `json:"cache_l1i"`
	CacheL2              string `json:"cache_l2"`
	CacheL3              string `json:"cache_l3"`
	Container            bool   `json:"container"`
	Topology             string `json:"topology,omitempty"`
	Affinity             string `json:"affinity,omitempty"`
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
	m := Machine{
		Cores:    runtime.NumCPU(),
		CPUModel: "unknown",
		Kernel:   "unknown",
		CacheL1d: "?",
		CacheL1i: "?",
		CacheL2:  "?",
		CacheL3:  "?",
	}
	if fs.dockerenv != "" {
		if _, err := os.Stat(fs.dockerenv); err == nil {
			m.Container = true
		}
	}
	if b, err := os.ReadFile(fs.proc + "/cpuinfo"); err == nil {
		for _, key := range []string{"model name", "Model name", "Hardware"} {
			if v := cpuinfoField(string(b), key); v != "" {
				m.CPUModel = v
				break
			}
		}
	}
	if b, err := os.ReadFile(fs.proc + "/meminfo"); err == nil {
		total, avail := parseMeminfo(string(b))
		m.MemoryBytes = total
		if total > 0 {
			m.Memory = formatBytes(total)
		}
		m.MemoryAvailableBytes = avail
		if avail > 0 {
			m.MemoryAvailable = formatBytes(avail)
		}
	}
	cpu0 := fs.sys + "/devices/system/cpu/cpu0/cache/"
	m.CacheL1d = cacheSizeAt(cpu0 + "index0/size")
	m.CacheL1i = cacheSizeAt(cpu0 + "index1/size")
	m.CacheL2 = cacheSizeAt(cpu0 + "index2/size")
	m.CacheL3 = cacheSizeAt(cpu0 + "index3/size")
	if out, err := exec.Command("uname", "-sr").Output(); err == nil {
		m.Kernel = strings.TrimSpace(string(out))
	} else if b, err := os.ReadFile(fs.proc + "/version"); err == nil {
		if f := strings.Fields(string(b)); len(f) >= 3 {
			m.Kernel = f[0] + " " + f[2]
		}
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
	b, err := os.ReadFile(path)
	if err != nil {
		return "?"
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "?"
	}
	return s
}

// /proc/meminfo reports kB, which is KiB (1024).
func parseMeminfo(text string) (total, avail int64) {
	for _, line := range strings.Split(text, "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if len(fields) > 1 && strings.EqualFold(fields[1], "kB") {
			v *= 1024
		}
		switch strings.TrimSpace(key) {
		case "MemTotal":
			total = v
		case "MemAvailable":
			avail = v
		}
	}
	return total, avail
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
