package config

import (
	"embed"
	"io/fs"
)

// defaultsFS is a copy of the repo-root config/ tree, so a released binary
// builds without a checkout. The repo-root copy is the editable one; keep the
// two identical.
//
//go:generate rm -rf defaults
//go:generate cp -r ../../../../config defaults
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
