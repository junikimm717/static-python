package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The two shapes a toolchain directory comes in: cross runs on the build
// machine and emits target code, native runs on the target itself.
const (
	KindCross  = "cross"
	KindNative = "native"
)

// PathSentinel is what a recipe puts in Cmd.EnvAdd["PATH"] (or in a "PATH="
// entry of a wholesale Cmd.Env) to have the Runner substitute Env.PathFor.
// A recipe must not compose PATH itself: only PathFor prepends the
// toolchain, and a recipe that pasted in os.Getenv("PATH") would put the
// host gcc first. Append a target triple to name the toolchain the command
// should see.
const PathSentinel = "\x00staticpy-path\x00"

var distSubdirs = []string{
	DirSrc,
	DirSrcTrees,
	DirArtifact,
	DirOut,
	DirBin,
	DirWork,
	DirStaging,
	DirTrash,
	DirLocks,
	filepath.Join(DirLogs, "runs"),
	filepath.Join(DirLogs, "jobs"),
	filepath.Join(DirState, "heartbeats"),
}

// It opportunistically collects scratch directories left behind by processes
// that died.
func (e *Env) EnsureDirs() error {
	if e.Dist == "" {
		return fmt.Errorf("core: Env.Dist is empty")
	}
	abs, err := filepath.Abs(e.Dist)
	if err != nil {
		return fmt.Errorf("core: resolve dist: %w", err)
	}
	if abs != e.Dist { // never write when unchanged: Env is read concurrently
		e.Dist = abs
	}
	for _, d := range distSubdirs {
		if err := os.MkdirAll(e.Path(d), 0o755); err != nil {
			return fmt.Errorf("core: create %s: %w", d, err)
		}
	}
	e.GCStale(StaleAge)
	return nil
}

// It is best-effort: losing a race with another collector is harmless.
// kill(pid, 0) is PID-namespace local, so two containers sharing dist/
// would otherwise treat each other's live LTO trees as dead after StaleAge
// (the scratch dir's mtime is the job start; WPA does not touch it).
// Heartbeat.Live() already accepts a recent UpdatedAt for that case.
func (e *Env) GCStale(age time.Duration) {
	e.gcHeartbeats()
	for _, root := range []string{e.Path(DirWork), e.Path(DirStaging)} {
		ents, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, ent := range ents {
			if !ent.IsDir() {
				continue
			}
			name := ent.Name()
			pid, ok := pidFromScratchName(name)
			if !ok {
				continue
			}
			if slug, ok := slugFromScratchName(name); ok {
				if hb, err := ReadHeartbeat(e, slug); err == nil && hb.Live() {
					continue
				}
			}
			if pidAlive(pid) {
				continue
			}
			info, err := ent.Info()
			if err != nil || time.Since(info.ModTime()) < age {
				continue
			}
			path := filepath.Join(root, name)
			e.Log.Info("collecting stale scratch dir", "path", path, "dead_pid", pid)
			if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
				e.Log.Warn("could not remove stale dir", "path", path, "err", err)
			}
		}
	}
}

// This keeps `status` from reporting a build that a crash ended long ago.
func (e *Env) gcHeartbeats() {
	dir := e.Path(DirState, "heartbeats")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, ent := range ents {
		name := ent.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		hb, err := ReadHeartbeat(e, strings.TrimSuffix(name, ".json"))
		if err == nil && hb.Live() {
			continue
		}
		os.Remove(filepath.Join(dir, name))
	}
}

// staticpy never builds or fetches one: the shim provisions it and we fail
// loudly rather than falling back to whatever compiler the host happens to
// have.
func (e *Env) ToolchainDir(triple, kind string) (string, error) {
	if kind != KindCross && kind != KindNative {
		return "", fmt.Errorf("core: toolchain kind %q for %s: want %q or %q", kind, triple, KindCross, KindNative)
	}
	if triple == "" {
		return "", fmt.Errorf("core: empty triple for %s toolchain", kind)
	}
	if dir, ok := e.Overrides[triple]; ok {
		if !isDir(dir) {
			return "", fmt.Errorf("core: toolchain override for %s is not a directory: %s", triple, dir)
		}
		return dir, nil
	}
	if e.Toolchains == "" {
		return "", fmt.Errorf("core: no toolchain root configured, so %s-%s cannot be resolved; the shim provisions it", triple, kind)
	}
	dir := filepath.Join(e.Toolchains, triple+"-"+kind)
	if !isDir(dir) {
		return "", fmt.Errorf("core: no %s toolchain for %s at %s; the shim provisions it before staticpy runs", kind, triple, dir)
	}
	return dir, nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// PathFor is the selected toolchain's bin (when a target is named) followed
// by the process PATH. The toolchain goes first so gcc/cc/ld resolve to
// gccfactory. Recipes still set CC to an absolute path; this is the
// backstop for configure scripts that look up `gcc` by name.
//
// A named target that will not resolve is an error, never a silent omission:
// dropping it would hand the build the host compiler under a target triple,
// or no compiler at all, and the failure would surface hundreds of lines
// later as something unrecognisable.
func (e *Env) PathFor(target string) ([]string, error) {
	var out []string
	if target != "" {
		dir, err := e.ToolchainDir(target, KindCross)
		if err != nil {
			var nerr error
			if dir, nerr = e.ToolchainDir(target, KindNative); nerr != nil {
				return nil, err
			}
		}
		out = append(out, filepath.Join(dir, "bin"))
	}
	out = append(out, filepath.SplitList(os.Getenv("PATH"))...)
	if len(out) == 0 {
		return nil, fmt.Errorf("core: PATH is empty")
	}
	return out, nil
}

func (e *Env) resolvePath(v string) (string, error) {
	if !strings.HasPrefix(v, PathSentinel) {
		return v, nil
	}
	dirs, err := e.PathFor(strings.TrimPrefix(v, PathSentinel))
	if err != nil {
		return "", err
	}
	return strings.Join(dirs, string(os.PathListSeparator)), nil
}

// The naming convention makes abandoned directories attributable to a dead
// process.
func scratchName(slug string) string {
	return fmt.Sprintf("%s.%d.%s", lockFileName(slug), os.Getpid(), randHex(4))
}

func pidFromScratchName(name string) (int, bool) {
	parts := strings.Split(name, ".")
	if len(parts) < 3 {
		return 0, false
	}
	pid, err := strconv.Atoi(parts[len(parts)-2])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func slugFromScratchName(name string) (string, bool) {
	parts := strings.Split(name, ".")
	if len(parts) < 3 {
		return "", false
	}
	if _, ok := pidFromScratchName(name); !ok {
		return "", false
	}
	return strings.Join(parts[:len(parts)-2], "."), true
}

// Same-namespace only. A sibling container sharing dist/ has a different
// PID namespace, so this is false for a live foreign builder. Prefer
// Heartbeat.Live(), which also accepts a recent UpdatedAt.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
