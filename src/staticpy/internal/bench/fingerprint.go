package bench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Fingerprint is every host property that can move a number. It is written
// onto manifest.json (and nested in env.json) so a later comparison can tell
// "same machine" from "same cpu_model string".
//
// SHA256 covers the identity fields only. Snapshots that change while the
// machine sits idle (load, current frequency, free RAM, this run's pin)
// live under Telemetry and are excluded from the digest.
type Fingerprint struct {
	SHA256          string            `json:"sha256,omitempty"`
	CPU             CPUInfo           `json:"cpu"`
	SMT             SMTInfo           `json:"smt"`
	Caches          []CacheInfo       `json:"caches,omitempty"`
	Memory          MemDetail         `json:"memory"`
	NUMA            NUMAInfo          `json:"numa"`
	Kernel          KernelInfo        `json:"kernel"`
	OS              OSInfo            `json:"os"`
	Vulnerabilities map[string]string `json:"vulnerabilities,omitempty"`
	Power           PowerInfo         `json:"power"`
	Platform        PlatformInfo      `json:"platform"`
	Virtualization  VirtInfo          `json:"virtualization"`
	Isolation       IsolationInfo     `json:"isolation"`
	Collector       CollectorInfo     `json:"collector"`
	Telemetry       *Telemetry        `json:"telemetry,omitempty"`
}

// Telemetry is a point-in-time snapshot of the host, recorded so a
// suspicious number can be audited, but not hashed: two runs on the same
// silicon must compare equal even if load and current MHz moved.
type Telemetry struct {
	CapturedUTC          string `json:"captured_utc,omitempty"`
	CPUMHz               string `json:"cpu_mhz,omitempty"`
	MemoryAvailable      string `json:"memory_available,omitempty"`
	MemoryAvailableBytes int64  `json:"memory_available_bytes,omitempty"`
	MemoryFreeBytes      int64  `json:"memory_free_bytes,omitempty"`
	BuffersBytes         int64  `json:"buffers_bytes,omitempty"`
	CachedBytes          int64  `json:"cached_bytes,omitempty"`
	SwapFreeBytes        int64  `json:"swap_free_bytes,omitempty"`
	DirtyBytes           int64  `json:"dirty_bytes,omitempty"`
	AnonPagesBytes       int64  `json:"anon_pages_bytes,omitempty"`
	ShmemBytes           int64  `json:"shmem_bytes,omitempty"`
	CurKHz               string `json:"cur_khz,omitempty"`
	Affinity             string `json:"affinity,omitempty"`
	Topology             string `json:"topology,omitempty"`
	Loadavg1             string `json:"loadavg_1m,omitempty"`
	Loadavg5             string `json:"loadavg_5m,omitempty"`
	Loadavg15            string `json:"loadavg_15m,omitempty"`
	RunnableEntities     string `json:"runnable,omitempty"`
	GOMAXPROCS           int    `json:"gomaxprocs,omitempty"`
}

