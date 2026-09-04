package ensure

import (
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

// identity is the ELF header a correctly cross-built binary for a target must
// have. Keyed by architecture; a target whose arch is missing cannot be
// verified, which is a failure rather than a silent pass.
type identity struct {
	Machine elf.Machine
	Class   elf.Class
	Data    elf.Data
}

var identities = map[string]identity{
	"x86_64":      {elf.EM_X86_64, elf.ELFCLASS64, elf.ELFDATA2LSB},
	"i386":        {elf.EM_386, elf.ELFCLASS32, elf.ELFDATA2LSB},
	"aarch64":     {elf.EM_AARCH64, elf.ELFCLASS64, elf.ELFDATA2LSB},
	"arm":         {elf.EM_ARM, elf.ELFCLASS32, elf.ELFDATA2LSB},
	"riscv32":     {elf.EM_RISCV, elf.ELFCLASS32, elf.ELFDATA2LSB},
	"riscv64":     {elf.EM_RISCV, elf.ELFCLASS64, elf.ELFDATA2LSB},
	"powerpc64":   {elf.EM_PPC64, elf.ELFCLASS64, elf.ELFDATA2MSB},
	"powerpc64le": {elf.EM_PPC64, elf.ELFCLASS64, elf.ELFDATA2LSB},
	"mips64":      {elf.EM_MIPS, elf.ELFCLASS64, elf.ELFDATA2MSB},
	"s390x":       {elf.EM_S390, elf.ELFCLASS64, elf.ELFDATA2MSB},
}

// Spellings that appear in triples or in hand-written config rows.
var archAliases = map[string]string{
	"amd64":    "x86_64",
	"x86":      "i386",
	"i486":     "i386",
	"i586":     "i386",
	"i686":     "i386",
	"arm64":    "aarch64",
	"armv7":    "arm",
	"armv7l":   "arm",
	"armhf":    "arm",
	"armel":    "arm",
	"ppc64":    "powerpc64",
	"ppc64le":  "powerpc64le",
	"mips64el": "mips64",
	"s390":     "s390x",
}

// ELFInfo is what verification cares about, not a full parse of the file.
type ELFInfo struct {
	Path    string `json:"path"`
	Machine string `json:"machine"`
	Class   string `json:"class"`
	Data    string `json:"data"`
	Type    string `json:"type"`

	HasInterp bool   `json:"has_interp"`
	Interp    string `json:"interp"`
	HasDynSeg bool   `json:"has_dynamic"`

	// Stripped means .symtab is absent. A stripped static binary is normal and
	// expected; it only means symbol checks cannot run.
	Stripped bool     `json:"stripped"`
	Symbols  int      `json:"symbols"`
	Needed   []string `json:"needed,omitempty"`

	machine elf.Machine
	class   elf.Class
	data    elf.Data
	symbols map[string]bool
}

func (i *ELFInfo) String() string {
	s := fmt.Sprintf("%s/%s/%s %s", i.Class, i.Data, i.Machine, i.Type)
	if i.Static() {
		return s + " static"
	}
	if i.HasInterp {
		return s + " dynamic(" + i.Interp + ")"
	}
	return s + " dynamic"
}

// Static is the property that matters: no program interpreter to find at exec
// time and no dynamic segment to process.
func (i *ELFInfo) Static() bool { return !i.HasInterp && !i.HasDynSeg }

func (i *ELFInfo) HasSymbol(name string) bool { return i.symbols[name] }

func InspectELF(path string) (*ELFInfo, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", path, err)
	}
	if st.IsDir() {
		return nil, fmt.Errorf("inspect %s: is a directory, expected an executable", path)
	}
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("inspect %s (%d bytes): %w", path, st.Size(), err)
	}
	defer f.Close()

	info := &ELFInfo{
		Path:    path,
		Machine: f.Machine.String(),
		Class:   f.Class.String(),
		Data:    f.Data.String(),
		Type:    f.Type.String(),
		machine: f.Machine,
		class:   f.Class,
		data:    f.Data,
		symbols: map[string]bool{},
	}
	for _, p := range f.Progs {
		switch p.Type {
		case elf.PT_INTERP:
			info.HasInterp = true
			buf := make([]byte, minU64(p.Filesz, 4096))
			if n, _ := p.ReadAt(buf, 0); n > 0 {
				info.Interp = string(trimNUL(buf[:n]))
			}
		case elf.PT_DYNAMIC:
			info.HasDynSeg = true
		}
	}
	if info.HasDynSeg {
		if libs, err := f.ImportedLibraries(); err == nil {
			info.Needed = libs
		}
	}

	syms, err := f.Symbols()
	switch {
	case errors.Is(err, elf.ErrNoSymbols):
		info.Stripped = true
	case err != nil:
		return nil, fmt.Errorf("inspect %s: read .symtab: %w", path, err)
	default:
		info.Symbols = len(syms)
		for _, s := range syms {
			info.symbols[s.Name] = true
		}
	}
	return info, nil
}

