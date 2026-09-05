package ensure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// Level is how much of CPython's own test suite to run.
type Level string

const (
	// LevelSmoke is the import probes only: seconds, and it gates every target.
	LevelSmoke Level = "smoke"
	// LevelCore is a curated subset covering the language core plus every
	// extension module staticpy links in itself.
	LevelCore Level = "core"
	// LevelFull is the whole suite.
	LevelFull Level = "full"
)

func Levels() []Level { return []Level{LevelSmoke, LevelCore, LevelFull} }

func ParseLevel(s string) (Level, error) {
	for _, l := range Levels() {
		if string(l) == s {
			return l, nil
		}
	}
	return "", fmt.Errorf("unknown verification level %q: want one of smoke, core, full", s)
}

// The set covers the language core, the containers and numerics where a
// miscompiled target shows up first, and every extension module staticpy has
// to link in by hand — those are the ones a wrong _sysconfigdata or a
// half-linked library breaks.
var CoreTests = []string{
	"test_builtin", "test_int", "test_long", "test_float", "test_complex",
	"test_str", "test_bytes", "test_list", "test_dict",
	"test_os", "test_io", "test_time", "test_math", "test_struct",
	"test_ssl", "test_sqlite3", "test_zlib", "test_bz2", "test_lzma", "test_zstd",
	"test_ctypes", "test_hashlib", "test_hmac", "test_socket", "test_subprocess",
	"test_threading", "test_importlib", "test_zipimport",
}

// DefaultTestTimeout is handed to regrtest as --timeout, so a wedged test is
// killed with a traceback instead of hanging the build.
const DefaultTestTimeout = 20 * time.Minute

// DefaultSuiteTimeout bounds the whole run. It has to exceed DefaultTestTimeout,
// or the suite kills runs regrtest still considers healthy.
func DefaultSuiteTimeout(level Level) time.Duration {
	if level == LevelFull {
		return 8 * time.Hour
	}
	return time.Hour
}

type SuiteOptions struct {
	// Tests overrides the level's default set.
	Tests []string
	// Jobs is regrtest's -j. Zero or one runs serially.
	Jobs int
	// TestTimeout is regrtest's per-test --timeout.
	TestTimeout time.Duration
	// Timeout bounds the whole suite run.
	Timeout time.Duration
	// Ignore becomes one -i per entry, so an impossible method drops out of the
	// run instead of failing the file it lives in.
	Ignore []string
	// Extra flags appended after the generated ones.
	Extra []string
	// PythonArgs is inserted between the interpreter and -m test.
	PythonArgs []string
}

// Outcome is what CPython's runner reported, per test.
type Outcome struct {
	Level  Level  `json:"level"`
	Runner string `json:"runner"`

	Failed     []string `json:"failed,omitempty"`
	Skipped    []string `json:"skipped,omitempty"`
	EnvChanged []string `json:"env_changed,omitempty"`
	Omitted    []string `json:"omitted,omitempty"`
	NoTests    []string `json:"ran_no_tests,omitempty"`
	Passed     int      `json:"passed"`

	// Requested is the set the run was asked for, nil for a full run where the
	// suite chooses. It is what lets an expectation entry outside this level's
	// scope be left alone rather than judged.
	Requested []string `json:"requested,omitempty"`

	Result RunResult     `json:"run"`
	Dur    time.Duration `json:"-"`
}

// TestStatus is what the suite did with one test.
type TestStatus string

const (
	TestPassed TestStatus = "passed"
	TestFailed TestStatus = "failed"
	// TestSkipped covers the suite's own skips: a missing resource, a platform
	// guard, a test file that ran nothing.
	TestSkipped TestStatus = "skipped"
	// TestAbsent means the test was not part of this run at all.
	TestAbsent TestStatus = "absent"
)

// Env-changed counts as a failure: a test that
// leaves the interpreter altered has found a real bug, and letting it pass
// would hide exactly the kind of state corruption a static build introduces.
func (o *Outcome) StatusOf(test string) TestStatus {
	switch {
	case contains(o.Failed, test), contains(o.EnvChanged, test):
		return TestFailed
	case contains(o.Skipped, test), contains(o.Omitted, test), contains(o.NoTests, test):
		return TestSkipped
	case o.Requested == nil, contains(o.Requested, test):
		// A full run runs everything, so a test nobody mentioned passed.
		return TestPassed
	}
	return TestAbsent
}

