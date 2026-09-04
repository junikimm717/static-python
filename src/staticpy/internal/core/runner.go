package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/logging"
)

// TailLines is how much of a failed command's output is quoted in the error.
const TailLines = 60

// Runner executes every command of one build attempt and leaves behind enough
// evidence to debug and to reproduce it by hand:
//
//	dist/logs/jobs/<slug>/<attempt>/NNN-<step>.log   one per command, with header
//	dist/logs/jobs/<slug>/<attempt>/commands.sh      replayable shell script
//	dist/logs/jobs/<slug>/latest -> <attempt>
type Runner struct {
	e    *Env
	slug string
	dir  string
	log  *logging.Logger

	mu     sync.Mutex
	n      int
	step   string
	script *os.File
}

func NewRunner(e *Env, slug string) (*Runner, error) {
	base := e.JobLogDir(slug)
	dir, err := uniqueDir(base, fmt.Sprintf("%s-%d", time.Now().UTC().Format("20060102T150405Z"), os.Getpid()))
	if err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	r := &Runner{e: e, slug: slug, dir: dir, log: e.Log.Named(slug), step: "start"}
	if err := r.openScript(); err != nil {
		return nil, err
	}
	linkLatest(base, dir)
	return r, nil
}

func (r *Runner) openScript() error {
	f, err := os.OpenFile(filepath.Join(r.dir, "commands.sh"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create commands.sh: %w", err)
	}
	fmt.Fprintf(f, "#!/bin/sh\n# Replay of job %s, attempt %s.\n# Every command below ran exactly as written.\nset -eux\n\n",
		r.slug, filepath.Base(r.dir))
	r.script = f
	return nil
}

// Attempt directories are never reused, so no previous attempt's logs are
// ever lost.
func uniqueDir(base, name string) (string, error) {
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	for i := 0; ; i++ {
		dir := filepath.Join(base, name)
		if i > 0 {
			dir = filepath.Join(base, fmt.Sprintf("%s.%d", name, i))
		}
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
}

// linkLatest atomically repoints <base>/latest at dir.
func linkLatest(base, dir string) {
	tmp := filepath.Join(base, fmt.Sprintf(".latest.%d.%s", os.Getpid(), randHex(3)))
	if err := os.Symlink(filepath.Base(dir), tmp); err != nil {
		return
	}
	if err := os.Rename(tmp, filepath.Join(base, "latest")); err != nil {
		os.Remove(tmp)
	}
}

func (r *Runner) Dir() string { return r.dir }

func (r *Runner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.script == nil {
		return nil
	}
	err := r.script.Close()
	r.script = nil
	return err
}

// Step shows up in the heartbeat and the live UI.
func (r *Runner) Step(name string) {
	r.mu.Lock()
	r.step = name
	if r.script != nil {
		fmt.Fprintf(r.script, "\n# ===== step: %s =====\n", name)
	}
	r.mu.Unlock()
	r.log.Info("step", "step", name)
}

func (r *Runner) CurrentStep() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.step
}

func (r *Runner) Log() *logging.Logger { return r.log }

// Run executes c, streaming merged stdout+stderr into a per-step log file. On
// failure it returns a *CmdError carrying the log path and the output tail.
func (r *Runner) Run(ctx context.Context, c Cmd) error {
	_, err := r.run(ctx, c, false)
	return err
}

// Output is Run plus the captured stdout+stderr, for commands whose output is
// itself data (e.g. `cc -dumpmachine`). It is logged exactly the same way.
func (r *Runner) Output(ctx context.Context, c Cmd) (string, error) {
	return r.run(ctx, c, true)
}

func (r *Runner) run(ctx context.Context, c Cmd, capture bool) (string, error) {
	if len(c.Args) == 0 {
		return "", fmt.Errorf("core: Runner.Run with empty Args")
	}
	name := c.Name
	if name == "" {
		name = filepath.Base(c.Args[0])
	}

	r.mu.Lock()
	r.n++
	n := r.n
	step := r.step
	r.mu.Unlock()

	logName := fmt.Sprintf("%03d-%s.log", n, sanitize(name))
	logPath := filepath.Join(r.dir, logName)
	f, err := os.Create(logPath)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", logPath, err)
	}
	defer f.Close()

	env, explicit, err := buildEnv(r.e, c)
	if err != nil {
		return "", err
	}
	start := time.Now()
	writeHeader(f, r.slug, step, name, c, explicit, start)
	r.appendScript(n, logName, name, c, explicit)

	tail := newTailWriter(TailLines)
	ws := []io.Writer{f, tail}
	var buf *bytes.Buffer
	if capture {
		buf = &bytes.Buffer{}
		ws = append(ws, buf)
	}
	if r.log.Enabled(logging.LevelDebug) {
		ws = append(ws, newLineWriter(func(line string) {
			r.log.Debug(line, "step", step, "cmd", name)
		}))
	}
	out := io.MultiWriter(ws...)

	cmd := exec.CommandContext(ctx, c.Args[0], c.Args[1:]...)
	cmd.Dir = c.Dir
	cmd.Env = env
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Stdin = nil
	// Every command gets its own process group so cancelling reaches the whole
	// tree. make's children are our grandchildren, and killing the supervisor
	// alone leaves them running: nine orphaned lto1 processes holding 18GB
	// survived a cancelled cross build and had to be reaped by hand.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid signals the group. SIGKILL rather than SIGTERM: an lto1
		// deep in a link ignores polite requests, and a cancelled job's output
		// is discarded anyway.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	// A grandchild that left the group -- the suite double-forks, and qemu's
	// children come back reparented to init -- survives that kill still holding
	// the write end of these pipes, and Wait blocks on the copy goroutine
	// forever. WaitDelay closes the pipes and gives up.
	cmd.WaitDelay = 10 * time.Second

	r.log.Debug("exec", "step", step, "cmd", ShellQuote(c.Args), "dir", c.Dir, "log", logPath)
	runErr := cmd.Run()
	dur := time.Since(start)
	code := exitCode(runErr)
	fmt.Fprintf(f, "\n# exit: %d\n# duration: %s\n# finished: %s\n", code, dur.Round(time.Millisecond), time.Now().UTC().Format(time.RFC3339))
	if runErr != nil && code == 0 {
		fmt.Fprintf(f, "# error: %v\n", runErr)
	}

	if runErr != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s: canceled: %w", name, ctx.Err())
		}
		ce := &CmdError{
			Cmd: append([]string(nil), c.Args...), Dir: c.Dir,
			ExitCode: code, LogPath: logPath, Tail: tail.String(), Step: step,
			Job: r.slug, Duration: dur,
		}
		if code == 0 {
			ce.ExitCode = -1
			ce.Tail = strings.TrimRight(ce.Tail+"\n"+runErr.Error(), "\n")
		}
		if c.SoftFail {
			r.log.Warn("command failed", "step", step, "cmd", name, "exit", ce.ExitCode, "log", logPath)
		} else {
			r.log.Error("command failed", "step", step, "cmd", name, "exit", ce.ExitCode, "log", logPath)
		}
		outStr := ""
		if buf != nil {
			outStr = buf.String()
		}
		return outStr, ce
	}
	r.log.Debug("command ok", "step", step, "cmd", name, "duration", dur.Round(time.Millisecond))
	if capture {
		return buf.String(), nil
	}
	return "", nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 0
}

