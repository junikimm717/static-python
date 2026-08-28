// Package ensure is staticpy's verification layer. It proves that a built
// interpreter is what it claims to be — statically linked, built for the right
// machine, able to import every module the recipe promised, and in agreement
// with CPython's own test suite — rather than trusting that the build exited
// zero.
//
// Everything here reports through a Report: a flat list of named checks that
// renders to a terminal and to JSON. A failing check carries the command that
// produced it and a bounded tail of its output, because the whole point of this
// package is the moment something breaks.
package ensure

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// Bounds on the output kept with a failed check. Enough to see a traceback,
// little enough that a report stays readable and a stored report.json stays a
// report rather than a log.
const (
	maxTailLines = 40
	maxTailBytes = 8 << 10
)

type Check struct {
	Name   string        `json:"name"`
	Status Status        `json:"status"`
	Dur    time.Duration `json:"-"`
	Detail string        `json:"detail,omitempty"`

	// Set on failure: the command that produced it, where it ran, and the tail
	// of what it printed.
	Cmd    []string `json:"cmd,omitempty"`
	Dir    string   `json:"dir,omitempty"`
	Output string   `json:"output,omitempty"`

	Err error `json:"-"`
}

func (c Check) OK() bool { return c.Status != StatusFail }

type Report struct {
	Subject string
	Checks  []Check
	Dur     time.Duration
}

func NewReport(subject string) *Report { return &Report{Subject: subject} }

func (r *Report) Add(c Check) {
	if c.Status == "" {
		if c.Err != nil {
			c.Status = StatusFail
		} else {
			c.Status = StatusPass
		}
	}
	c.Output = Tail(c.Output)
	r.Checks = append(r.Checks, c)
}

func (r *Report) Pass(name, format string, a ...any) {
	r.Add(Check{Name: name, Status: StatusPass, Detail: sprintf(format, a...)})
}

func (r *Report) PassIn(name string, d time.Duration, format string, a ...any) {
	r.Add(Check{Name: name, Status: StatusPass, Dur: d, Detail: sprintf(format, a...)})
}

// A skip is neither a pass nor a failure and is printed distinctly, so nobody
// reads a wall of green and assumes the check ran.
func (r *Report) Skip(name, format string, a ...any) {
	r.Add(Check{Name: name, Status: StatusSkip, Detail: sprintf(format, a...)})
}

func (r *Report) Failf(name, format string, a ...any) {
	r.Add(Check{Name: name, Status: StatusFail, Err: fmt.Errorf("%s", sprintf(format, a...))})
}

func (r *Report) Fail(name string, err error, format string, a ...any) {
	r.Add(Check{Name: name, Status: StatusFail, Err: err, Detail: sprintf(format, a...)})
}

// FailCmd records a failure whose evidence is a command's own output.
func (r *Report) FailCmd(name string, res RunResult, err error, format string, a ...any) {
	r.Add(Check{
		Name:   name,
		Status: StatusFail,
		Dur:    res.Dur,
		Err:    err,
		Detail: sprintf(format, a...),
		Cmd:    res.Argv,
		Dir:    res.Dir,
		Output: res.Combined(),
	})
}

// Absorb folds a sub-report in, optionally namespacing its check names.
func (r *Report) Absorb(prefix string, sub *Report) {
	if sub == nil {
		return
	}
	for _, c := range sub.Checks {
		if prefix != "" {
			c.Name = prefix + c.Name
		}
		r.Checks = append(r.Checks, c)
	}
}

func (r *Report) OK() bool {
	for _, c := range r.Checks {
		if !c.OK() {
			return false
		}
	}
	return true
}

func (r *Report) Counts() (pass, fail, skip int) {
	for _, c := range r.Checks {
		switch c.Status {
		case StatusSkip:
			skip++
		case StatusFail:
			fail++
		default:
			pass++
		}
	}
	return
}

func (r *Report) Failures() []Check {
	var out []Check
	for _, c := range r.Checks {
		if !c.OK() {
			out = append(out, c)
		}
	}
	return out
}

