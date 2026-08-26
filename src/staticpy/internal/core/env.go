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
// entry of a wholesale Cmd.Env) to have the Runner substitute Env.PathFor. A
// recipe must not compose PATH itself: in hermetic mode the whole point is that
// only the Env decides which directories a command can see, and a recipe that
// pasted in os.Getenv("PATH") would silently reintroduce host tools. Append a
// target triple to name the toolchain the command should see.
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

// EnsureDirs creates the dist/ skeleton and opportunistically collects scratch
// directories left behind by processes that died.
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

// GCStale removes work/ and .staging/ directories whose owning pid is gone and
// which have not been touched for at least age, plus heartbeats left behind by
// dead builders. It is best-effort: losing a race with another collector is
// harmless.
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
			pid, ok := pidFromScratchName(ent.Name())
			if !ok || pidAlive(pid) {
				continue
			}
			info, err := ent.Info()
			if err != nil || time.Since(info.ModTime()) < age {
				continue
			}
			path := filepath.Join(root, ent.Name())
			e.Log.Info("collecting stale scratch dir", "path", path, "dead_pid", pid)
			if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
				e.Log.Warn("could not remove stale dir", "path", path, "err", err)
			}
		}
	}
}

// gcHeartbeats drops heartbeat files whose builder is gone, so `status` never
// reports a build that a crash ended long ago.
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

// ToolchainDir resolves the toolchain tree for a triple. staticpy never builds
// or fetches one: the shim provisions it and we fail loudly rather than falling
// back to whatever compiler the host happens to have.
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

// PathFor is the PATH a build sees. Hermetic mode lists exactly the selected
// toolchain and busybox, so nothing a developer happens to have installed can
// leak into an artifact and change it. Without it the host's PATH follows, which
// is friendlier on a dev box and reproducible nowhere.
//
// A named target that will not resolve is an error, never a silent omission:
// dropping it would hand the build the host compiler under a target triple, or
// no compiler at all, and the failure would surface hundreds of lines later as
// something unrecognisable.
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
	if e.Busybox != "" {
		out = append(out, filepath.Dir(e.Busybox))
	}
	if !e.Hermetic {
		out = append(out, filepath.SplitList(os.Getenv("PATH"))...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("core: hermetic build has an empty PATH; --busybox is unset and no target was named")
	}
	return out, nil
}

// resolvePath expands a PathSentinel value; anything else passes through.
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

// scratchName builds "<slug>.<pid>.<rand>", the naming convention that makes
// abandoned directories attributable to a dead process.
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

// pidAlive reports whether pid names a live process on this machine. A dist/
// shared across machines would defeat this; the mtime guard keeps that case
// merely wasteful rather than dangerous.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
