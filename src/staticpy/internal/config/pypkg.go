package config

import (
	"fmt"
	"path"
	"strings"
)

// AsSource is the fetch identity of a pinned sdist, so srctree and
// `staticpy sources` treat it the same way as a native-library tarball.
func (p PyPackage) AsSource() Source {
	return Source{
		Name:    p.Name,
		Version: p.Version,
		File:    p.File,
		TopDir:  p.TopDir,
		URLs:    append([]string(nil), p.URLs...),
		SHA256:  p.SdistSHA256,
	}
}

// ImportNames is what smoke verification tries to import after the
// interpreter is built. An explicit list wins; otherwise the package name
// plus every compiled-in module.
func (p PyPackage) ImportNames() []string {
	if len(p.Imports) > 0 {
		return append([]string(nil), p.Imports...)
	}
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	add(p.Name)
	for _, m := range p.Modules {
		add(m.Name)
	}
	return out
}

// RelPath reports whether p is a source-relative path that cannot escape
// the tree it is resolved against.
func RelPath(p string) error {
	if p == "" {
		return fmt.Errorf("empty path")
	}
	s := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if path.IsAbs(s) || s == ".." || strings.HasPrefix(s, "../") {
		return fmt.Errorf("unsafe path %q", p)
	}
	return nil
}
