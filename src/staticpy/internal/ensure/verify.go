package ensure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// Set as an environment variable rather than a flag on purpose: skipping the
// only step that proves the interpreter works should take a deliberate act,
// not a habit picked up from a shell history.
const SkipEnv = "STATICPY_SKIP_VERIFY"

// ReportName is the file the job publishes: the whole report, in JSON, so a CI
// run keeps its evidence after the terminal output is gone.
const ReportName = "report.json"

// DefaultPythonRel is where the interpreter sits inside the interpreter job's
// artifact.
const DefaultPythonRel = "bin/python3"

// SymbolsExpected are the symbols ctypes.pythonapi resolves through. They exist
// only when the build was not stripped, so their absence is reported as a skip
// rather than a failure.
var SymbolsExpected = []string{"Py_GetVersion", "Py_Initialize"}

// Options tune one verification run.
type Options struct {
	// PythonRel is the interpreter's path inside the interpreter artifact.
	PythonRel string
	// Symbols are checked against .symtab when the binary is not stripped.
	Symbols []string
	// WantVersion, if set, is the version prefix sys.version must report.
	WantVersion string
	// Modules overrides the smoke tier's import list.
	Modules []string
	// Jobs is regrtest's -j for the full level.
	Jobs int
	// TestTimeout and SuiteTimeout bound one test and the whole suite.
	TestTimeout  time.Duration
	SuiteTimeout time.Duration
	// WantDynamic is the host-built reference: shared libpython, a PT_INTERP,
	// no staticapi symbol table in the executable.
	WantDynamic bool
}

// A verification job depends on the job that produced the interpreter and
// publishes nothing but its own report, so a failed verification leaves no
// artifact anyone can mistake for a working interpreter.
type Job struct {
	interp  core.Job
	target  config.Target
	profile string
	level   Level
	expect  config.TestExpect
	opts    Options
}

// expect must already be resolved for (target, runner) — see LookupExpect.
func NewJob(interp core.Job, target config.Target, profile string, level Level, expect config.TestExpect, opts Options) *Job {
	if opts.PythonRel == "" {
		opts.PythonRel = DefaultPythonRel
	}
	if opts.Symbols == nil {
		opts.Symbols = SymbolsExpected
	}
	return &Job{interp: interp, target: target, profile: profile, level: level, expect: expect, opts: opts}
}

// checkerVersion invalidates stored reports when the checks themselves change.
// Without it a green report written by a laxer checker outlives the fix that
// tightened it, which is how a verification system lies.
const checkerVersion = "3"

func (j *Job) Name() string { return "verify" }

func (j *Job) Slug() string {
	return fmt.Sprintf("verify:%s:%s:%s", j.profile, j.target.Triple, j.level)
}

func (j *Job) Deps() []core.Job { return []core.Job{j.interp} }

func (j *Job) KeyInputs() map[string]string {
	in := map[string]string{
		"level":   string(j.level),
		"target":  j.target.Triple,
		"profile": j.profile,
		"expect":  ExpectHash(j.expect),
		"python":  j.opts.PythonRel,
		"checker": checkerVersion,
		"dynamic": fmt.Sprintf("%t", j.opts.WantDynamic),
		// The set of tests a level runs is part of what the report claims, so
		// editing CoreTests has to invalidate it. checkerVersion covers the
		// checking code; this covers what was checked.
		"tests": testSetHash(j.level),
	}
	// A skipped verification must never key the same as a real one, or the next
	// build would reuse it and call the interpreter proven.
	if SkipRequested() {
		in["skipped"] = "1"
	}
	return in
}

// LevelFull runs whatever the interpreter ships, so there is no list to hash.
func testSetHash(level Level) string {
	if level != LevelCore {
		return string(level)
	}
	sum := sha256.Sum256([]byte(strings.Join(CoreTests, "\n")))
	return hex.EncodeToString(sum[:8])
}

func (j *Job) ArtifactDir(e *core.Env) string {
	return e.Path(core.DirArtifact, pathSlug(j.Slug()))
}

func SkipRequested() bool { return os.Getenv(SkipEnv) == "1" }