// A non-zero exit is
// an expected outcome, so it lands in the Outcome rather than the error; the
// error is reserved for not being able to run the suite at all.
func RunSuite(ctx context.Context, r *core.Runner, l *Launcher, level Level, python, work string, opts SuiteOptions) (*Outcome, error) {
	tests := opts.Tests
	if tests == nil {
		switch level {
		case LevelCore:
			tests = CoreTests
		case LevelFull:
			tests = nil
		default:
			return nil, fmt.Errorf("level %q does not run CPython's test suite", level)
		}
	}

	testTimeout := opts.TestTimeout
	if testTimeout <= 0 {
		testTimeout = DefaultTestTimeout
	}

	args := append([]string(nil), opts.PythonArgs...)
	args = append(args, "-m", "test", "--timeout", strconv.Itoa(int(testTimeout.Seconds())))
	if opts.Jobs > 1 {
		args = append(args, "-j", strconv.Itoa(opts.Jobs))
	}
	for _, pat := range opts.Ignore {
		args = append(args, "-i", pat)
	}
	args = append(args, opts.Extra...)
	args = append(args, tests...)

	dir := filepath.Join(work, "suite")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create the suite working directory %s: %w", dir, err)
	}

	prev := l.Timeout
	l.Timeout = opts.Timeout
	if l.Timeout <= 0 {
		l.Timeout = DefaultSuiteTimeout(level)
	}
	start := time.Now()
	res, err := l.Run(ctx, r, "suite-"+string(level), dir, python, args...)
	l.Timeout = prev
	if err != nil {
		return nil, err
	}

	out := &Outcome{Level: level, Runner: l.Runner, Requested: tests, Result: res, Dur: time.Since(start)}
	parseRegrtest(res.Stdout, out)
	return out, nil
}

// Section headers regrtest prints before each list of test names, e.g.
// "10 tests failed:" or "1 test skipped (resource denied):". Re-run sections
// are ignored: a test that still fails after its re-run is also in "failed".
var sectionRE = regexp.MustCompile(`^(\d+) (re-run )?tests?\s*(.*):$`)

var okRE = regexp.MustCompile(`^(?:All )?(\d+) tests? OK\.$`)

func parseRegrtest(stdout string, out *Outcome) {
	lines := strings.Split(stdout, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if m := okRE.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out.Passed, _ = strconv.Atoi(m[1])
			continue
		}
		m := sectionRE.FindStringSubmatch(line)
		if m == nil || m[2] != "" {
			continue
		}
		var bucket *[]string
		switch {
		case m[3] == "failed":
			bucket = &out.Failed
		case strings.HasPrefix(m[3], "skipped"):
			bucket = &out.Skipped
		case m[3] == "omitted":
			bucket = &out.Omitted
		case m[3] == "run no tests":
			bucket = &out.NoTests
		case strings.Contains(m[3], "env changed"):
			bucket = &out.EnvChanged
		default:
			continue
		}
		i = collectNames(lines, i+1, bucket)
	}
}

// regrtest wraps the name list with textwrap at a four-space indent, so the
// section runs until the first line that is not indented.
func collectNames(lines []string, i int, bucket *[]string) int {
	for ; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == "" || !strings.HasPrefix(line, " ") {
			return i - 1
		}
		for _, name := range strings.Fields(line) {
			if !contains(*bucket, name) {
				*bucket = append(*bucket, name)
			}
		}
	}
	return i - 1
}

// Accounted is how many distinct tests came back with any result at all.
func (o *Outcome) Accounted() int {
	seen := map[string]bool{}
	for _, b := range [][]string{o.Failed, o.Skipped, o.EnvChanged, o.Omitted, o.NoTests} {
		for _, t := range b {
			seen[t] = true
		}
	}
	return o.Passed + len(seen)
}

// StatusOf treats a requested test that landed in no bucket as passed, because
// regrtest names the tests that did not pass and counts the ones that did. That
// inference is only sound while every requested test is accounted for, so this
// is the check that has to hold for the rest of the classification to mean
// anything: regrtest prints a summary even when everything fails, so nothing at
// all came back means the suite never ran.
func (o *Outcome) CheckCoverage() error {
	n := o.Accounted()
	if o.Result.TimedOut {
		return fmt.Errorf("the suite ran out of time after %s with %d of %d tests reported; "+
			"it was cut off, not answered", o.Result.Dur.Round(time.Second), n, len(o.Requested))
	}
	if n == 0 {
		return fmt.Errorf("the suite reported no results at all (exit %d), so it never ran; "+
			"a missing Lib/test looks exactly like this", o.Result.ExitCode)
	}
	if want := len(o.Requested); want > 0 && n < want {
		return fmt.Errorf("only %d of the %d requested tests reported a result (exit %d)",
			n, want, o.Result.ExitCode)
	}
	return nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
