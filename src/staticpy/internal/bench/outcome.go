package bench

import (
	"os"
	"path/filepath"
)

// Accounting is the session sidecar every suite writes before (and after)
// the numbers exist: what ran, on what host, which binaries.
type Accounting struct {
	Baseline      string
	SuiteName     string
	Pins          Pins
	Identities    []Identity
	Skipped       []string
	Machine       Machine
	Kit           *KitDoc
	PythonVersion string
	Extra         map[string]any
}

// Reports is the full session: accounting plus the comparison table.
// WriteReports is what makes pyperformance and micro land as the same files.
type Reports struct {
	Accounting
	Order   []string
	Rows    []Row
	Geomean map[string]float64
}

func (s *Session) WriteAccounting(a Accounting) error {
	if a.Skipped == nil {
		a.Skipped = []string{}
	}
	man := ManifestSuite(s.Stamp, a.Baseline, a.SuiteName, a.Pins, a.Identities, a.Skipped)
	if a.PythonVersion != "" {
		if _, ok := man["python_version"]; !ok {
			man["python_version"] = a.PythonVersion
		}
	}
	ApplyKitToManifest(man, a.Kit)
	for k, v := range a.Extra {
		man[k] = v
	}
	a.Machine.AttachToManifest(man)
	if err := s.WriteJSON("manifest.json", man); err != nil {
		return err
	}
	if err := s.WriteJSON("env.json", a.Machine); err != nil {
		return err
	}
	return s.WriteJSON("skipped.json", a.Skipped)
}

func (s *Session) WriteReports(r Reports) (string, map[string]any, error) {
	if r.Geomean == nil {
		r.Geomean = map[string]float64{}
	}
	if r.Rows == nil {
		r.Rows = []Row{}
	}
	if err := s.WriteAccounting(r.Accounting); err != nil {
		return "", nil, err
	}
	payload := map[string]any{
		"baseline":            r.Baseline,
		"rows":                r.Rows,
		"geomean_vs_baseline": r.Geomean,
		"protocol":            Protocol,
		"suite":               SuiteMap(r.SuiteName, r.Pins),
	}
	if err := s.WriteJSON("report.json", payload); err != nil {
		return "", nil, err
	}
	rep := SuiteReport{
		SuiteName:     r.SuiteName,
		Baseline:      r.Baseline,
		Order:         r.Order,
		Rows:          r.Rows,
		Geomean:       r.Geomean,
		Machine:       r.Machine,
		Protocol:      Protocol,
		Pins:          r.Pins,
		Identities:    r.Identities,
		Skipped:       len(r.Skipped),
		Kit:           r.Kit,
		PythonVersion: r.PythonVersion,
	}
	md := rep.Markdown()
	if err := os.WriteFile(filepath.Join(s.Dir, "report.md"), []byte(md), 0o644); err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "report.html"), []byte(rep.HTML()), 0o644); err != nil {
		return "", nil, err
	}
	return md, payload, nil
}