type CPUInfo struct {
	Vendor         string   `json:"vendor,omitempty"`
	Family         string   `json:"family,omitempty"`
	Model          string   `json:"model,omitempty"`
	ModelName      string   `json:"model_name,omitempty"`
	ModelNames     []string `json:"model_names,omitempty"`
	Stepping       string   `json:"stepping,omitempty"`
	Microcode      string   `json:"microcode,omitempty"`
	MicrocodeSysfs string   `json:"microcode_sysfs,omitempty"`
	CacheSize      string   `json:"cpuinfo_cache_size,omitempty"`
	PhysicalID     string   `json:"physical_id,omitempty"`
	Siblings       string   `json:"siblings,omitempty"`
	CoreID         string   `json:"core_id,omitempty"`
	CPUCores       string   `json:"cpu_cores,omitempty"`
	ApicID         string   `json:"apicid,omitempty"`
	Flags          string   `json:"flags,omitempty"`
	Bugs           string   `json:"bugs,omitempty"`
	BogoMIPS       string   `json:"bogomips,omitempty"`
	ClflushSize    string   `json:"clflush_size,omitempty"`
	CacheAlignment string   `json:"cache_alignment,omitempty"`
	AddressSizes   string   `json:"address_sizes,omitempty"`
	CPUIDLevel     string   `json:"cpuid_level,omitempty"`
	Hardware       string   `json:"hardware,omitempty"`
	Implementer    string   `json:"implementer,omitempty"`
	Architecture   string   `json:"architecture,omitempty"`
	Variant        string   `json:"variant,omitempty"`
	Part           string   `json:"part,omitempty"`
	Revision       string   `json:"revision,omitempty"`
	Features       string   `json:"features,omitempty"`
	MIDR           string   `json:"midr,omitempty"`
	REVIDR         string   `json:"revidr,omitempty"`
	Capacity       string   `json:"cpu_capacity,omitempty"`
	PackageID      string   `json:"physical_package_id,omitempty"`
	DieID          string   `json:"die_id,omitempty"`
	ClusterID      string   `json:"cluster_id,omitempty"`
	ThreadSiblings string   `json:"thread_siblings_list,omitempty"`
	CoreCPUs       string   `json:"core_cpus_list,omitempty"`
	PackageCPUs    string   `json:"package_cpus_list,omitempty"`
	Heterogeneous  bool     `json:"heterogeneous,omitempty"`
	LogicalCPUs    int      `json:"logical_cpus,omitempty"`
}

type SMTInfo struct {
	Active         string `json:"active,omitempty"`
	Control        string `json:"control,omitempty"`
	ThreadsPerCore int    `json:"threads_per_core,omitempty"`
}

type CacheInfo struct {
	Level      string `json:"level,omitempty"`
	Type       string `json:"type,omitempty"`
	Size       string `json:"size,omitempty"`
	LineSize   string `json:"coherency_line_size,omitempty"`
	Sets       string `json:"number_of_sets,omitempty"`
	Ways       string `json:"ways_of_associativity,omitempty"`
	SharedCPUs string `json:"shared_cpu_list,omitempty"`
}

type MemDetail struct {
	TotalBytes     int64  `json:"total_bytes,omitempty"`
	Total          string `json:"total,omitempty"`
	SwapTotalBytes int64  `json:"swap_total_bytes,omitempty"`
	HugePages      int64  `json:"hugepages_total,omitempty"`
	HugePageSize   int64  `json:"hugepage_size_bytes,omitempty"`
	DirectMap4k    int64  `json:"directmap_4k_bytes,omitempty"`
	DirectMap2M    int64  `json:"directmap_2m_bytes,omitempty"`
	DirectMap1G    int64  `json:"directmap_1g_bytes,omitempty"`
	PageSize       int    `json:"page_size,omitempty"`
	THP            string `json:"transparent_hugepage,omitempty"`
	THPDefrag      string `json:"transparent_hugepage_defrag,omitempty"`
	Swappiness     string `json:"swappiness,omitempty"`
	ASLR           string `json:"randomize_va_space,omitempty"`
}

type NUMAInfo struct {
	Nodes    string            `json:"nodes,omitempty"`
	MemTotal map[string]string `json:"node_mem_total,omitempty"`
	Distance map[string]string `json:"distance,omitempty"`
}

type KernelInfo struct {
	Sysname        string `json:"sysname,omitempty"`
	Release        string `json:"release,omitempty"`
	Version        string `json:"version,omitempty"`
	Machine        string `json:"machine,omitempty"`
	Uname          string `json:"uname,omitempty"`
	ProcVersion    string `json:"proc_version,omitempty"`
	Cmdline        string `json:"cmdline,omitempty"`
	Clocksource    string `json:"clocksource,omitempty"`
	ClockAvailable string `json:"clocksource_available,omitempty"`
}

