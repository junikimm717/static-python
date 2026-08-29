package config

import (
	"embed"
	"io/fs"
)

// The configuration itself, compiled in so a released binary builds without a
// checkout. It lives here rather than at the repo root because go:embed cannot
// reach outside its own package; <repo>/config is a symlink to it.
//
//go:embed defaults
var defaultsFS embed.FS

func embedded() fs.FS {
	sub, err := fs.Sub(defaultsFS, "defaults")
	if err != nil {
		// Only reachable if the embed directive above stops matching.
		panic(err)
	}
	return sub
}