// CheckELF asserts that path is the ELF the profile asked for. wantDynamic is
// the host-built reference: shared libpython, a PT_INTERP, no staticapi table
// in the executable. A static profile still requires no PT_INTERP / PT_DYNAMIC.
// Symbols are checked only when the binary carries a .symtab; a stripped
// binary (or a dynamic one) reports those checks as skipped.
//
// There is deliberately no .dynsym check here: a correct static interpreter
// is static and stripped, so an empty dynamic symbol table is the expected
// state, not evidence of anything.
func CheckELF(rep *Report, path string, t config.Target, wantSymbols []string, wantDynamic bool) *ELFInfo {
	info, err := InspectELF(path)
	if err != nil {
		rep.Fail("elf:readable", err, "%s must be a readable ELF executable", path)
		return nil
	}
	rep.Pass("elf:readable", "%s", info)

	switch {
	case wantDynamic && info.Static():
		rep.Failf("elf:dynamic", "%s is fully static; the reference profile must be a shared interpreter", path)
	case wantDynamic:
		rep.Pass("elf:dynamic", "PT_INTERP %s DT_NEEDED %s", info.Interp, strings.Join(info.Needed, ", "))
	case info.Static():
		rep.Pass("elf:static", "no PT_INTERP, no PT_DYNAMIC")
	default:
		var why []string
		if info.HasInterp {
			why = append(why, "PT_INTERP is "+info.Interp)
		}
		if info.HasDynSeg {
			why = append(why, "PT_DYNAMIC is present")
		}
		if len(info.Needed) > 0 {
			why = append(why, "DT_NEEDED: "+strings.Join(info.Needed, ", "))
		}
		rep.Failf("elf:static", "%s is not fully static: %s", path, strings.Join(why, "; "))
	}

	want, ok := IdentityFor(t)
	switch {
	case !ok:
		rep.Failf("elf:machine", "no ELF identity known for target %s (arch %q); "+
			"add a row to identities in internal/ensure/elf.go before shipping this target",
			t.Triple, t.Arch)
	case info.machine != want.Machine || info.class != want.Class || info.data != want.Data:
		rep.Failf("elf:machine", "%s: expected %s/%s/%s for %s, got %s/%s/%s",
			path, want.Class, want.Data, want.Machine, t.Triple, info.Class, info.Data, info.Machine)
	default:
		rep.Pass("elf:machine", "%s/%s/%s matches %s", info.Class, info.Data, info.Machine, t.Triple)
	}

	if t.Bits != 0 && ok {
		if bits := classBits(info.class); bits != t.Bits {
			rep.Failf("elf:bits", "%s: ELF class is %d-bit but target %s declares bits = %d",
				path, bits, t.Triple, t.Bits)
		} else {
			rep.Pass("elf:bits", "%d-bit", bits)
		}
	}

	for _, sym := range wantSymbols {
		name := "elf:symbol:" + sym
		switch {
		case wantDynamic:
			rep.Skip(name, "dynamic interpreter: ctypes.pythonapi uses libpython, not a static table")
		case info.Stripped:
			rep.Skip(name, "%s has no .symtab (stripped); symbol presence cannot be checked", path)
		case info.HasSymbol(sym):
			rep.Pass(name, "present in .symtab (%d symbols)", info.Symbols)
		default:
			rep.Failf(name, "%s: %s is absent from .symtab (%d symbols); "+
				"ctypes.pythonapi resolves through this table, so a missing symbol is a broken interpreter",
				path, sym, info.Symbols)
		}
	}
	return info
}

// IdentityFor resolves a target's expected ELF header, accepting either the
// arch column or the leading component of the triple.
func IdentityFor(t config.Target) (identity, bool) {
	for _, k := range []string{t.Arch, strings.SplitN(t.Triple, "-", 2)[0]} {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			continue
		}
		if alias, found := archAliases[k]; found {
			k = alias
		}
		if id, found := identities[k]; found {
			return id, true
		}
	}
	return identity{}, false
}

func classBits(c elf.Class) int {
	if c == elf.ELFCLASS32 {
		return 32
	}
	return 64
}

func trimNUL(b []byte) []byte {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		return b[:i]
	}
	return b
}

func minU64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