type OSInfo struct {
	PrettyName string `json:"pretty_name,omitempty"`
	ID         string `json:"id,omitempty"`
	VersionID  string `json:"version_id,omitempty"`
	Libc       string `json:"libc,omitempty"`
}

type PowerInfo struct {
	Governor     string `json:"governor,omitempty"`
	Driver       string `json:"driver,omitempty"`
	MinKHz       string `json:"min_khz,omitempty"`
	MaxKHz       string `json:"max_khz,omitempty"`
	BaseKHz      string `json:"base_khz,omitempty"`
	Boost        string `json:"boost,omitempty"`
	NoTurbo      string `json:"no_turbo,omitempty"`
	IntelPstate  string `json:"intel_pstate,omitempty"`
	MinPerfPct   string `json:"min_perf_pct,omitempty"`
	MaxPerfPct   string `json:"max_perf_pct,omitempty"`
	EPP          string `json:"energy_performance_preference,omitempty"`
	IdleDriver   string `json:"idle_driver,omitempty"`
	IdleGovernor string `json:"idle_governor,omitempty"`
}

type PlatformInfo struct {
	SysVendor      string `json:"sys_vendor,omitempty"`
	ProductName    string `json:"product_name,omitempty"`
	ProductVersion string `json:"product_version,omitempty"`
	ProductFamily  string `json:"product_family,omitempty"`
	BoardVendor    string `json:"board_vendor,omitempty"`
	BoardName      string `json:"board_name,omitempty"`
	BIOSVendor     string `json:"bios_vendor,omitempty"`
	BIOSVersion    string `json:"bios_version,omitempty"`
	BIOSDate       string `json:"bios_date,omitempty"`
}

type VirtInfo struct {
	Hypervisor     string `json:"hypervisor,omitempty"`
	HypervisorType string `json:"hypervisor_type,omitempty"`
	Container      bool   `json:"container,omitempty"`
	Cgroup         string `json:"cgroup,omitempty"`
}

type IsolationInfo struct {
	Online      string `json:"online,omitempty"`
	Present     string `json:"present,omitempty"`
	Possible    string `json:"possible,omitempty"`
	Offline     string `json:"offline,omitempty"`
	Isolated    string `json:"isolated,omitempty"`
	NohzFull    string `json:"nohz_full,omitempty"`
	CpusAllowed string `json:"cpus_allowed_list,omitempty"`
}

type CollectorInfo struct {
	GOOS   string `json:"goos,omitempty"`
	GOARCH string `json:"goarch,omitempty"`
	NumCPU int    `json:"num_cpu,omitempty"`
}

func (m *Machine) SetRunPlacement(affinity, topology string) {
	m.Affinity = affinity
	if topology != "" {
		m.Topology = topology
	}
	if m.Fingerprint == nil {
		return
	}
	if m.Fingerprint.Telemetry == nil {
		m.Fingerprint.Telemetry = &Telemetry{}
	}
	m.Fingerprint.Telemetry.Affinity = affinity
	if topology != "" {
		m.Fingerprint.Telemetry.Topology = topology
	}
}

func (m Machine) AttachToManifest(man map[string]any) {
	if m.Fingerprint == nil {
		return
	}
	man["fingerprint"] = m.Fingerprint
	man["fingerprint_sha256"] = m.Fingerprint.SHA256
}

func (f *Fingerprint) seal() {
	tel := f.Telemetry
	f.Telemetry = nil
	f.SHA256 = ""
	b, err := json.Marshal(f)
	f.Telemetry = tel
	if err != nil {
		return
	}
	sum := sha256.Sum256(b)
	f.SHA256 = hex.EncodeToString(sum[:])
}

