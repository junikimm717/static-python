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

// Applied with `patch -p1` from the source root.
type Patch struct {
	Name string
	Data []byte
}

// LoadPatches reads s.Patches in listed order. Order is part of the contract:
// a diff series is not commutative, and the listed order is also what
// PatchSetHash hashes.
func LoadPatches(a Assets, s config.Source) ([]Patch, error) {
	return loadNamed(a, s, s.Patches)
}

// LoadTargetPatches reads the diffs s declares for one triple, if any.
func LoadTargetPatches(a Assets, s config.Source, triple string) ([]Patch, error) {
	return loadNamed(a, s, s.TargetPatches[triple])
}

func loadNamed(a Assets, s config.Source, names []string) ([]Patch, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if a == nil {
		return nil, fmt.Errorf("sources: %s lists %d patches but no asset tree was provided", s.Name, len(names))
	}
	out := make([]Patch, 0, len(names))
	for _, name := range names {
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

// A sha256 over the ordered contents, so reordering or editing a diff in place
// rebuilds the tree even though the filenames did not change.
func PatchSetHash(a Assets, s config.Source) (string, error) {
	patches, err := LoadPatches(a, s)
	if err != nil {
		return "", err
	}
	return hashPatches(patches), nil
}

// TargetPatchSetHash reports "none" for a target with no entry, which is what
// keeps a fix for one architecture out of every other architecture's key.
func TargetPatchSetHash(a Assets, s config.Source, triple string) (string, error) {
	patches, err := LoadTargetPatches(a, s, triple)
	if err != nil {
		return "", err
	}
	return hashPatches(patches), nil
}

func hashPatches(patches []Patch) string {
	if len(patches) == 0 {
		return "none"
	}
	h := sha256.New()
	for _, p := range patches {
		fmt.Fprintf(h, "%s\x00%d\x00", p.Name, len(p.Data))
		h.Write(p.Data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Shelling out to patch(1) is the one concession in this package: a correct
// unified-diff applier with fuzz and offset handling is a project of its own,
// and going through the Runner keeps the invocation in the job log.
func ApplyPatches(ctx context.Context, r *core.Runner, a Assets, s config.Source, tree, work string) error {
	patches, err := LoadPatches(a, s)
	if err != nil {
		return err
	}
	return apply(ctx, r, s, patches, tree, work, "")
}

// ApplyTargetPatches runs the triple's diffs over a staged copy of the tree,
// never over the published srctree.
func ApplyTargetPatches(ctx context.Context, r *core.Runner, a Assets, s config.Source, triple, tree, work string) error {
	patches, err := LoadTargetPatches(a, s, triple)
	if err != nil {
		return err
	}
	return apply(ctx, r, s, patches, tree, work, triple)
}

func apply(ctx context.Context, r *core.Runner, s config.Source, patches []Patch, tree, work, triple string) error {
	if len(patches) == 0 {
		return nil
	}
	sub, what := "patches", Slug(s)
	if triple != "" {
		sub, what = "patches-"+triple, Slug(s)+" for "+triple
	}
	dir := filepath.Join(work, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	r.Step(fmt.Sprintf("applying %d patches to %s", len(patches), what))
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
			return fmt.Errorf("sources: applying %s to %s: %w", p.Name, what, err)
		}
	}
	return nil
}
