package cli

import (
	"bytes"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

// patchTree is the asset tree the recipe hands to internal/sources: paths of
// the form patches/<name>-<version>/<file>. Rather than duplicating the layer
// logic, it delegates to Config.OpenAsset, which already knows whether patches/
// came from the binary or from an explicit --sources overlay.
type patchTree struct{ cfg *config.Config }

func (t patchTree) ReadFile(name string) ([]byte, error) {
	rel, err := t.rel(name)
	if err != nil {
		return nil, err
	}
	return t.cfg.OpenAsset(rel)
}

func (t patchTree) Open(name string) (fs.File, error) {
	b, err := t.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return &memFile{name: path.Base(name), r: bytes.NewReader(b), size: int64(len(b))}, nil
}

func (t patchTree) rel(name string) (string, error) {
	clean := path.Clean(name)
	if !fs.ValidPath(clean) {
		return "", &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if clean != "patches" && !strings.HasPrefix(clean, "patches/") {
		return "", &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return strings.TrimPrefix(clean, "patches/"), nil
}

type memFile struct {
	name string
	r    *bytes.Reader
	size int64
}

func (f *memFile) Stat() (fs.FileInfo, error)         { return memInfo{f.name, f.size}, nil }
func (f *memFile) Read(p []byte) (int, error)         { return f.r.Read(p) }
func (f *memFile) Seek(o int64, w int) (int64, error) { return f.r.Seek(o, w) }
func (f *memFile) Close() error                       { return nil }

type memInfo struct {
	name string
	size int64
}

func (i memInfo) Name() string       { return i.name }
func (i memInfo) Size() int64        { return i.size }
func (i memInfo) Mode() fs.FileMode  { return 0o444 }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return false }
func (i memInfo) Sys() any           { return nil }
