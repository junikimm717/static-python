package sources

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// srctreeVersion is part of the job key. Bump it whenever the unpack, patch or
// edit procedure changes in a way that would produce a different tree from the
// same inputs; without it a fix to this package leaves every cached tree stale
// and undetectably wrong.
const srctreeVersion = "1"

// Options carries what the recipe layer knows and this package does not.
type Options struct {
	// Assets is the resolved patches/ and edit-text tree. Required only when the
	// source declares patches or a text_file edit.
	Assets Assets
}

// SrcTree is the job that turns one pinned source into an unpacked, patched,
// edited tree. It is architecture-free and profile-free: one tree is built once
// and shared by every target and every profile, which is why nothing about a
// triple or a flag set appears in its key.
func SrcTree(s config.Source, opts Options) core.Job {
	j := &srcTree{src: s, assets: opts.Assets}
	// Hashing is done up front because KeyInputs cannot report an error. A
	// missing patch file surfaces from Build with the real message.
	if h, err := PatchSetHash(j.assets, s); err != nil {
		j.err = err
	} else {
		j.patchHash = h
	}
	if h, err := EditSetHash(j.assets, s); err != nil && j.err == nil {
		j.err = err
	} else if err == nil {
		j.editHash = h
	}
	return j
}

type srcTree struct {
	src       config.Source
	assets    Assets
	patchHash string
	editHash  string
	err       error
}

func (j *srcTree) Name() string { return "srctree" }

func (j *srcTree) Slug() string { return "srctree:" + Slug(j.src) }

func (j *srcTree) Deps() []core.Job { return nil }

func (j *srcTree) KeyInputs() map[string]string {
	in := map[string]string{
		"source":          j.src.Name,
		"version":         j.src.Version,
		"sha256":          j.src.SHA256,
		"patches":         j.patchHash,
		"edits":           j.editHash,
		"srctree_version": srctreeVersion,
	}
	if j.err != nil {
		// A key that cannot be computed must not collide with a good one; the
		// job fails in Build either way.
		in["error"] = j.err.Error()
	}
	return in
}

func (j *srcTree) ArtifactDir(e *core.Env) string {
	return e.Path(core.DirSrcTrees, Slug(j.src))
}

func (j *srcTree) Build(ctx context.Context, e *core.Env, r *core.Runner, work, stage string) error {
	if j.err != nil {
		return j.err
	}
	r.Step("fetching " + j.src.File)
	archive, err := Fetch(ctx, e, j.src)
	if err != nil {
		return err
	}

	r.Step("extracting " + filepath.Base(archive))
	if err := Extract(ctx, archive, stage, j.src); err != nil {
		return err
	}
	if err := ApplyPatches(ctx, r, j.assets, j.src, stage, work); err != nil {
		return err
	}
	if len(j.src.Edits) > 0 {
		r.Step(fmt.Sprintf("applying %d edits to %s", len(j.src.Edits), Slug(j.src)))
		if err := ApplyEdits(j.assets, j.src, stage); err != nil {
			return err
		}
	}
	return nil
}
