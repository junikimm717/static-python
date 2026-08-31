package recipe

import (
	"debug/elf"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// After a host-built install, every ELF still names the published prefix in
// DT_RPATH/DT_RUNPATH — that path is what make and libtool were configured
// with. It is always longer than an $ORIGIN-relative spelling, so shrinking
// in place does not move the dynamic section. The in-tree build binary is
// left alone; only the installed tree is rewritten, so `make` still sees the
// absolute rpath plus LD_LIBRARY_PATH.
func rewriteRootfsRpaths(root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if !isELF(p) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		return patchRpath(p, originRunpath(rel))
	})
}

func originRunpath(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." {
		dir = ""
	}
	var parts []string
	for _, lib := range []string{"lib", "lib64"} {
		from := filepath.FromSlash(dir)
		if from == "" {
			from = "."
		}
		relLib, err := filepath.Rel(from, lib)
		if err != nil {
			continue
		}
		relLib = filepath.ToSlash(relLib)
		if relLib == "." {
			parts = append(parts, "$ORIGIN")
			continue
		}
		parts = append(parts, "$ORIGIN/"+relLib)
	}
	return strings.Join(parts, ":")
}

func patchRpath(path, wanted string) error {
	f, err := elf.Open(path)
	if err != nil {
		return nil
	}
	cur, strOff, dynstr, err := rpathLocation(f)
	needed, _ := f.DynString(elf.DT_NEEDED)
	f.Close()
	if err != nil {
		if needsShippedLib(needed) {
			return fmt.Errorf("recipe: %s needs %s but has no RUNPATH to rewrite to $ORIGIN", path, strings.Join(needed, ", "))
		}
		return nil
	}
	if cur == wanted {
		return nil
	}
	if len(wanted) > len(cur) {
		return fmt.Errorf("recipe: %s: $ORIGIN runpath %q is longer than the baked rpath %q; in-place rewrite would overflow .dynstr",
			path, wanted, cur)
	}
	if dynstr == nil {
		return fmt.Errorf("recipe: %s: no .dynstr", path)
	}
	fileOff := int64(dynstr.Offset) + int64(strOff)
	rw, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer rw.Close()
	buf := make([]byte, len(cur)+1)
	if _, err := rw.ReadAt(buf, fileOff); err != nil {
		return err
	}
	if string(buf[:len(cur)]) != cur || buf[len(cur)] != 0 {
		return fmt.Errorf("recipe: %s: rpath at .dynstr offset %d is not %q", path, strOff, cur)
	}
	out := make([]byte, len(cur)+1)
	copy(out, wanted)
	_, err = rw.WriteAt(out, fileOff)
	return err
}

func rpathLocation(f *elf.File) (string, uint64, *elf.Section, error) {
	for _, tag := range []elf.DynTag{elf.DT_RUNPATH, elf.DT_RPATH} {
		vals, err := f.DynValue(tag)
		if err != nil || len(vals) == 0 || vals[0] == 0 {
			continue
		}
		ss, err := f.DynString(tag)
		if err != nil || len(ss) == 0 {
			continue
		}
		return ss[0], vals[0], f.Section(".dynstr"), nil
	}
	return "", 0, nil, fmt.Errorf("no rpath")
}

func needsShippedLib(needed []string) bool {
	for _, n := range needed {
		base := n
		if i := strings.Index(n, ".so"); i > 0 {
			base = n[:i]
		}
		switch {
		case strings.HasPrefix(base, "ld-"),
			base == "libc", base == "libm", base == "libdl",
			base == "libpthread", base == "librt", base == "libresolv",
			base == "libgcc_s", base == "libstdc++", base == "libatomic",
			n == "linux-vdso.so.1":
			continue
		default:
			return true
		}
	}
	return false
}

func isELF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var mag [4]byte
	if _, err := f.Read(mag[:]); err != nil {
		return false
	}
	return mag[0] == 0x7f && mag[1] == 'E' && mag[2] == 'L' && mag[3] == 'F'
}