func (r *Report) String() string {
	var b strings.Builder
	pass, fail, skip := r.Counts()
	fmt.Fprintf(&b, "%s: %d passed, %d failed", r.Subject, pass, fail)
	if skip > 0 {
		fmt.Fprintf(&b, ", %d skipped", skip)
	}
	if r.Dur > 0 {
		fmt.Fprintf(&b, " (%s)", r.Dur.Round(time.Millisecond))
	}
	b.WriteByte('\n')

	width := 0
	for _, c := range r.Checks {
		if n := utf8.RuneCountInString(c.Name); n > width && n <= 44 {
			width = n
		}
	}
	for _, c := range r.Checks {
		mark := "x"
		switch c.Status {
		case StatusSkip:
			mark = "-"
		case StatusPass:
			mark = "+"
		}
		fmt.Fprintf(&b, "  %s %s", mark, pad(c.Name, width))
		if c.OK() && c.Detail != "" {
			b.WriteString("  " + firstLine(c.Detail))
		}
		if c.OK() && c.Dur >= 100*time.Millisecond {
			fmt.Fprintf(&b, "  (%s)", c.Dur.Round(time.Millisecond))
		}
		b.WriteByte('\n')
		if c.OK() {
			continue
		}
		for _, line := range evidence(c) {
			b.WriteString("      " + line + "\n")
		}
	}
	return b.String()
}

func evidence(c Check) []string {
	var out []string
	add := func(s string) {
		if s == "" {
			return
		}
		out = append(out, strings.Split(strings.TrimRight(s, "\n"), "\n")...)
	}
	if c.Err != nil {
		add(c.Err.Error())
	}
	add(c.Detail)
	if len(c.Cmd) > 0 {
		add("cmd: " + ShJoin(c.Cmd))
	}
	if c.Dir != "" {
		add("cwd: " + c.Dir)
	}
	if c.Output != "" {
		add("output:")
		for _, l := range strings.Split(strings.TrimRight(c.Output, "\n"), "\n") {
			out = append(out, "  "+l)
		}
	}
	return out
}

type reportError struct{ r *Report }

func (e *reportError) Error() string {
	pass, fail, _ := e.r.Counts()
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d of %d checks failed\n", e.r.Subject, fail, pass+fail)
	for _, c := range e.r.Failures() {
		b.WriteString("  x " + c.Name + "\n")
		for _, line := range evidence(c) {
			b.WriteString("      " + line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// Err is nil when the report passed, else an error listing every failure with
// the evidence that produced it.
func (r *Report) Err() error {
	if r.OK() {
		return nil
	}
	return &reportError{r}
}

type jsonCheck struct {
	Check
	Error string `json:"error,omitempty"`
	Ms    int64  `json:"ms"`
}

type jsonReport struct {
	Subject string      `json:"subject"`
	OK      bool        `json:"ok"`
	Passed  int         `json:"passed"`
	Failed  int         `json:"failed"`
	Skipped int         `json:"skipped"`
	Ms      int64       `json:"ms"`
	Checks  []jsonCheck `json:"checks"`
}

func (r *Report) MarshalJSON() ([]byte, error) {
	pass, fail, skip := r.Counts()
	out := jsonReport{
		Subject: r.Subject, OK: r.OK(),
		Passed: pass, Failed: fail, Skipped: skip,
		Ms:     r.Dur.Milliseconds(),
		Checks: make([]jsonCheck, 0, len(r.Checks)),
	}
	for _, c := range r.Checks {
		jc := jsonCheck{Check: c, Ms: c.Dur.Milliseconds()}
		if c.Err != nil {
			jc.Error = c.Err.Error()
		}
		out.Checks = append(out.Checks, jc)
	}
	return json.Marshal(out)
}

func (r *Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

// Tail bounds text kept with a failed check, keeping the end: a traceback's
// last lines say what went wrong, its first lines say where it started.
func Tail(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	if len(s) > maxTailBytes {
		s = "... (" + fmt.Sprint(len(s)-maxTailBytes) + " earlier bytes omitted)\n" + s[len(s)-maxTailBytes:]
	}
	lines := strings.Split(s, "\n")
	if len(lines) > maxTailLines {
		lines = append([]string{fmt.Sprintf("... (%d earlier lines omitted)", len(lines)-maxTailLines)},
			lines[len(lines)-maxTailLines:]...)
	}
	return strings.Join(lines, "\n")
}

func sprintf(format string, a ...any) string {
	if format == "" {
		return ""
	}
	if len(a) == 0 {
		return format
	}
	return fmt.Sprintf(format, a...)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " ..."
	}
	return s
}

func pad(s string, w int) string {
	if n := utf8.RuneCountInString(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// ShJoin renders an argv the way a shell would accept it, so a failure can be
// re-run by copy and paste.
func ShJoin(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = shQuote(a)
	}
	return strings.Join(out, " ")
}

func shQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("-_./=:+,@", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
