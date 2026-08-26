package cli

import (
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// Job states as `status` and `build --dry-run` name them.
const (
	stateOK       = "ok"
	stateStale    = "stale"
	stateBuilding = "building"
	stateMissing  = "missing"
)

// nodeState classifies one planned job. "stale" is the distinction that
// matters: an artifact is there, but its inputs moved, so a build will replace
// it rather than skip it.
func nodeState(e *core.Env, n core.PlanNode) string {
	if n.Building {
		return stateBuilding
	}
	if n.Valid {
		return stateOK
	}
	if _, ok := artifactManifest(n.Job.ArtifactDir(e)); ok {
		return stateStale
	}
	return stateMissing
}

func colorState(s string) string {
	switch s {
	case stateOK:
		return green(s)
	case stateStale:
		return yellow(s)
	case stateBuilding:
		return blue(s)
	}
	return dim(s)
}

// Looked up by name, never by listing the directory: heartbeats are published
// with rename(), and a concurrent readdir can miss an entry that open() still
// resolves.
func liveHeartbeat(e *core.Env, slug string) *core.Heartbeat {
	h, err := core.ReadHeartbeat(e, slug)
	if err != nil || !h.Live() {
		return nil
	}
	if h.Slug == "" {
		h.Slug = slug
	}
	return h
}

func who(h *core.Heartbeat) string {
	if h == nil {
		return ""
	}
	host := h.Host
	if i := strings.IndexByte(host, '.'); i > 0 {
		host = host[:i]
	}
	return strconv.Itoa(h.PID) + "@" + host
}

// A present manifest means "something was published here"; whether it is still
// current is core.Plan's answer, not this one's.
func artifactManifest(dir string) (*core.Manifest, bool) {
	m, err := core.ReadManifest(dir)
	if err != nil {
		return nil, false
	}
	return m, true
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// pathSlug is the on-disk spelling of a job slug. Slugs carry ':' to stay
// readable on the CLI and in logs; core writes their directories with '_', and
// both spellings are accepted here so a slug copied out of either place works.
func pathSlug(slug string) string {
	out := []rune(slug)
	for i, r := range out {
		if r == ':' || r == '/' {
			out[i] = '_'
		}
	}
	return string(out)
}
