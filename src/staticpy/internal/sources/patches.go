package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// Assets is the patch and edit-text tree: the config package's embedded
// defaults with any on-disk overlay already resolved. Tests pass an fstest.MapFS.
type Assets = fs.FS

// PatchDir is where a source's diffs live inside Assets.
func PatchDir(s config.Source) string { return path.Join("patches", Slug(s)) }

// Patch is one unified diff, applied with `patch -p1` from the source root.
type Patch struct {
	Name string
	Data []byte
}

// LoadPatches reads s.Patches in listed order. Order is part of the contract:
// a diff series is not commutative, and the listed order is also what
// PatchSetHash hashes.
func LoadPatches(a Assets, s config.Source) ([]Patch, error) {
	if len(s.Patches) == 0 {
		return nil, nil
	}
	if a == nil {
		return nil, fmt.Errorf("sources: %s lists %d patches but no asset tree was provided", s.Name, len(s.Patches))
	}
	out := make([]Patch, 0, len(s.Patches))
	for _, name := range s.Patches {
		if strings.Contains(name, "/") || name == "" || name == "." || name == ".." {
			return nil, fmt.Errorf("sources: %s: patch %q must be a bare filename under %s", s.Name, name, PatchDir(s))
		}
		p := path.Join(PatchDir(s), name)
		b, err := fs.ReadFile(a, p)
		if err != nil {
			return nil, fmt.Errorf("sources: %s: reading patch %s: %w", s.Name, p, err)
		}
		out = append(out, Patch{Name: name, Data: b})
	}
	return out, nil
}

// PatchSetHash is the patch series' contribution to the job key: a sha256 over
// the ordered contents, so reordering or editing a diff in place rebuilds the
// tree even though the filenames did not change.
func PatchSetHash(a Assets, s config.Source) (string, error) {
	patches, err := LoadPatches(a, s)
	if err != nil {
		return "", err
	}
	if len(patches) == 0 {
		return "none", nil
	}
	h := sha256.New()
	for _, p := range patches {
		fmt.Fprintf(h, "%s\x00%d\x00", p.Name, len(p.Data))
		h.Write(p.Data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ApplyPatches materialises the series into work and applies each diff to tree
// in order. Shelling out to patch(1) is the one concession in this package:
// a correct unified-diff applier with fuzz and offset handling is a project of
// its own, and going through the Runner keeps the invocation in the job log.
func ApplyPatches(ctx context.Context, r *core.Runner, a Assets, s config.Source, tree, work string) error {
	patches, err := LoadPatches(a, s)
	if err != nil {
		return err
	}
	if len(patches) == 0 {
		return nil
	}
	dir := filepath.Join(work, "patches")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	r.Step(fmt.Sprintf("applying %d patches to %s", len(patches), Slug(s)))
	for i, p := range patches {
		file := filepath.Join(dir, fmt.Sprintf("%02d-%s", i, p.Name))
		if err := os.WriteFile(file, p.Data, 0o644); err != nil {
			return fmt.Errorf("sources: staging patch %s: %w", p.Name, err)
		}
		cmd := core.Cmd{
			Dir: tree,
			// --batch so a mismatched hunk fails instead of prompting on a stdin
			// no build has; --no-backup-if-mismatch so a .orig file never ends up
			// in the published tree.
			Args: []string{"patch", "-p1", "--batch", "--no-backup-if-mismatch", "-i", file},
			Name: fmt.Sprintf("patch-%s-%02d", s.Name, i),
		}
		if err := r.Run(ctx, cmd); err != nil {
			return fmt.Errorf("sources: applying %s to %s: %w", p.Name, Slug(s), err)
		}
	}
	return nil
}
