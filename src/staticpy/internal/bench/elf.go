package bench

import (
	"debug/elf"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// linkage reports how a binary reaches its libc, plus its build id if it has
// one. PT_INTERP is the distinction that survives stripping, which the flags
// that produced the binary do not: nothing in a stripped ELF records -static.
func linkage(path string) (kind, buildID string) {
	f, err := elf.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			return "dynamic", buildIDOf(f)
		}
	}
	if f.Type != elf.ET_DYN {
		return "static", buildIDOf(f)
	}
	// ET_DYN with no interpreter is either a static-pie executable or an
	// ordinary shared library. Only the executable sets DF_1_PIE, and calling a
	// .so "static" would misreport every shared build's own libpython.
	if flags, err := f.DynValue(elf.DT_FLAGS_1); err == nil {
		for _, v := range flags {
			if v&uint64(elf.DF_1_PIE) != 0 {
				return "static-pie", buildIDOf(f)
			}
		}
	}
	return "shared-library", buildIDOf(f)
}

func buildIDOf(f *elf.File) string {
	s := f.Section(".note.gnu.build-id")
	if s == nil {
		return ""
	}
	b, err := s.Data()
	if err != nil || len(b) < 12 {
		return ""
	}
	// ELF note: namesz, descsz, type, then the name padded to 4 bytes.
	nameSz := f.ByteOrder.Uint32(b[0:4])
	descSz := f.ByteOrder.Uint32(b[4:8])
	off := 12 + int((nameSz+3)&^3)
	if off < 0 || off+int(descSz) > len(b) {
		return ""
	}
	return hex.EncodeToString(b[off : off+int(descSz)])
}

// sharedCore locates the libpython an --enable-shared executable delegates to.
//
// That executable is a few kilobytes of main(); the interpreter under
// measurement is the library. Recording only the stub's hash and size says
// nothing about what actually ran, and makes a shared build look absurdly
// smaller than a static one in the same table.
func sharedCore(path string) string {
	f, err := elf.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	needed, err := f.DynString(elf.DT_NEEDED)
	if err != nil {
		return ""
	}
	lib := ""
	for _, n := range needed {
		if strings.HasPrefix(n, "libpython") {
			lib = n
			break
		}
	}
	if lib == "" {
		return ""
	}
	origin := filepath.Dir(path)
	for _, tag := range []elf.DynTag{elf.DT_RUNPATH, elf.DT_RPATH} {
		entries, err := f.DynString(tag)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			for _, dir := range strings.Split(entry, ":") {
				dir = strings.ReplaceAll(dir, "${ORIGIN}", origin)
				dir = strings.ReplaceAll(dir, "$ORIGIN", origin)
				if p := filepath.Join(dir, lib); exists(p) {
					return p
				}
			}
		}
	}
	// No runpath: a build installed into a prefix still keeps the library one
	// directory over from bin/, which is where the rootfs layout puts it.
	for _, dir := range []string{"lib", "lib64"} {
		if p := filepath.Join(filepath.Dir(origin), dir, lib); exists(p) {
			return p
		}
	}
	return ""
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