func readFingerprint(fs procFS) *Fingerprint {
	f := &Fingerprint{}
	tel := &Telemetry{CapturedUTC: time.Now().UTC().Format(time.RFC3339)}
	if b, err := os.ReadFile(fs.proc + "/cpuinfo"); err == nil {
		cpu, mhz := parseCPUInfo(string(b))
		f.CPU = cpu
		tel.CPUMHz = mhz
	}
	cpu0 := fs.sys + "/devices/system/cpu/cpu0/"
	f.CPU.MicrocodeSysfs = readTrim(cpu0 + "microcode/version")
	f.CPU.Capacity = readTrim(cpu0 + "cpu_capacity")
	f.CPU.PackageID = readTrim(cpu0 + "topology/physical_package_id")
	f.CPU.DieID = readTrim(cpu0 + "topology/die_id")
	f.CPU.ClusterID = readTrim(cpu0 + "topology/cluster_id")
	f.CPU.CoreID = firstNonEmpty(f.CPU.CoreID, readTrim(cpu0+"topology/core_id"))
	f.CPU.ThreadSiblings = readTrim(cpu0 + "topology/thread_siblings_list")
	f.CPU.CoreCPUs = readTrim(cpu0 + "topology/core_cpus_list")
	f.CPU.PackageCPUs = readTrim(cpu0 + "topology/package_cpus_list")
	f.CPU.MIDR = readTrim(cpu0 + "regs/identification/midr_el1")
	f.CPU.REVIDR = readTrim(cpu0 + "regs/identification/revidr_el1")
	f.CPU.LogicalCPUs = runtime.NumCPU()
	if n := len(parseCPUList(f.CPU.ThreadSiblings)); n > 0 {
		f.SMT.ThreadsPerCore = n
	}
	f.SMT.Active = readTrim(fs.sys + "/devices/system/cpu/smt/active")
	f.SMT.Control = readTrim(fs.sys + "/devices/system/cpu/smt/control")
	f.Caches = readCaches(cpu0 + "cache")
	mem, memTel := readMemDetail(fs)
	f.Memory = mem
	if memTel != (Telemetry{}) {
		tel.MemoryAvailable = memTel.MemoryAvailable
		tel.MemoryAvailableBytes = memTel.MemoryAvailableBytes
		tel.MemoryFreeBytes = memTel.MemoryFreeBytes
		tel.BuffersBytes = memTel.BuffersBytes
		tel.CachedBytes = memTel.CachedBytes
		tel.SwapFreeBytes = memTel.SwapFreeBytes
		tel.DirtyBytes = memTel.DirtyBytes
		tel.AnonPagesBytes = memTel.AnonPagesBytes
		tel.ShmemBytes = memTel.ShmemBytes
	}
	f.NUMA = readNUMA(fs.sys + "/devices/system/node")
	f.Kernel = readKernel(fs)
	f.OS = readOS()
	f.Vulnerabilities = readDirMap(fs.sys + "/devices/system/cpu/vulnerabilities")
	power, curKHz := readPower(fs)
	f.Power = power
	tel.CurKHz = curKHz
	f.Platform = readPlatform(fs.sys + "/class/dmi/id")
	f.Virtualization = readVirt(fs)
	iso, isoTel := readIsolation(fs)
	f.Isolation = iso
	tel.Loadavg1 = isoTel.Loadavg1
	tel.Loadavg5 = isoTel.Loadavg5
	tel.Loadavg15 = isoTel.Loadavg15
	tel.RunnableEntities = isoTel.RunnableEntities
	f.Collector = CollectorInfo{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		NumCPU: runtime.NumCPU(),
	}
	tel.GOMAXPROCS = runtime.GOMAXPROCS(0)
	f.Telemetry = tel
	f.seal()
	return f
}

