package recipe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// Fat LTO objects would still enter WPA if the python link has -flto.
// -flinker-output=nolto-rel is the relocatable that is no longer IR.
// --gc-sections here would drop symbols python has not yet referenced.
func (j *depJob) materializeArchives(ctx context.Context, r *core.Runner, te *toolenv, stage, work string) error {
	if j.res.LTOMode != config.LTOModePerDep || j.res.HostBuilt() {
		return nil
	}
	if !hasLTOCompile(j.res.CFlags) {
		return nil
	}
	archives, err := findStaticArchives(stage)
	if err != nil {
		return err
	}
	if len(archives) == 0 {
		return nil
	}
	relDir := filepath.Join(work, "lto-rel")
	if err := os.MkdirAll(relDir, 0o755); err != nil {
		return err
	}
	flags := ltoRelocFlags(j.res.CFlags, j.res.LDFlags)
	for _, ar := range archives {
		rel := filepath.Join(relDir, archiveRelName(stage, ar)+".o")
		r.Step("LTO-rel " + filepath.Base(ar))
		if err := r.Run(ctx, te.cmd(j.name+"-lto-rel", filepath.Dir(ar),
			ltoRelocArgs(te.tools.CC, rel, ar, flags), nil)); err != nil {
			return err
		}
		// ar rcs *adds* a member. The original IR objects stay, so the
		// python link sees every symbol twice (lib_libbz2.a.o and bzlib.o).
		replaced := ar + ".rel"
		if err := r.Run(ctx, te.cmd(j.name+"-lto-ar", filepath.Dir(ar),
			[]string{te.tools.AR, "rcs", replaced, rel}, nil)); err != nil {
			return err
		}
		if err := os.Rename(replaced, ar); err != nil {
			return fmt.Errorf("recipe: replacing %s with the LTO relocatable: %w", ar, err)
		}
	}
	return nil
}

func findStaticArchives(stage string) ([]string, error) {
	var out []string
	for _, dir := range []string{"lib", "lib64"} {
		root := filepath.Join(stage, dir)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".a") {
				return nil
			}
			out = append(out, path)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("recipe: listing static archives under %s: %w", root, err)
		}
	}
	return out, nil
}

func archiveRelName(stage, ar string) string {
	rel, err := filepath.Rel(stage, ar)
	if err != nil {
		return strings.ReplaceAll(filepath.Base(ar), ".", "_")
	}
	return strings.ReplaceAll(rel, string(os.PathSeparator), "_")
}

func hasLTOCompile(flags []string) bool {
	for _, f := range flags {
		if f == "-flto" || strings.HasPrefix(f, "-flto=") {
			return true
		}
	}
	return false
}

func ltoRelocFlags(cflags, ldflags []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(list []string) {
		for _, f := range list {
			if seen[f] {
				continue
			}
			if f == "-flto" || strings.HasPrefix(f, "-flto=") ||
				f == "-fuse-linker-plugin" || strings.HasPrefix(f, "-flto-partition") {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	add(cflags)
	add(ldflags)
	return out
}

func ltoRelocArgs(cc, dst, archive string, flags []string) []string {
	args := []string{cc, "-r", "-nostdlib"}
	args = append(args, flags...)
	args = append(args, "-flinker-output=nolto-rel", "-o", dst,
		"-Wl,--whole-archive", archive, "-Wl,--no-whole-archive")
	return args
}
