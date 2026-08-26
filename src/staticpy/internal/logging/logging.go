// Package logging provides the single logger used by every staticpy package.
// Every event goes to two places at once: a colorized human stream on stderr,
// and a machine-readable JSONL stream at dist/logs/runs/<ts>-<pid>/run.jsonl so
// a failed run can be replayed and diffed after the fact.
//
// All methods are safe on a nil *Logger, so packages never need to guard.
package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// It gates both output streams.
type Level int8

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	}
	return "?"
}

func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug", "d":
		return LevelDebug, nil
	case "info", "i", "":
		return LevelInfo, nil
	case "warn", "warning", "w":
		return LevelWarn, nil
	case "error", "err", "e":
		return LevelError, nil
	}
	return LevelInfo, fmt.Errorf("unknown log level %q (want debug|info|warn|error)", s)
}

// It is exactly what a JSONL line holds.
type Event struct {
	Time   time.Time      `json:"time"`
	Level  string         `json:"level"`
	Msg    string         `json:"msg"`
	Job    string         `json:"job,omitempty"`
	Step   string         `json:"step,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
}

type Options struct {
	// RunsRoot is dist/logs/runs. Empty disables the JSONL stream.
	RunsRoot string
	// Stderr receives the human stream; defaults to os.Stderr. Use io.Discard
	// to silence it.
	Stderr io.Writer
	// Level is the minimum severity emitted to either stream.
	Level Level
	// Color forces ANSI colors on/off; nil auto-detects a terminal.
	Color *bool
}

type sink struct {
	mu     sync.Mutex
	out    io.Writer
	file   *os.File
	enc    *json.Encoder
	level  Level
	color  bool
	runDir string
}

// Logger writes to both streams. Derive scoped loggers with With/Named; they
// share the underlying streams.
type Logger struct {
	s      *sink
	fields map[string]any
}

// Close it when the run ends.
func New(o Options) (*Logger, error) {
	s := &sink{out: o.Stderr, level: o.Level}
	if s.out == nil {
		s.out = os.Stderr
	}
	if o.Color != nil {
		s.color = *o.Color
	} else {
		s.color = autoColor(s.out)
	}
	if o.RunsRoot != "" {
		dir, err := uniqueDir(o.RunsRoot, fmt.Sprintf("%s-%d", time.Now().UTC().Format("20060102T150405Z"), os.Getpid()))
		if err != nil {
			return nil, fmt.Errorf("create run log dir: %w", err)
		}
		f, err := os.OpenFile(filepath.Join(dir, "run.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open run.jsonl: %w", err)
		}
		s.runDir, s.file = dir, f
		s.enc = json.NewEncoder(f)
		s.enc.SetEscapeHTML(false)
	}
	return &Logger{s: s}, nil
}

// Useful in tests.
func Discard() *Logger {
	return &Logger{s: &sink{out: io.Discard, level: LevelError + 1}}
}

// Two runs starting in the same second therefore never share a run.jsonl.
func uniqueDir(root, name string) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	for i := 0; ; i++ {
		dir := filepath.Join(root, name)
		if i > 0 {
			dir = filepath.Join(root, fmt.Sprintf("%s.%d", name, i))
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

func autoColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// RunDir is the directory holding run.jsonl, or "" if none.
func (l *Logger) RunDir() string {
	if l == nil || l.s == nil {
		return ""
	}
	return l.s.runDir
}

func (l *Logger) Close() error {
	if l == nil || l.s == nil || l.s.file == nil {
		return nil
	}
	l.s.mu.Lock()
	defer l.s.mu.Unlock()
	err := l.s.file.Close()
	l.s.file, l.s.enc = nil, nil
	return err
}

func (l *Logger) Enabled(lv Level) bool {
	if l == nil || l.s == nil {
		return false
	}
	return lv >= l.s.level
}

// The keys "job" and "step" are promoted to dedicated event fields.
func (l *Logger) With(kv ...any) *Logger {
	if l == nil || l.s == nil {
		return l
	}
	f := make(map[string]any, len(l.fields)+len(kv)/2)
	for k, v := range l.fields {
		f[k] = v
	}
	mergeKV(f, kv)
	return &Logger{s: l.s, fields: f}
}

func (l *Logger) Named(slug string) *Logger { return l.With("job", slug) }

func (l *Logger) Debug(msg string, kv ...any) { l.emit(LevelDebug, msg, kv) }

func (l *Logger) Info(msg string, kv ...any) { l.emit(LevelInfo, msg, kv) }

func (l *Logger) Warn(msg string, kv ...any) { l.emit(LevelWarn, msg, kv) }

func (l *Logger) Error(msg string, kv ...any) { l.emit(LevelError, msg, kv) }

func mergeKV(dst map[string]any, kv []any) {
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			k = fmt.Sprint(kv[i])
		}
		dst[k] = kv[i+1]
	}
	if len(kv)%2 == 1 {
		dst["!extra"] = fmt.Sprint(kv[len(kv)-1])
	}
}

func (l *Logger) emit(lv Level, msg string, kv []any) {
	if !l.Enabled(lv) {
		return
	}
	f := make(map[string]any, len(l.fields)+len(kv)/2)
	for k, v := range l.fields {
		f[k] = v
	}
	mergeKV(f, kv)

	ev := Event{Time: time.Now().UTC(), Level: lv.String(), Msg: msg}
	if s, ok := f["job"].(string); ok {
		ev.Job = s
		delete(f, "job")
	}
	if s, ok := f["step"].(string); ok {
		ev.Step = s
		delete(f, "step")
	}
	if len(f) > 0 {
		ev.Fields = f
	}
	l.write(lv, ev)
}

func (l *Logger) write(lv Level, ev Event) {
	s := l.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enc != nil {
		if err := s.enc.Encode(ev); err != nil {
			// A field was not JSON-encodable; keep the record, drop the fields.
			ev.Fields = map[string]any{"!encode_error": err.Error()}
			_ = s.enc.Encode(ev)
		}
	}
	fmt.Fprintln(s.out, humanLine(lv, ev, s.color))
}

const (
	ansiReset = "\x1b[0m"
	ansiDim   = "\x1b[90m"
	ansiBold  = "\x1b[1m"
)

func levelColor(lv Level) string {
	switch lv {
	case LevelDebug:
		return ansiDim
	case LevelWarn:
		return "\x1b[33m"
	case LevelError:
		return "\x1b[31m"
	}
	return "\x1b[36m"
}

var levelLabel = map[Level]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO ",
	LevelWarn:  "WARN ",
	LevelError: "ERROR",
}

func humanLine(lv Level, ev Event, color bool) string {
	var b strings.Builder
	paint := func(c, s string) {
		if color {
			b.WriteString(c)
			b.WriteString(s)
			b.WriteString(ansiReset)
			return
		}
		b.WriteString(s)
	}
	paint(ansiDim, ev.Time.Local().Format("15:04:05"))
	b.WriteByte(' ')
	paint(levelColor(lv), levelLabel[lv])
	b.WriteByte(' ')
	if ev.Job != "" {
		paint(ansiBold, ev.Job)
		b.WriteByte(' ')
	}
	b.WriteString(ev.Msg)
	if ev.Step != "" {
		b.WriteString(" ")
		paint(ansiDim, "step="+ev.Step)
	}
	if len(ev.Fields) > 0 {
		keys := make([]string, 0, len(ev.Fields))
		for k := range ev.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteByte(' ')
			paint(ansiDim, fmt.Sprintf("%s=%v", k, ev.Fields[k]))
		}
	}
	return b.String()
}
