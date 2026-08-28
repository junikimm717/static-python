package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

var cmdShell = &command{
	Name:     "shell",
	Short:    "drop into a shell with a job's exact environment and cwd",
	Synopsis: "staticpy shell <job-slug> [--print] [--step NAME]",
	Long: `Reconstructs the environment a job's commands ran under - the cwd and every
variable the recipe overlaid (PATH, CC, CFLAGS, CONFIG_SITE, ...) - and execs
your $SHELL there. This is the fastest way to re-run a failing configure or make
by hand: the log's ` + "`# cmd:`" + ` line is a copy-pasteable argv.

The environment is recovered from the job's latest attempt under
dist/logs/jobs/<slug>/ - its commands.sh, or the header of a step log - so it
works long after the process that ran the job is gone. A hermetic job recorded a
PATH containing only busybox and its toolchain; that is reproduced as written,
which is the point, so expect the host's tools to be absent.

The build tree lives in dist/work/<slug>.<pid>.<rand>/ and is deleted when a job
succeeds. If it is gone, rebuild with ` + "`staticpy build --keep-work`" + ` to keep it.

FLAGS
  --print       print the environment as shell commands instead of exec'ing
  --step NAME   take the environment of this step rather than the last one`,
	Run: runShell,
}

func runShell(g *Global, args []string) error {
	fs := g.flagSet("shell")
	printOnly := fs.Bool("print", false, "print the environment instead of exec'ing a shell")
	step := fs.String("step", "", "take the environment from this step")
	if err := parse(fs, args); err != nil {
		return finish("shell", err)
	}
	if err := g.resolve(); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return usagef("need a job slug.%s", knownSlugsHint(g.Dist))
	}
	slug := fs.Arg(0)

	base := jobLogBase(g.Dist, slug)
	dir, err := latestAttempt(base)
	if err != nil {
		return fmt.Errorf("no recorded environment for job %q under %s: %w.%s", slug, base, err, knownSlugsHint(g.Dist))
	}
	rec, src, err := jobEnv(dir, *step)
	if err != nil {
		return err
	}
	work, note := resolveWorkDir(g.Dist, slug, rec.cwd)

	if *printOnly {
		fmt.Printf("# recovered from %s\ncd %s\n", src, core.ShellQuote([]string{work}))
		for _, k := range sortedKeys(rec.env) {
			fmt.Printf("export %s=%s\n", k, core.ShellQuote([]string{rec.env[k]}))
		}
		return nil
	}

	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", bold("staticpy shell:"), slug)
	fmt.Fprintf(os.Stderr, "  env from  %s  (%d variable%s)\n", src, len(rec.env), plural(len(rec.env)))
	if rec.replaced {
		fmt.Fprintf(os.Stderr, "  %s the job replaced its environment wholesale; only the variables above are set\n", dim("note:"))
	}
	fmt.Fprintf(os.Stderr, "  cwd       %s\n", work)
	if note != "" {
		fmt.Fprintf(os.Stderr, "  %s %s\n", yellow("note:"), note)
	}
	fmt.Fprintf(os.Stderr, "  %s\n\n", dim("`staticpy logs "+slug+" --failed` shows the command that failed; exit to return"))

	if err := os.Chdir(work); err != nil {
		return fmt.Errorf("cd %s: %w", work, err)
	}
	bin, err := exec.LookPath(sh)
	if err != nil {
		return fmt.Errorf("cannot find $SHELL (%s): %w", sh, err)
	}
	return syscall.Exec(bin, []string{filepath.Base(bin), "-i"}, rec.environ(slug))
}

type recordedEnv struct {
	cwd string
	env map[string]string
	// replaced records that the job supplied its whole environment rather than
	// overlaying onto the caller's.
	replaced bool
}

// A replaced environment is honoured as recorded, plus the few variables an
// interactive shell is unusable without - inheriting the rest would quietly
// undo the hermetic PATH we came here to reproduce.
func (r recordedEnv) environ(slug string) []string {
	var out []string
	if !r.replaced {
		out = append(out, os.Environ()...)
	} else {
		for _, k := range []string{"HOME", "TERM", "USER", "SHELL"} {
			if v, ok := os.LookupEnv(k); ok && r.env[k] == "" {
				out = append(out, k+"="+v)
			}
		}
	}
	for _, k := range sortedKeys(r.env) {
		out = append(out, k+"="+r.env[k])
	}
	return append(out, "STATICPY_JOB="+slug)
}