// buildEnv returns the final environment plus the subset a human needs to
// reproduce the command (the explicit overlay, or everything if Env replaces
// the environment wholesale). PathSentinel values are expanded here so the log
// header and commands.sh show the PATH that actually ran.
func buildEnv(e *Env, c Cmd) (env []string, explicit []string, err error) {
	if c.Env != nil {
		for _, kv := range c.Env {
			if strings.HasPrefix(kv, "PATH=") {
				p, perr := e.resolvePath(strings.TrimPrefix(kv, "PATH="))
				if perr != nil {
					return nil, nil, perr
				}
				kv = "PATH=" + p
			}
			env = append(env, kv)
		}
		explicit = append([]string(nil), env...)
		sort.Strings(explicit)
		return env, explicit, nil
	}
	env = os.Environ()
	// -flto-partition=none hands the assembler one .s per binary: ~5GB resident
	// and as much on disk for a full interpreter. The default TMPDIR is often a
	// RAM-backed tmpfs, where two concurrent links are enough to exhaust it and
	// fail the build with "Quota exceeded"; dist/ is on real disk by
	// construction. The resident half is capped by memWorkers, not here.
	if _, set := c.EnvAdd["TMPDIR"]; !set {
		tmp := e.Path(DirTmp)
		if err := os.MkdirAll(tmp, 0o755); err != nil {
			return nil, nil, err
		}
		env = append(env, "TMPDIR="+tmp)
		explicit = append(explicit, "TMPDIR="+tmp)
	}
	keys := make([]string, 0, len(c.EnvAdd))
	for k := range c.EnvAdd {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := c.EnvAdd[k]
		if k == "PATH" {
			if v, err = e.resolvePath(v); err != nil {
				return nil, nil, err
			}
		}
		kv := k + "=" + v
		env = append(env, kv)
		explicit = append(explicit, kv)
	}
	return env, explicit, nil
}

