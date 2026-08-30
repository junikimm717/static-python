// Package assets holds the files staticpy injects into a CPython source tree:
// the Setup.local base block, the staticapi module, the target-ABI probe, the
// ctypes fragments and the per-target pyconfig headers.
//
// They are embedded rather than read from the repo so a released binary builds
// the same interpreter with no checkout present.
package assets

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

//go:embed all:files
var embedded embed.FS

func Get(name string) ([]byte, error) {
	clean := strings.TrimPrefix(path.Clean("/"+name), "/")
	b, err := embedded.ReadFile(path.Join("files", clean))
	if err != nil {
		return nil, fmt.Errorf("assets: %q: %w", name, err)
	}
	return b, nil
}

// MustGet is for asset names fixed at compile time; a miss is a build-system
// bug, never a user error.
func MustGet(name string) []byte {
	b, err := Get(name)
	if err != nil {
		panic(err)
	}
	return b
}

func List(prefix string) []string {
	var out []string
	fs.WalkDir(embedded, "files", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := p[len("files/"):]
		if strings.HasPrefix(name, prefix) {
			out = append(out, name)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func WriteTo(dir, name string) error {
	b, err := Get(name)
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

var hashes sync.Map

// Hash is the sha256 of an asset, for folding into a job key. It panics on an
// unknown name: silently keying a job on the empty string would make a stale
// artifact look valid.
func Hash(name string) string {
	if v, ok := hashes.Load(name); ok {
		return v.(string)
	}
	sum := sha256.Sum256(MustGet(name))
	h := hex.EncodeToString(sum[:])
	hashes.Store(name, h)
	return h
}
