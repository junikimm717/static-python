package ensure

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

// ExpectKey names the expectation set for one target and one runner.
// Expectations are keyed per runner because qemu-user has its own failures
// around signals, threads and subprocesses that say nothing about whether the
// build is correct; a skip earned under qemu must not silence the same test
// running natively.
func ExpectKey(triple, runner string) string { return triple + ":" + runner }

// LookupExpect resolves the expectations for (triple, runner), merging the
// triple-wide entries with the runner-specific ones. A bare triple key applies
// to both runners; "<triple>:qemu" applies only under qemu.
func LookupExpect(all map[string]config.TestExpect, triple, runner string) config.TestExpect {
	var out config.TestExpect
	for _, key := range []string{triple, ExpectKey(triple, runner)} {
		e, ok := all[key]
		if !ok {
			continue
		}
		out.Skip = append(out.Skip, e.Skip...)
		out.Fail = append(out.Fail, e.Fail...)
	}
	return out
}

// Verdict is one test judged against what was expected of it.
type Verdict struct {
	Test     string     `json:"test"`
	Observed TestStatus `json:"observed"`
	// Why is the operator's recorded reason, carried through so a stale entry
	// can be found and deleted by the same string that justified it.
	Why string `json:"why,omitempty"`
}

// Classified is the four buckets a run sorts into. Only the first two decide
// the exit status.
type Classified struct {
	Target string `json:"target"`
	Runner string `json:"runner"`
	Level  Level  `json:"level"`

	UnexpectedFailures []Verdict `json:"unexpected_failures,omitempty"`
	UnexpectedPasses   []Verdict `json:"unexpected_passes,omitempty"`
	ExpectedFailures   []Verdict `json:"expected_failures,omitempty"`
	ExpectedSkips      []Verdict `json:"expected_skips,omitempty"`

	Passed int `json:"passed"`
}

func (c *Classified) OK() bool {
	return len(c.UnexpectedFailures) == 0 && len(c.UnexpectedPasses) == 0
}

// Classify sorts a run against its expectations.
//
// An unexpected pass fails the run. If test_re starts passing because musl
// fixed byte-level case folding, the entry is stale and the operator has to be
// told; a skip list that only ever grows is how a suite quietly stops meaning
// anything.
func Classify(o *Outcome, expect config.TestExpect, target string) *Classified {
	c := &Classified{Target: target, Runner: o.Runner, Level: o.Level, Passed: o.Passed}

	skipWhy := whyMap(expect.Skip)
	failWhy := whyMap(expect.Fail)

	judged := map[string]bool{}
	judge := func(test, why string, wantFail bool) {
		if judged[test] {
			return
		}
		judged[test] = true

		status := o.StatusOf(test)
		if status == TestAbsent {
			// The test is outside this level's scope; the entry is neither
			// confirmed nor stale, so say nothing about it.
			return
		}
		v := Verdict{Test: test, Observed: status, Why: why}
		switch {
		case status == TestPassed:
			c.UnexpectedPasses = append(c.UnexpectedPasses, v)
		case wantFail:
			c.ExpectedFailures = append(c.ExpectedFailures, v)
		case status == TestSkipped:
			c.ExpectedSkips = append(c.ExpectedSkips, v)
		default:
			// Declared as a skip, but it ran and failed: the entry describes
			// something other than what the suite is doing now.
			c.UnexpectedFailures = append(c.UnexpectedFailures, v)
		}
	}

	for _, test := range sortedKeys(failWhy) {
		judge(test, failWhy[test], true)
	}
	for _, test := range sortedKeys(skipWhy) {
		judge(test, skipWhy[test], false)
	}

	for _, test := range o.Failed {
		if !judged[test] {
			c.UnexpectedFailures = append(c.UnexpectedFailures, Verdict{Test: test, Observed: TestFailed})
		}
	}
	for _, test := range o.EnvChanged {
		if !judged[test] && !contains(o.Failed, test) {
			c.UnexpectedFailures = append(c.UnexpectedFailures, Verdict{
				Test: test, Observed: TestFailed, Why: "altered the execution environment",
			})
		}
	}
	sortVerdicts(c.UnexpectedFailures)
	sortVerdicts(c.UnexpectedPasses)
	return c
}