func parseCPUInfo(text string) (CPUInfo, string) {
	var names []string
	seen := map[string]bool{}
	var first CPUInfo
	mhz := ""
	got := false
	for _, block := range strings.Split(text, "\n\n") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		get := func(k string) string { return cpuinfoField(block, k) }
		name := firstNonEmpty(get("model name"), get("Model name"), get("Hardware"))
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
		if got {
			continue
		}
		got = true
		mhz = get("cpu MHz")
		first = CPUInfo{
			Vendor:         get("vendor_id"),
			Family:         get("cpu family"),
			Model:          get("model"),
			ModelName:      name,
			Stepping:       get("stepping"),
			Microcode:      get("microcode"),
			CacheSize:      get("cache size"),
			PhysicalID:     get("physical id"),
			Siblings:       get("siblings"),
			CoreID:         get("core id"),
			CPUCores:       get("cpu cores"),
			ApicID:         get("apicid"),
			Flags:          get("flags"),
			Bugs:           get("bugs"),
			BogoMIPS:       firstNonEmpty(get("bogomips"), get("BogoMIPS")),
			ClflushSize:    get("clflush size"),
			CacheAlignment: get("cache_alignment"),
			AddressSizes:   get("address sizes"),
			CPUIDLevel:     get("cpuid level"),
			Hardware:       get("Hardware"),
			Implementer:    get("CPU implementer"),
			Architecture:   get("CPU architecture"),
			Variant:        get("CPU variant"),
			Part:           get("CPU part"),
			Revision:       get("CPU revision"),
			Features:       get("Features"),
		}
	}
	first.ModelNames = names
	first.Heterogeneous = len(names) > 1
	return first, mhz
}

func readCaches(dir string) []CacheInfo {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var idxs []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "index") {
			idxs = append(idxs, e.Name())
		}
	}
	sort.Strings(idxs)
	var out []CacheInfo
	for _, name := range idxs {
		p := filepath.Join(dir, name)
		c := CacheInfo{
			Level:      readTrim(filepath.Join(p, "level")),
			Type:       readTrim(filepath.Join(p, "type")),
			Size:       readTrim(filepath.Join(p, "size")),
			LineSize:   readTrim(filepath.Join(p, "coherency_line_size")),
			Sets:       readTrim(filepath.Join(p, "number_of_sets")),
			Ways:       readTrim(filepath.Join(p, "ways_of_associativity")),
			SharedCPUs: readTrim(filepath.Join(p, "shared_cpu_list")),
		}
		if c.Level == "" && c.Size == "" {
			continue
		}
		out = append(out, c)
	}
	return out
}

func readMemDetail(fs procFS) (MemDetail, Telemetry) {
	m := MemDetail{PageSize: os.Getpagesize()}
	tel := Telemetry{}
	b, err := os.ReadFile(fs.proc + "/meminfo")
	if err == nil {
		vals := parseMeminfoMap(string(b))
		set := func(dst *int64, key string) {
			if v, ok := vals[key]; ok {
				*dst = v
			}
		}
		set(&m.TotalBytes, "MemTotal")
		set(&tel.MemoryAvailableBytes, "MemAvailable")
		set(&tel.MemoryFreeBytes, "MemFree")
		set(&tel.BuffersBytes, "Buffers")
		set(&tel.CachedBytes, "Cached")
		set(&m.SwapTotalBytes, "SwapTotal")
		set(&tel.SwapFreeBytes, "SwapFree")
		set(&tel.DirtyBytes, "Dirty")
		set(&tel.AnonPagesBytes, "AnonPages")
		set(&tel.ShmemBytes, "Shmem")
		set(&m.HugePages, "HugePages_Total")
		set(&m.HugePageSize, "Hugepagesize")
		set(&m.DirectMap4k, "DirectMap4k")
		set(&m.DirectMap2M, "DirectMap2M")
		set(&m.DirectMap1G, "DirectMap1G")
		if m.TotalBytes > 0 {
			m.Total = formatBytes(m.TotalBytes)
		}
		if tel.MemoryAvailableBytes > 0 {
			tel.MemoryAvailable = formatBytes(tel.MemoryAvailableBytes)
		}
	}
	m.THP = readTrim(fs.sys + "/kernel/mm/transparent_hugepage/enabled")
	m.THPDefrag = readTrim(fs.sys + "/kernel/mm/transparent_hugepage/defrag")
	m.Swappiness = readTrim(fs.proc + "/sys/vm/swappiness")
	m.ASLR = readTrim(fs.proc + "/sys/kernel/randomize_va_space")
	return m, tel
}

