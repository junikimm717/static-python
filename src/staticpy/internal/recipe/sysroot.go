package recipe

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// Sysroot composes the per-package prefixes into the single -I/-L view CPython
// is configured against. The dependencies stay separately keyed artifacts; this
// job is only the view of them, so bumping one library rebuilds one library.
func Sysroot(cfg *config.Config, assets fs.FS, t config.Target, profile string) (core.Job, error) {
	b := &depBuilder{cfg: cfg, assets: assets, target: t, profile: profile,
		memo: map[string]*depJob{}, onStack: map[string]bool{}}
	j := &sysrootJob{target: t, profile: profile}
	for _, name := range sortedKeys(cfg.Packages) {
		skip, err := cfg.PackageSkipped(name, profile)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		d, err := b.job(name)
		if err != nil {
			return nil, err
		}
		j.deps = append(j.deps, d)
	}
	if len(j.deps) == 0 {
		return nil, fmt.Errorf("recipe: no packages in packages.toml, so there is nothing to compose a sysroot from")
	}
	return j, nil
}

type sysrootJob struct {
	target  config.Target
	profile string
	deps    []*depJob // sorted by package name
}

func (j *sysrootJob) Name() string { return "sysroot" }

func (j *sysrootJob) Slug() string { return "sysroot:" + j.profile + ":" + j.target.Triple }

func (j *sysrootJob) Deps() []core.Job {
	out := make([]core.Job, 0, len(j.deps))
	for _, d := range j.deps {
		out = append(out, d)
	}
	return out
}

func (j *sysrootJob) KeyInputs() map[string]string {
	var names []string
	for _, d := range j.deps {
		names = append(names, d.name)
	}
	return map[string]string{
		"recipe_version": strconv.Itoa(Version),
		"target":         j.target.Triple,
		"profile":        j.profile,
		"packages":       strings.Join(names, "\x00"),
	}
}

func (j *sysrootJob) ArtifactDir(e *core.Env) string {
	return e.Path(core.DirArtifact, artifactName(j.Slug()))
}

func (j *sysrootJob) Build(ctx context.Context, e *core.Env, r *core.Runner, work, stage string) error {
	// Files are rewritten to name the published sysroot, not this pid-tagged
	// staging directory, which stops existing the moment the job finishes.
	final := j.ArtifactDir(e)

	c := &composer{
		e:       e,
		stage:   stage,
		final:   final,
		owner:   map[string]string{},
		rewrite: map[string]string{},
	}
	for _, d := range j.deps {
		c.rewrite[d.ArtifactDir(e)] = final
	}
	for _, d := range j.deps {
		r.Step("composing " + d.name)
		if err := c.merge(d); err != nil {
			return err
		}
	}
	if len(c.skipped) > 0 {
		e.Log.Info("left programs out of the sysroot: they baked in their own prefix and nothing compiles against them",
			"count", len(c.skipped), "first", c.skipped[0])
	}
	return nil
}

// Files are symlinked, so a sysroot costs nothing to build; the ones carrying
// a baked-in prefix are copied and rewritten instead, because writing through
// a symlink would edit a published artifact that other jobs are reading.
type composer struct {
	// programs left out because they baked in a prefix; reported, not silent.
	skipped []string
	e       *core.Env
	stage   string
	final   string
	// owner maps a relative path to the package that placed it.
	owner   map[string]string
	rewrite map[string]string
}

func (c *composer) merge(d *depJob) error {
	root := d.ArtifactDir(c.e)
	return filepath.WalkDir(root, func(p string, ent os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == core.ManifestName {
			return nil
		}
		dst := filepath.Join(c.stage, rel)
		if ent.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if prev, ok := c.owner[rel]; ok {
			return c.collide(prev, d.name, rel, p, dst)
		}
		c.owner[rel] = d.name

		info, err := ent.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, dst)
		}
		return c.place(d, p, dst, rel, info.Mode().Perm())
	})
}

// place symlinks a file into the tree, unless it names one of the dependency
// prefixes: pkg-config .pc files, libtool .la files and the *-config scripts
// all record the prefix they were configured with, and a consumer reading one
// out of the composed tree has to be pointed at the composed tree.
func (c *composer) place(d *depJob, src, dst, rel string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	out, hit := c.rewritten(data)
	if !hit {
		return os.Symlink(src, dst)
	}
	if !isText(data) {
		// A sysroot exists to be compiled and linked against, so an executable
		// that baked in its own prefix is not something any consumer reads --
		// xz's lzmainfo carries the LOCALEDIR it was configured with. Leave it
		// out rather than corrupting it or failing the build over it. A library
		// or a header in the same position is a real problem and still is one.
		if isProgramDir(rel) {
			c.skipped = append(c.skipped, rel)
			return nil
		}
		return fmt.Errorf("recipe: sysroot %s: %s (from %s) records a dependency prefix inside a binary file; "+
			"rewriting it would corrupt it. Configure %s with a prefix it can keep instead",
			c.final, rel, d.name, d.name)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, out, mode); err != nil {
		return err
	}
	return c.assertNoStalePrefix(d, rel, out)
}

// bin and sbin hold programs the dependency built for its own use. Nothing in
// a compile or a link reads them.
func isProgramDir(rel string) bool {
	switch top, _, _ := strings.Cut(filepath.ToSlash(rel), "/"); top {
	case "bin", "sbin", "libexec":
		return true
	}
	return false
}

func (c *composer) rewritten(data []byte) ([]byte, bool) {
	hit := false
	for _, from := range sortedKeys(c.rewrite) {
		if !bytes.Contains(data, []byte(from)) {
			continue
		}
		hit = true
		data = bytes.ReplaceAll(data, []byte(from), []byte(c.rewrite[from]))
	}
	return data, hit
}

// assertNoStalePrefix refuses to ship a file that still points into the store
// after rewriting. Silently publishing one means a downstream configure gets
// -I and -L flags for a directory that belongs to somebody else, or to nobody.
func (c *composer) assertNoStalePrefix(d *depJob, rel string, data []byte) error {
	for _, root := range []string{c.e.Path(core.DirArtifact), c.e.Path(core.DirStaging)} {
		idx := 0
		for {
			at := bytes.Index(data[idx:], []byte(root+string(os.PathSeparator)))
			if at < 0 {
				break
			}
			at += idx
			idx = at + len(root)
			if bytes.HasPrefix(data[at:], []byte(c.final)) {
				continue
			}
			return fmt.Errorf("recipe: sysroot %s: %s (from %s) still names %s after prefix rewriting; "+
				"it would send a consumer outside the composed tree. Add that path shape to the rewrite rather than shipping it",
				c.final, rel, d.name, quoteAround(data, at))
		}
	}
	return nil
}

// quoteAround extracts the offending path for the error message.
func quoteAround(data []byte, at int) string {
	end := at
	for end < len(data) && !strings.ContainsRune(" \t\r\n'\"", rune(data[end])) {
		end++
	}
	return string(data[at:end])
}

// Letting one win is how a header ends up describing a different build of the
// library beside it.
func (c *composer) collide(first, second, rel, src, dst string) error {
	placed, errA := os.ReadFile(dst)
	incoming, errB := os.ReadFile(src)
	if errA == nil && errB == nil {
		// Compare what would be placed, not what is on disk: the first copy may
		// already have had its prefixes rewritten.
		if rw, _ := c.rewritten(incoming); bytes.Equal(placed, rw) {
			return nil
		}
	}
	return fmt.Errorf("recipe: sysroot %s: %s and %s both install %s with different contents. "+
		"One of them has to stop: composing them would pair a header with a library that does not match it",
		c.final, first, second, rel)
}
