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

	"github.com/junikimm717/static-python/src/staticpy/internal/buildinfo"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
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
	return NewSessionIn(filepath.Join(distDir, "bench"), arch, now)
}

func NewSessionIn(parent, arch string, now time.Time) (*Session, error) {
	stamp := now.UTC().Format("20060102T150405Z")
	dir := filepath.Join(parent, stamp+"-"+arch)
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
	updateLatest(parent, stamp+"-"+arch)
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

// SessionFiles is the set every suite writes. venv/, raw/, and logs/ stay
// on the machine that measured.
var SessionFiles = []string{
	"manifest.json",
	"env.json",
	"report.json",
	"report.md",
	"report.html",
	"skipped.json",
	"timeline.jsonl",
}

// SuiteMap is the suite object on manifest.json and report.json.
// pyperformance records its pins; micro is built-in and has none.
func SuiteMap(name string, pins Pins) map[string]string {
	if name == "" {
		name = SuitePyperformance
	}
	m := map[string]string{"name": name}
	if name == SuitePyperformance {
		pins = pins.withDefaults()
		m["pyperformance"] = pins.Pyperformance
		m["pyperf"] = pins.Pyperf
	}
	return m
}

// SuiteLabel is the one-line suite description in markdown/HTML.
func SuiteLabel(name string, pins Pins) string {
	if name == "" {
		name = SuitePyperformance
	}
	if name == SuitePyperformance {
		pins = pins.withDefaults()
		return fmt.Sprintf("pyperformance %s, pyperf %s", pins.Pyperformance, pins.Pyperf)
	}
	return name
}

// Manifest is the session accounting file. Protocol and the suite object live
// here so a later reader can refuse stale numbers without re-reading the report.
// A kit run also stores kit.json under "kit" and promotes python_version,
// kit_version, triple, and git_revision to the top level.
func Manifest(stamp, baseline string, pins Pins, ids []Identity, skipped []string) map[string]any {
	return ManifestSuite(stamp, baseline, SuitePyperformance, pins, ids, skipped)
}

func ManifestSuite(stamp, baseline, suiteName string, pins Pins, ids []Identity, skipped []string) map[string]any {
	pins = pins.withDefaults()
	if skipped == nil {
		skipped = []string{}
	}
	if ids == nil {
		ids = []Identity{}
	}
	m := map[string]any{
		"stamp":        stamp,
		"baseline":     baseline,
		"protocol":     Protocol,
		"suite":        SuiteMap(suiteName, pins),
		"interpreters": ids,
		"skipped":      skipped,
	}
	if rev := buildinfo.GitRevision; rev != "" {
		m["git_revision"] = rev
	}
	return m
}

// Identity is everything needed to know which binary produced a column.
// Factors exist because a profile name is not a stable description of
// linkage, LTO, allocator or toolchain; those can change under "default".
type Identity struct {
	Label        string            `json:"label"`
	Path         string            `json:"path,omitempty"`
	BinarySHA256 string            `json:"binary_sha256"`
	ArtifactKey  string            `json:"artifact_key,omitempty"`
	Factors      *Factors          `json:"factors,omitempty"`
	Packages     map[string]string `json:"packages,omitempty"`
	Size         int64             `json:"size_bytes,omitempty"`
	Version      string            `json:"version,omitempty"`
	BuildID      string            `json:"build_id,omitempty"`
	Core         *Identity         `json:"core,omitempty"`
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
	id.BinarySHA256 = hex.EncodeToString(h.Sum(nil))
	link, buildID := linkage(real)
	id.BuildID = buildID
	id.Factors = &Factors{Linkage: link}
	id.ArtifactKey = artifactKeyNear(real)
	if lib := sharedCore(real); lib != "" {
		if coreID, err := Identify(label+":libpython", lib); err == nil {
			id.Core = &coreID
		}
	}
	return id, nil
}

// Walk toward the prefix looking for the job stamp. An unpacked pack
// tarball has none; a dist/artifacts tree does.
func artifactKeyNear(path string) string {
	dir := filepath.Dir(path)
	for i := 0; i < 8 && dir != "" && dir != "/" && dir != "."; i++ {
		m, err := core.ReadManifest(dir)
		if err == nil && m.Key != "" {
			return m.Key
		}
		dir = filepath.Dir(dir)
	}
	return ""
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