func parseMeminfoMap(text string) map[string]int64 {
	out := map[string]int64{}
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
		out[strings.TrimSpace(key)] = v
	}
	return out
}

func readNUMA(root string) NUMAInfo {
	n := NUMAInfo{}
	n.Nodes = readTrim(filepath.Join(root, "online"))
	if n.Nodes == "" {
		return n
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return n
	}
	n.MemTotal = map[string]string{}
	n.Distance = map[string]string{}
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), "node") || !e.IsDir() {
			continue
		}
		id := e.Name()
		if b, err := os.ReadFile(filepath.Join(root, id, "meminfo")); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if strings.Contains(line, "MemTotal") {
					n.MemTotal[id] = strings.TrimSpace(line)
					break
				}
			}
		}
		if d := readTrim(filepath.Join(root, id, "distance")); d != "" {
			n.Distance[id] = d
		}
	}
	if len(n.MemTotal) == 0 {
		n.MemTotal = nil
	}
	if len(n.Distance) == 0 {
		n.Distance = nil
	}
	return n
}

func readKernel(fs procFS) KernelInfo {
	k := KernelInfo{}
	if out, err := exec.Command("uname", "-s", "-r", "-v", "-m").Output(); err == nil {
		k.Uname = strings.TrimSpace(string(out))
		f := strings.Fields(k.Uname)
		if len(f) >= 1 {
			k.Sysname = f[0]
		}
		if len(f) >= 2 {
			k.Release = f[1]
		}
		if len(f) >= 3 {
			k.Machine = f[len(f)-1]
			k.Version = strings.Join(f[2:len(f)-1], " ")
		}
	}
	if b, err := os.ReadFile(fs.proc + "/version"); err == nil {
		k.ProcVersion = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile(fs.proc + "/cmdline"); err == nil {
		k.Cmdline = strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", " "))
	}
	cs := fs.sys + "/devices/system/clocksource/clocksource0/"
	k.Clocksource = readTrim(cs + "current_clocksource")
	k.ClockAvailable = readTrim(cs + "available_clocksource")
	return k
}

func readOS() OSInfo {
	o := OSInfo{}
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		b, err = os.ReadFile("/usr/lib/os-release")
	}
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			v = strings.Trim(v, `"'`)
			switch k {
			case "PRETTY_NAME":
				o.PrettyName = v
			case "ID":
				o.ID = v
			case "VERSION_ID":
				o.VersionID = v
			}
		}
	}
	if out, err := exec.Command("getconf", "GNU_LIBC_VERSION").Output(); err == nil {
		o.Libc = strings.TrimSpace(string(out))
	}
	return o
}

func readPower(fs procFS) (PowerInfo, string) {
	cpu0 := fs.sys + "/devices/system/cpu/cpu0/"
	freq := cpu0 + "cpufreq/"
	pstate := fs.sys + "/devices/system/cpu/intel_pstate/"
	idle := fs.sys + "/devices/system/cpu/cpuidle/"
	return PowerInfo{
		Governor:     readTrim(freq + "scaling_governor"),
		Driver:       readTrim(freq + "scaling_driver"),
		MinKHz:       readTrim(freq + "cpuinfo_min_freq"),
		MaxKHz:       readTrim(freq + "cpuinfo_max_freq"),
		BaseKHz:      readTrim(freq + "base_frequency"),
		Boost:        readTrim(fs.sys + "/devices/system/cpu/cpufreq/boost"),
		NoTurbo:      readTrim(pstate + "no_turbo"),
		IntelPstate:  readTrim(pstate + "status"),
		MinPerfPct:   readTrim(pstate + "min_perf_pct"),
		MaxPerfPct:   readTrim(pstate + "max_perf_pct"),
		EPP:          readTrim(freq + "energy_performance_preference"),
		IdleDriver:   readTrim(idle + "current_driver"),
		IdleGovernor: firstNonEmpty(readTrim(idle+"current_governor_ro"), readTrim(idle+"current_governor")),
	}, firstNonEmpty(readTrim(freq+"scaling_cur_freq"), readTrim(freq+"cpuinfo_cur_freq"))
}