func jobEnv(dir, step string) (recordedEnv, string, error) {
	logs, err := listLogs(dir)
	if err == nil && len(logs) > 0 {
		pick := logs[len(logs)-1]
		if step != "" {
			found := false
			for _, l := range logs {
				if strings.Contains(strings.ToLower(stepName(l)), strings.ToLower(step)) {
					pick, found = l, true
				}
			}
			if !found {
				return recordedEnv{}, "", fmt.Errorf("no step matching %q; this attempt's steps are:\n  %s",
					step, strings.Join(stepNames(logs), "\n  "))
			}
		}
		rec := parseLogHeader(pick)
		if rec.cwd != "" || len(rec.env) > 0 {
			return rec, pick, nil
		}
	}
	script := filepath.Join(dir, "commands.sh")
	if rec, ok := parseCommandsScript(script); ok {
		return rec, script, nil
	}
	return recordedEnv{}, "", fmt.Errorf("no step log or commands.sh with a usable header under %s", dir)
}

// parseLogHeader reads the header core.Runner writes at the top of every step
// log: `# cwd:`, `# env: K=V`, then `# cmd:`, which ends it.
func parseLogHeader(path string) recordedEnv {
	rec := recordedEnv{env: map[string]string{}}
	f, err := os.Open(path)
	if err != nil {
		return rec
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "# cwd:"):
			rec.cwd = strings.TrimSpace(strings.TrimPrefix(line, "# cwd:"))
		case strings.HasPrefix(line, "# env: (replaced"):
			rec.replaced = true
		case strings.HasPrefix(line, "# env:"):
			if k, v, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(line, "# env:")), "="); ok {
				rec.env[k] = v
			}
		case strings.HasPrefix(line, "# cmd:"):
			return rec
		}
	}
	return rec
}

// parseCommandsScript reads the replay script's last recorded command:
// `cd <dir>` followed by `env [-i] K=V ... argv`.
func parseCommandsScript(path string) (recordedEnv, bool) {
	f, err := os.Open(path)
	if err != nil {
		return recordedEnv{}, false
	}
	defer f.Close()
	rec := recordedEnv{env: map[string]string{}}
	ok := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "cd "):
			rec.cwd = shUnquote(strings.TrimSpace(strings.TrimPrefix(line, "cd ")))
			ok = true
		case strings.HasPrefix(line, "env "):
			fields := splitShell(strings.TrimPrefix(line, "env "))
			env := map[string]string{}
			replaced := false
			for _, fld := range fields {
				if fld == "-i" {
					replaced = true
					continue
				}
				k, v, isAssign := strings.Cut(fld, "=")
				if !isAssign || strings.ContainsAny(k, "/ ") {
					break // the argv starts here
				}
				env[k] = v
			}
			if len(env) > 0 {
				rec.env, rec.replaced, ok = env, replaced, true
			}
		}
	}
	return rec, ok
}

// splitShell is enough of a shell word splitter for the quoting core.Runner
// emits: single-quoted words, and nothing else.
func splitShell(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, has := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			inQuote = !inQuote
			has = true
		case (c == ' ' || c == '\t') && !inQuote:
			if has || cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteByte(c)
		}
	}
	if has || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func shUnquote(s string) string {
	parts := splitShell(strings.TrimSpace(s))
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// The recorded cwd is preferred; a surviving scratch dir for the same job is
// the next best thing.
func resolveWorkDir(dist, slug, recorded string) (string, string) {
	if recorded != "" && isDir(recorded) {
		return recorded, ""
	}
	matches, _ := filepath.Glob(filepath.Join(dist, core.DirWork, pathSlug(slug)+".*"))
	sort.Strings(matches)
	for i := len(matches) - 1; i >= 0; i-- {
		if isDir(matches[i]) {
			note := ""
			if recorded != "" {
				note = "the recorded cwd " + recorded + " is gone; landing in the surviving build tree instead"
			}
			return matches[i], note
		}
	}
	note := "the build tree is gone (a job deletes it when it succeeds). Re-run the build with --keep-work to keep it; landing in dist/ instead."
	return dist, note
}