func (j *Job) Build(ctx context.Context, e *core.Env, r *core.Runner, work, stage string) error {
	if SkipRequested() {
		e.Log.Warn("verification skipped",
			"target", j.target.Triple, "level", string(j.level), "env", SkipEnv+"=1")
		rep := NewReport(j.Slug())
		rep.Skip("verify", "%s=1: nothing about this interpreter has been checked", SkipEnv)
		return writeReport(stage, rep)
	}

	dir := j.interp.ArtifactDir(e)
	// Host-built reference publishes a rootfs/ wrapper; pynative is the prefix.
	if st, err := os.Stat(filepath.Join(dir, "rootfs")); err == nil && st.IsDir() {
		dir = filepath.Join(dir, "rootfs")
	}
	python := filepath.Join(dir, j.opts.PythonRel)
	rep, err := Verify(ctx, e, r, j.target, j.level, python, work, j.expect, j.opts)
	if werr := writeReport(stage, rep); werr != nil && err == nil {
		err = werr
	}
	if rep != nil {
		e.Log.Info("verification finished", "target", j.target.Triple, "level", string(j.level),
			"report", rep.String())
	}
	return err
}

// The returned error is the report's, so a caller that only wants the
// evidence can ignore it and print the report.
func Verify(ctx context.Context, e *core.Env, r *core.Runner, t config.Target, level Level, python, work string, expect config.TestExpect, opts Options) (*Report, error) {
	start := time.Now()
	rep := NewReport(fmt.Sprintf("verify %s %s", t.Triple, level))
	defer func() { rep.Dur = time.Since(start) }()

	if opts.PythonRel == "" {
		opts.PythonRel = DefaultPythonRel
	}
	if opts.Symbols == nil {
		opts.Symbols = SymbolsExpected
	}

	if st, err := os.Stat(python); err != nil || st.IsDir() {
		rep.Failf("interpreter", "%s is not a file: the interpreter job did not produce %s",
			python, opts.PythonRel)
		return rep, rep.Err()
	}

	CheckELF(rep, python, t, opts.Symbols, opts.WantDynamic)

	l, err := NewLauncher(e, t)
	if err != nil {
		rep.Fail("runner", err, "")
		return rep, rep.Err()
	}
	rep.Pass("runner", "%s%s", l.Runner, launcherDetail(l))

	if r != nil {
		r.Step("verify " + t.Triple + " " + string(level))
	}

	rep.Absorb("", RunProbes(ctx, r, l, t, python, work, ProbeOptions{
		Modules:     opts.Modules,
		WantVersion: opts.WantVersion,
	}))

	// The suite is worth nothing if the interpreter cannot import its own
	// modules, and running it anyway would bury the real failure under
	// thousands of lines.
	if level == LevelSmoke || !rep.OK() {
		return rep, rep.Err()
	}

	suiteStart := time.Now()
	ignore := make([]string, 0, len(expect.Ignore))
	for _, e := range expect.Ignore {
		ignore = append(ignore, e.Test)
	}
	out, err := RunSuite(ctx, r, l, level, python, work, SuiteOptions{
		Ignore:      ignore,
		Jobs:        opts.Jobs,
		TestTimeout: opts.TestTimeout,
		Timeout:     opts.SuiteTimeout,
	})
	if err != nil {
		rep.Fail("suite", err, "CPython's test suite could not be run")
		return rep, rep.Err()
	}
	if err := out.CheckCoverage(); err != nil {
		rep.FailCmd("suite:coverage", out.Result, err, "%s", l.FailDetail(out.Result))
		return rep, rep.Err()
	}
	if out.Result.TimedOut {
		rep.FailCmd("suite", out.Result, fmt.Errorf("the test suite did not finish"), "%s", l.FailDetail(out.Result))
		return rep, rep.Err()
	}

	class := Classify(out, expect, t.Triple)
	rep.Absorb("", class.Report(time.Since(suiteStart)))
	return rep, rep.Err()
}

func launcherDetail(l *Launcher) string {
	if l.Runner != RunnerQemu {
		return ""
	}
	s := " via " + l.Prefix[0]
	if l.Sysroot != "" {
		s += " -L " + l.Sysroot
	}
	return s
}

func writeReport(stage string, rep *Report) error {
	if rep == nil {
		return nil
	}
	b, err := rep.JSON()
	if err != nil {
		return fmt.Errorf("encode verification report: %w", err)
	}
	path := filepath.Join(stage, ReportName)
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("write verification report to %s: %w", path, err)
	}
	return nil
}

// pathSlug mirrors core's slug-to-path rule: ':' is readable in logs, '_' is
// safe on every filesystem.
func pathSlug(slug string) string {
	out := []rune(slug)
	for i, c := range out {
		if c == ':' || c == '/' {
			out[i] = '_'
		}
	}
	return string(out)
}

var _ core.Job = (*Job)(nil)