// Report renders the classification as checks. The two buckets that decide the
// exit status become failures; the two that do not become one summary line
// each, so an operator can see the size of the skip list without reading it.
func (c *Classified) Report(dur time.Duration) *Report {
	rep := NewReport(fmt.Sprintf("%s suite %s (%s)", c.Level, c.Target, c.Runner))
	rep.Dur = dur

	key := ExpectKey(c.Target, c.Runner)
	for _, v := range c.UnexpectedFailures {
		rep.Failf("suite:"+v.Test, "failed, and nothing in the expectations for %s says it should. "+
			"Fix the build, or record it under [expect.\"%s\"] with a reason for why it cannot pass",
			key, key)
	}
	for _, v := range c.UnexpectedPasses {
		rep.Failf("suite:"+v.Test, "passed, but the expectations for %s still list it (%s). "+
			"Delete the entry: a skip list that only ever grows is how a suite quietly stops meaning anything",
			key, orDash(v.Why))
	}
	if len(c.ExpectedFailures) > 0 {
		rep.Pass("suite:expected-failures", "%d as declared: %s",
			len(c.ExpectedFailures), joinTests(c.ExpectedFailures))
	}
	if len(c.ExpectedSkips) > 0 {
		rep.Skip("suite:expected-skips", "%d as declared: %s",
			len(c.ExpectedSkips), joinTests(c.ExpectedSkips))
	}
	if c.OK() {
		rep.Pass("suite:result", "%d tests passed, %d expected failures, %d expected skips",
			c.Passed, len(c.ExpectedFailures), len(c.ExpectedSkips))
	}
	return rep
}

func (c *Classified) Err() error {
	if c.OK() {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s suite on %s (%s): ", c.Level, c.Target, c.Runner)
	var parts []string
	if n := len(c.UnexpectedFailures); n > 0 {
		parts = append(parts, fmt.Sprintf("%d unexpected failure(s): %s", n, joinTests(c.UnexpectedFailures)))
	}
	if n := len(c.UnexpectedPasses); n > 0 {
		parts = append(parts, fmt.Sprintf("%d unexpected pass(es): %s", n, joinTests(c.UnexpectedPasses)))
	}
	b.WriteString(strings.Join(parts, "; "))
	return fmt.Errorf("%s", b.String())
}

// ExpectHash identifies an expectation set for a job key. Only test names are
// hashed: editing the reason attached to an entry changes nothing about what
// the run will do, and forcing a re-verification for a reworded comment would
// make people stop writing them.
func ExpectHash(expect config.TestExpect) string {
	names := make([]string, 0, len(expect.Skip)+len(expect.Fail))
	for _, e := range expect.Skip {
		names = append(names, "skip:"+e.Test)
	}
	for _, e := range expect.Fail {
		names = append(names, "fail:"+e.Test)
	}
	sort.Strings(names)
	sum := sha256.Sum256([]byte(strings.Join(names, "\n")))
	return hex.EncodeToString(sum[:8])
}

func whyMap(entries []config.TestEntry) map[string]string {
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.Test == "" {
			continue
		}
		out[e.Test] = e.Why
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortVerdicts(v []Verdict) {
	sort.Slice(v, func(i, j int) bool { return v[i].Test < v[j].Test })
}

func joinTests(v []Verdict) string {
	out := make([]string, len(v))
	for i, x := range v {
		out[i] = x.Test
	}
	return strings.Join(out, " ")
}

func orDash(s string) string {
	if s == "" {
		return "no reason recorded"
	}
	return s
}
