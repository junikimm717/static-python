package recipe

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// packVersion is the archive layout generation: bump it when the entry order,
// the top-level directory or the header normalisation changes, since none of
// that shows up in the interpreter's key.
const packVersion = "2"

// tarEpoch is the timestamp every entry carries. Unix zero rather than the Go
// zero time: a pre-1970 mtime cannot be written as a plain ustar field, so it
// would force a pax record onto every single entry.
var tarEpoch = time.Unix(0, 0).UTC()

// Pack turns the interpreter prefix into the distributable tarball. after is
// the last job that must succeed first — the verification, when there is one —
// so an unverified interpreter is never packed.
func Pack(cfg *config.Config, target config.Target, profile string, interp, after core.Job) (core.Job, error) {
	src, err := pythonSource(cfg)
	if err != nil {
		return nil, err
	}
	j := &pack{interp: interp, after: after, target: target, profile: profile, version: src.Version}
	if after != nil && after.Slug() == interp.Slug() {
		j.after = nil
	}
	return j, nil
}

type pack struct {
	interp  core.Job
	after   core.Job
	target  config.Target
	profile string
	version string
}

func (j *pack) Name() string { return "pack" }

func (j *pack) Slug() string { return fmt.Sprintf("pack:%s:%s", j.profile, j.target.Triple) }

func (j *pack) Deps() []core.Job {
	if j.after == nil {
		return []core.Job{j.interp}
	}
	return []core.Job{j.interp, j.after}
}

func (j *pack) KeyInputs() map[string]string {
	return map[string]string{
		"recipe_version": strconv.Itoa(Version),
		"pack_version":   packVersion,
		"python_version": j.version,
		"target":         j.target.Triple,
		"profile":        j.profile,
		"archive":        j.archiveName(),
		"topdir":         j.topDir(),
	}
}

func (j *pack) ArtifactDir(e *core.Env) string {
	dir := e.Path(core.DirOut, j.profile, j.target.Triple)
	if p, ok := j.interp.(*pyRef); ok {
		return dir + hostPublishSuffix(p.tc)
	}
	return dir
}

func (j *pack) topDir() string {
	return fmt.Sprintf("python-%s-%s", j.version, j.target.Triple)
}

func (j *pack) archiveName() string {
	return fmt.Sprintf("python-%s-%s-%s.tar.gz", j.version, j.target.Triple, j.profile)
}

func (j *pack) Build(ctx context.Context, e *core.Env, r *core.Runner, work, stage string) error {
	prefix := packContentRoot(j.interp, e)
	archive := filepath.Join(stage, j.archiveName())

	r.Step("packing " + j.archiveName())
	sum, err := writeTarGz(ctx, prefix, j.topDir(), archive)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("%s  %s\n", sum, j.archiveName())
	return os.WriteFile(archive+".sha256", []byte(line), 0o644)
}

// pyref publishes a rootfs/ wrapper around the prefix; pack the prefix, so
// unpacking looks like the static tarball (bin/, lib/) rather than an extra
// directory that only the content-addressed store needs.
func packContentRoot(interp core.Job, e *core.Env) string {
	dir := interp.ArtifactDir(e)
	if sub := filepath.Join(dir, "rootfs"); isDir(sub) {
		return sub
	}
	return dir
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// writeTarGz produces a byte-reproducible archive: entries in sorted path
// order, zeroed timestamps, and uid/gid 0 with no owner names, so the same
// prefix packed on two machines is the same file.
//
// Lib/test is deliberately not excluded. It is 6.4 MB gzipped against a 23 MB
// prefix, and it is what lets whoever has real riscv32 or s390x hardware run
// the suite there — the one question qemu cannot answer.
func writeTarGz(ctx context.Context, root, topDir, dst string) (string, error) {
	entries, err := tarEntries(root)
	if err != nil {
		return "", err
	}

	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	zw, err := gzip.NewWriterLevel(io.MultiWriter(f, h), gzip.BestCompression)
	if err != nil {
		return "", err
	}
	tw := tar.NewWriter(zw)

	for _, ent := range entries {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := writeEntry(tw, root, topDir, ent); err != nil {
			return "", err
		}
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// The manifest is left out: it records a hostname, a pid and a wall-clock
// time, which is exactly what a reproducible archive must not contain.
func tarEntries(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." || rel == core.ManifestName {
			return nil
		}
		if d.IsDir() && d.Name() == "__pycache__" {
			return fs.SkipDir
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func writeEntry(tw *tar.Writer, root, topDir, rel string) error {
	full := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(full)
	if err != nil {
		return err
	}
	link := ""
	if info.Mode()&os.ModeSymlink != 0 {
		if link, err = os.Readlink(full); err != nil {
			return err
		}
	}
	hdr, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}
	hdr.Name = path.Join(topDir, rel)
	if info.IsDir() && !strings.HasSuffix(hdr.Name, "/") {
		hdr.Name += "/"
	}
	hdr.Uid, hdr.Gid = 0, 0
	hdr.Uname, hdr.Gname = "", ""
	hdr.ModTime = tarEpoch
	// Left unset so the writer omits them; a set atime/ctime costs a pax record
	// per entry and says nothing.
	hdr.AccessTime, hdr.ChangeTime = time.Time{}, time.Time{}
	hdr.Format = tar.FormatPAX
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	f, err := os.Open(full)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(tw, f)
	if err != nil {
		return err
	}
	if n != info.Size() {
		return fmt.Errorf("recipe: %s changed size while it was being packed", full)
	}
	return nil
}

var _ core.Job = (*pack)(nil)
