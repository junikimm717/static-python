package bench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Session is one bench run's output directory.
//
// Results are timestamped and never deduplicated. A measurement is not a pure
// function of its inputs, so content-addressing it the way builds are cached
// would serve an old machine state as if it were current.
type Session struct {
	Dir      string
	Stamp    string
	timeline *os.File
	quiet    *os.File
}

func NewSession(distDir, arch string, now time.Time) (*Session, error) {
	stamp := now.UTC().Format("20060102T150405Z")
	dir := filepath.Join(distDir, "bench", stamp+"-"+arch)
	for _, sub := range []string{"raw", "logs", "venv"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, err
		}
	}
	s := &Session{Dir: dir, Stamp: stamp}
	var err error
	if s.timeline, err = os.Create(filepath.Join(dir, "timeline.jsonl")); err != nil {
		return nil, err
	}
	if s.quiet, err = os.Create(filepath.Join(dir, "quiet.jsonl")); err != nil {
		return nil, err
	}
	updateLatest(filepath.Join(distDir, "bench"), stamp+"-"+arch)
	return s, nil
}

func updateLatest(base, name string) {
	link := filepath.Join(base, "latest")
	os.Remove(link)
	os.Symlink(name, link)
}

func (s *Session) Close() error {
	if s.timeline != nil {
		s.timeline.Close()
	}
	if s.quiet != nil {
		s.quiet.Close()
	}
	return nil
}

// Event is one measurement, recorded as it happens.
//
// This is what makes a suspicious number auditable months later: it fixes the
// interleaving order and records what the machine was doing at the time, so
// "was something running on the sibling core when that outlier was taken" has
// an answer.
type Event struct {
	UTC       string  `json:"utc"`
	Arm       string  `json:"arm"`
	Benchmark string  `json:"benchmark"`
	WallSec   float64 `json:"wall_s"`
	OK        bool    `json:"ok"`
	Err       string  `json:"err,omitempty"`
	Load1     float64 `json:"loadavg_1m"`
	PinBusy   float64 `json:"pinned_cpu_busy,omitempty"`
	SibBusy   float64 `json:"sibling_cpu_busy,omitempty"`
}

func (s *Session) Record(e Event) {
	if s.timeline == nil {
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	s.timeline.Write(append(b, '\n'))
	s.timeline.Sync()
}

func (s *Session) RecordQuiet(v any) {
	if s.quiet == nil {
		return
	}
	if b, err := json.Marshal(v); err == nil {
		s.quiet.Write(append(b, '\n'))
	}
}

func (s *Session) WriteJSON(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, name), append(b, '\n'), 0o644)
}

// Identity is everything needed to know which binary produced a column.
type Identity struct {
	Label   string `json:"label"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size_bytes"`
	Linkage string `json:"linkage"`
	Version string `json:"version,omitempty"`
	BuildID string `json:"build_id,omitempty"`
	// Core is the libpython a shared build delegates to; see sharedCore.
	Core *Identity `json:"core,omitempty"`
}

func Identify(label, path string) (Identity, error) {
	id := Identity{Label: label, Path: path}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real = path
	}
	fi, err := os.Stat(real)
	if err != nil {
		return id, err
	}
	id.Size = fi.Size()
	f, err := os.Open(real)
	if err != nil {
		return id, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return id, err
	}
	id.SHA256 = hex.EncodeToString(h.Sum(nil))
	id.Linkage, id.BuildID = linkage(real)
	if lib := sharedCore(real); lib != "" {
		if core, err := Identify(label+":libpython", lib); err == nil {
			id.Core = &core
		}
	}
	return id, nil
}

func loadAvg1() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	var one float64
	fmt.Sscanf(string(b), "%f", &one)
	return one
}
