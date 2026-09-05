// Package core is the build engine: content-addressed job keys, the crash-safe
// artifact store, cross-process locking, and the scheduler that turns a job DAG
// into parallel work.
//
// The invariants every other package relies on:
//
//   - An artifact directory is either absent or complete. It is published by a
//     single rename, with its manifest written last.
//   - A job is rebuilt only when its Merkle key changes.
//   - Two staticpy processes may share one dist/ safely.
//
// This file is the contract the rest of the tree compiles against. Adding to it
// is fine; changing a signature here breaks every recipe at once.
package core

import (
	"context"
	"path/filepath"
	"runtime"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/logging"
)

// Subdirectories of dist/.
const (
	DirSrc      = "src"
	DirSrcTrees = "srctrees"
	DirArtifact = "artifacts"
	DirOut      = "out"
	DirWork     = "work"
	DirTmp      = ".tmp"
	DirStaging  = ".staging"
	DirTrash    = ".trash"
	DirLocks    = "locks"
	DirLogs     = "logs"
	DirState    = "state"
	DirBin      = ".bin"
)

// ManifestName is the stamp that makes an artifact directory valid.
const ManifestName = ".staticpy.json"

// StaleAge is how old a pid-tagged scratch dir must be, on top of having a dead
// owner, before startup GC removes it.
const StaleAge = 10 * time.Minute

// Implementations live in internal/recipe.
type Job interface {
	// Name is the recipe family, e.g. "dep" or "pycross".
	Name() string
	// Slug is filesystem-safe, unique per job, and stable across recipe edits.
	// It selects the artifact path, lock file and log directory.
	Slug() string
	// Deps are the jobs whose artifacts this job reads.
	Deps() []Job
	// It must be deterministic, and must not contain any path that varies per
	// run.
	KeyInputs() map[string]string
	// ArtifactDir is the absolute path where the published output lives.
	ArtifactDir(e *Env) string
	// work and stage are fresh empty directories. All deps are guaranteed valid
	// and read-locked for the duration of the call.
	Build(ctx context.Context, e *Env, r *Runner, work, stage string) error
}

// Manifest is the .staticpy.json stamp inside a published artifact. Its
// presence with a matching key is the definition of "built".
type Manifest struct {
	Key         string            `json:"key"`
	Slug        string            `json:"slug"`
	Name        string            `json:"name"`
	Inputs      map[string]string `json:"inputs"`
	Deps        map[string]string `json:"deps"`
	CompletedAt time.Time         `json:"completed_at"`
	BuiltBy     string            `json:"built_by"`
	Duration    string            `json:"duration"`
	// Provenance records inputs that were taken on trust rather than verified:
	// a toolchain with no gccfactory manifest, an explicit --sources overlay.
	// A build that took a weaker path must never look identical to one that did
	// not.
	Provenance map[string]string `json:"provenance,omitempty"`
}

type Cmd struct {
	// Dir is the working directory; required.
	Dir string
	// Env, if non-nil, replaces the environment entirely.
	Env []string
	// EnvAdd overlays variables onto whatever Env resolves to.
	EnvAdd map[string]string
	// Args is the argv; Args[0] is the program.
	Args []string
	// Name labels the log file, e.g. "openssl-configure".
	Name string
	// SoftFail logs a non-zero exit as a warning, not an error. The caller
	// still gets a CmdError; use it when failure is an expected skip
	// (a C-extension requirement on a static interpreter) rather than
	// a dead job.
	SoftFail bool
}

// Set Dist to an absolute path: EnsureDirs will otherwise rewrite it, which is
// unsafe once other goroutines are reading the Env.
type Env struct {
	Dist     string
	RepoRoot string

	// Toolchains is the directory holding one subdir per <triple>-<cross|native>.
	// Provisioned by the shim; staticpy never fetches it. Overrides maps a
	// triple to an explicit path, for testing one target against a hand-built
	// tree.
	Toolchains string
	Overrides  map[string]string

	// Busybox and Qemu are absolute paths to binaries the shim supplied. Qemu is
	// keyed by target triple.
	Busybox string
	Qemu    map[string]string

	// RestrictPath composes PATH from Busybox plus the selected toolchain and
	// nothing else. Off means the process PATH is appended, so the host gcc
	// can win a name lookup. This is not a sandbox.
	RestrictPath bool

	// Host is the build machine's triple. Recipes need it to reach a compiler
	// whose output runs here, for packages that build a helper and then execute
	// it mid-build.
	Host string

	// Offline serves only what dist/src already holds. It rides on Env rather
	// than a package global so a test can run two configurations at once.
	Offline bool

	Jobs       int // -j handed to make
	MaxWorkers int
	KeepWork   bool
	Log        *logging.Logger
}

func (e *Env) Path(parts ...string) string {
	return filepath.Join(append([]string{e.Dist}, parts...)...)
}

func (e *Env) Workers() int {
	if e.MaxWorkers < 1 {
		return 1
	}
	return e.MaxWorkers
}

func (e *Env) MakeJobs() int {
	if e.Jobs < 1 {
		return runtime.NumCPU()
	}
	return e.Jobs
}

// Lock files are never deleted: removing one would break flock identity for
// anyone holding it open.
func (e *Env) LockPath(slug string) string {
	return e.Path(DirLocks, lockFileName(slug)+".lock")
}

func (e *Env) JobLogDir(slug string) string {
	return e.Path(DirLogs, "jobs", lockFileName(slug))
}

// Slugs carry ':' to stay readable in logs and on the CLI; paths get '_' so the
// two never diverge by accident on a filesystem that dislikes colons.
func lockFileName(slug string) string {
	out := []rune(slug)
	for i, r := range out {
		if r == ':' || r == '/' {
			out[i] = '_'
		}
	}
	return string(out)
}

// PlanNode is one resolved job plus its current state, for status and dry-run.
type PlanNode struct {
	Job      Job
	Key      string
	Valid    bool
	Building bool // a live heartbeat exists
}