func writeHeader(w io.Writer, slug, step, name string, c Cmd, explicit []string, start time.Time) {
	fmt.Fprintf(w, "# job: %s\n# step: %s\n# name: %s\n# cwd: %s\n", slug, step, name, c.Dir)
	if c.Env != nil {
		fmt.Fprintf(w, "# env: (replaced, %d vars)\n", len(explicit))
	}
	for _, kv := range explicit {
		fmt.Fprintf(w, "# env: %s\n", kv)
	}
	fmt.Fprintf(w, "# cmd: %s\n# started: %s\n\n", ShellQuote(c.Args), start.UTC().Format(time.RFC3339))
}

func (r *Runner) appendScript(n int, logName, name string, c Cmd, explicit []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.script == nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# [%03d] %s  (log: %s)\n", n, name, logName)
	fmt.Fprintf(&b, "cd %s\n", shellQuoteOne(c.Dir))
	prefix := ""
	if c.Env != nil {
		prefix = "env -i "
	} else if len(explicit) > 0 {
		prefix = "env "
	}
	if prefix != "" {
		var parts []string
		for _, kv := range explicit {
			parts = append(parts, shellQuoteOne(kv))
		}
		prefix += strings.Join(parts, " ") + " "
	}
	fmt.Fprintf(&b, "%s%s\n\n", prefix, ShellQuote(c.Args))
	io.WriteString(r.script, b.String())
}

func ShellQuote(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = shellQuoteOne(a)
	}
	return strings.Join(parts, " ")
}

func shellQuoteOne(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("@%_-+=:,./", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "cmd"
	}
	return b.String()
}

type CmdError struct {
	Cmd      []string
	Dir      string
	ExitCode int
	LogPath  string
	Tail     string
	Job      string
	Step     string
	Duration time.Duration
}

func (e *CmdError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "command failed (exit %d) in job %s, step %s\n", e.ExitCode, e.Job, e.Step)
	fmt.Fprintf(&b, "  cwd: %s\n  cmd: %s\n", e.Dir, ShellQuote(e.Cmd))
	if t := strings.TrimRight(e.Tail, "\n"); t != "" {
		fmt.Fprintf(&b, "--- last %d lines ---\n%s\n---\n", TailLines, t)
	}
	fmt.Fprintf(&b, "full log: %s", e.LogPath)
	return b.String()
}

type tailWriter struct {
	mu   sync.Mutex
	n    int
	ring []string
	part []byte
}

func newTailWriter(n int) *tailWriter { return &tailWriter{n: n} }

func (t *tailWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.part = append(t.part, p...)
	for {
		i := bytes.IndexByte(t.part, '\n')
		if i < 0 {
			break
		}
		t.push(string(bytes.TrimRight(t.part[:i], "\r")))
		t.part = t.part[i+1:]
	}
	if len(t.part) > 1<<20 {
		t.push(string(t.part))
		t.part = nil
	}
	return len(p), nil
}

func (t *tailWriter) push(line string) {
	t.ring = append(t.ring, line)
	if len(t.ring) > t.n {
		t.ring = t.ring[len(t.ring)-t.n:]
	}
}

func (t *tailWriter) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := append([]string(nil), t.ring...)
	if len(t.part) > 0 {
		out = append(out, string(t.part))
		if len(out) > t.n {
			out = out[len(out)-t.n:]
		}
	}
	return strings.Join(out, "\n")
}

type lineWriter struct {
	mu  sync.Mutex
	buf []byte
	fn  func(string)
}

func newLineWriter(fn func(string)) *lineWriter { return &lineWriter{fn: fn} }

func (l *lineWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		l.fn(string(bytes.TrimRight(l.buf[:i], "\r")))
		l.buf = l.buf[i+1:]
	}
	return len(p), nil
}

// RecordRun exists because verification runs the target under an emulator
// with stdout and stderr kept apart and a non-zero exit treated as data
// rather than a build failure, neither of which Run can express.
func (r *Runner) RecordRun(name, dir string, argv, explicit []string, stdout, stderr string, code int, start time.Time, dur time.Duration) string {
	if len(argv) == 0 {
		return ""
	}
	r.mu.Lock()
	r.n++
	n := r.n
	step := r.step
	r.mu.Unlock()

	logName := fmt.Sprintf("%03d-%s.log", n, sanitize(name))
	logPath := filepath.Join(r.dir, logName)
	f, err := os.Create(logPath)
	if err != nil {
		r.log.Warn("cannot record command output", "cmd", name, "err", err)
		return ""
	}
	defer f.Close()

	c := Cmd{Name: name, Args: argv, Dir: dir}
	writeHeader(f, r.slug, step, name, c, explicit, start)
	r.appendScript(n, logName, name, c, explicit)
	io.WriteString(f, stdout)
	if stderr != "" {
		io.WriteString(f, "\n# ----- stderr -----\n"+stderr)
	}
	fmt.Fprintf(f, "\n# exit: %d\n# duration: %s\n# finished: %s\n",
		code, dur.Round(time.Millisecond), time.Now().UTC().Format(time.RFC3339))
	return logPath
}