func readPlatform(dmi string) PlatformInfo {
	return PlatformInfo{
		SysVendor:      readTrim(filepath.Join(dmi, "sys_vendor")),
		ProductName:    readTrim(filepath.Join(dmi, "product_name")),
		ProductVersion: readTrim(filepath.Join(dmi, "product_version")),
		ProductFamily:  readTrim(filepath.Join(dmi, "product_family")),
		BoardVendor:    readTrim(filepath.Join(dmi, "board_vendor")),
		BoardName:      readTrim(filepath.Join(dmi, "board_name")),
		BIOSVendor:     readTrim(filepath.Join(dmi, "bios_vendor")),
		BIOSVersion:    readTrim(filepath.Join(dmi, "bios_version")),
		BIOSDate:       readTrim(filepath.Join(dmi, "bios_date")),
	}
}

func readVirt(fs procFS) VirtInfo {
	v := VirtInfo{
		HypervisorType: readTrim(fs.sys + "/hypervisor/type"),
	}
	if fs.dockerenv != "" {
		if _, err := os.Stat(fs.dockerenv); err == nil {
			v.Container = true
		}
	}
	if b, err := os.ReadFile(fs.proc + "/1/cgroup"); err == nil {
		v.Cgroup = firstLine(string(b))
	}
	if b, err := os.ReadFile(fs.proc + "/cpuinfo"); err == nil {
		flags := cpuinfoField(string(b), "flags")
		if strings.Contains(" "+flags+" ", " hypervisor ") {
			v.Hypervisor = "cpuinfo_flag"
		}
	}
	return v
}

func readIsolation(fs procFS) (IsolationInfo, Telemetry) {
	cpu := fs.sys + "/devices/system/cpu/"
	iso := IsolationInfo{
		Online:   readTrim(cpu + "online"),
		Present:  readTrim(cpu + "present"),
		Possible: readTrim(cpu + "possible"),
		Offline:  readTrim(cpu + "offline"),
		Isolated: readTrim(cpu + "isolated"),
		NohzFull: readTrim(cpu + "nohz_full"),
	}
	tel := Telemetry{}
	if b, err := os.ReadFile(fs.proc + "/self/status"); err == nil {
		iso.CpusAllowed = cpuinfoField(string(b), "Cpus_allowed_list")
	}
	if b, err := os.ReadFile(fs.proc + "/loadavg"); err == nil {
		f := strings.Fields(string(b))
		if len(f) >= 3 {
			tel.Loadavg1, tel.Loadavg5, tel.Loadavg15 = f[0], f[1], f[2]
		}
		if len(f) >= 4 {
			tel.RunnableEntities = f[3]
		}
	}
	return iso, tel
}

func readDirMap(dir string) map[string]string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		out[e.Name()] = readTrim(filepath.Join(dir, e.Name()))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func cacheByType(caches []CacheInfo, level, typ string) string {
	for _, c := range caches {
		if c.Level == level && strings.EqualFold(c.Type, typ) {
			return c.Size
		}
	}
	return ""
}

func vulnSummary(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var vuln, mit, other int
	for _, k := range keys {
		v := strings.ToLower(m[k])
		switch {
		case strings.HasPrefix(v, "vulnerable"):
			vuln++
		case strings.HasPrefix(v, "mitigation"):
			mit++
		default:
			other++
		}
	}
	return strconv.Itoa(vuln) + " vulnerable / " + strconv.Itoa(mit) + " mitigated / " + strconv.Itoa(other) + " other"
}
