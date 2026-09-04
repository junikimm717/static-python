package recipe

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/bench"
	"github.com/junikimm717/static-python/src/staticpy/internal/buildinfo"
	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/sources"
)

// kitVersion is the archive layout generation. Bump it when run, kit.json,
// or the python/<label>/ shape changes.
const kitVersion = "1"

func planKit(cfg *config.Config, assets fs.FS, o PlanOptions) ([]core.Job, error) {
	spec, ok := cfg.Kits[o.Kit]
	if !ok {
		names := make([]string, 0, len(cfg.Kits))
		for n := range cfg.Kits {
			names = append(names, n)
		}
		return nil, fmt.Errorf("recipe: kit %q is not in bench.toml (have %s)", o.Kit, strings.Join(names, ", "))
	}
	o.Targets = []string{o.Host}
	var arms []kitArm
	for _, name := range spec.Arms {
		po := o
		po.Kit = ""
		po.Profile = name
		po.Pack = true
		jobs, err := Plan(cfg, assets, po)
		if err != nil {
			return nil, fmt.Errorf("recipe: kit %q arm %q: %w", o.Kit, name, err)
		}
		if len(jobs) != 1 {
			return nil, fmt.Errorf("recipe: kit %q arm %q: plan returned %d roots, want 1", o.Kit, name, len(jobs))
		}
		p, ok := jobs[0].(*pack)
		if !ok {
			return nil, fmt.Errorf("recipe: kit %q arm %q: root is %s, want pack", o.Kit, name, jobs[0].Slug())
		}
		arms = append(arms, kitArm{label: name, pack: p, interp: p.interp})
	}
	host, ok := cfg.Targets[o.Host]
	if !ok {
		return nil, fmt.Errorf("recipe: host %q is not in targets.toml", o.Host)
	}
	j, err := newKit(cfg, host, o.Kit, spec, arms)
	if err != nil {
		return nil, err
	}
	return []core.Job{j}, nil
}

type kitArm struct {
	label  string
	pack   core.Job
	interp core.Job
}

type kitJob struct {
	name    string
	spec    config.Kit
	target  config.Target
	version string
	arms    []kitArm
	cfg     *config.Config
}

func newKit(cfg *config.Config, target config.Target, name string, spec config.Kit, arms []kitArm) (*kitJob, error) {
	src, err := pythonSource(cfg)
	if err != nil {
		return nil, err
	}
	return &kitJob{
		name:    name,
		spec:    spec,
		target:  target,
		version: src.Version,
		arms:    arms,
		cfg:     cfg,
	}, nil
}

func (j *kitJob) Name() string { return "kit" }

func (j *kitJob) Slug() string { return fmt.Sprintf("kit:%s:%s", j.name, j.target.Triple) }

func (j *kitJob) Deps() []core.Job {
	out := make([]core.Job, 0, len(j.arms))
	for _, a := range j.arms {
		out = append(out, a.pack)
	}
	return out
}

func (j *kitJob) KeyInputs() map[string]string {
	labels := make([]string, len(j.arms))
	for i, a := range j.arms {
		labels[i] = a.label
	}
	return map[string]string{
		"recipe_version": strconv.Itoa(Version),
		"kit_version":    kitVersion,
		"kit":            j.name,
		"baseline":       j.spec.Baseline,
		"arms":           strings.Join(labels, ","),
		"python_version": j.version,
		"target":         j.target.Triple,
		"archive":        j.archiveName(),
		"topdir":         j.topDir(),
		"git_revision":   buildinfo.GitRevision,
		"vendor":         vendorKey(j.cfg.Bench.Vendor),
	}
}

func (j *kitJob) ArtifactDir(e *core.Env) string {
	dir := e.Path(core.DirOut, "kit", j.name, j.target.Triple)
	for _, a := range j.arms {
		if p, ok := a.pack.(*pack); ok {
			if ref, ok := p.interp.(*pyRef); ok {
				return dir + hostPublishSuffix(ref.tc)
			}
		}
	}
	return dir
}

func (j *kitJob) topDir() string {
	return fmt.Sprintf("staticpy-kit-%s-%s", j.version, j.target.Triple)
}

func (j *kitJob) archiveName() string {
	return j.topDir() + ".tar.gz"
}

func (j *kitJob) Build(ctx context.Context, e *core.Env, r *core.Runner, work, stage string) error {
	root := filepath.Join(work, j.topDir())
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "python"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
		return err
	}

	abi, err := pythonABI(j.cfg)
	if err != nil {
		return err
	}

	doc := bench.KitDoc{
		Protocol:      bench.Protocol,
		KitVersion:    kitVersion,
		GitRevision:   buildinfo.GitRevision,
		PythonVersion: j.version,
		Triple:        j.target.Triple,
		Baseline:      j.spec.Baseline,
		Suite:         "pyperformance",
		Pins: bench.Pins{
			Pyperformance: j.cfg.Bench.Pyperformance,
			Pyperf:        j.cfg.Bench.Pyperf,
		},
	}

	for _, a := range j.arms {
		r.Step("stage " + a.label)
		prefix := packContentRoot(a.interp, e)
		dst := filepath.Join(root, "python", a.label)
		if err := copyTree(prefix, dst); err != nil {
			return fmt.Errorf("kit: copy %s: %w", a.label, err)
		}
		bin, err := findStagedPython(dst, abi)
		if err != nil {
			return fmt.Errorf("kit: %s: %w", a.label, err)
		}
		rel, err := filepath.Rel(root, bin)
		if err != nil {
			return err
		}
		id, err := bench.Identify(a.label, bin)
		if err != nil {
			return fmt.Errorf("kit: identify %s: %w", a.label, err)
		}
		f := kitFactors(j.cfg, a.label, j.target)
		id.Factors = &f
		if err := writeJSON(filepath.Join(dst, "identity.json"), id); err != nil {
			return err
		}
		doc.Arms = append(doc.Arms, bench.KitArm{
			Label:        a.label,
			Path:         filepath.ToSlash(rel),
			ArtifactKey:  id.ArtifactKey,
			BinarySHA256: id.BinarySHA256,
			Factors:      id.Factors,
		})
	}

	r.Step("vendor suite")
	if err := vendorSuite(ctx, e, j.cfg, filepath.Join(root, "vendor")); err != nil {
		return err
	}

	r.Step("runner")
	if err := installRunner(root); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "run"), []byte(kitRunScript), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte(kitREADME), 0o644); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(root, "kit.json"), doc); err != nil {
		return err
	}

	r.Step("packing " + j.archiveName())
	archive := filepath.Join(stage, j.archiveName())
	sum, err := writeTarGz(ctx, root, j.topDir(), archive)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("%s  %s\n", sum, j.archiveName())
	return os.WriteFile(archive+".sha256", []byte(line), 0o644)
}

func pythonABI(cfg *config.Config) (string, error) {
	s, err := pythonSource(cfg)
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(s.Version, ".", 3)
	if len(parts) < 2 {
		return "", fmt.Errorf("the pinned python version %q has no major.minor to take an ABI from", s.Version)
	}
	return parts[0] + "." + parts[1], nil
}

func findStagedPython(prefix, abi string) (string, error) {
	p := filepath.Join(prefix, "bin", "python"+abi)
	if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
		return p, nil
	}
	matches, _ := filepath.Glob(filepath.Join(prefix, "bin", "python3.*"))
	for _, m := range matches {
		base := filepath.Base(m)
		if strings.Contains(base, "config") {
			continue
		}
		if st, err := os.Stat(m); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return m, nil
		}
	}
	return "", fmt.Errorf("no interpreter binary under %s/bin", prefix)
}

func kitFactors(cfg *config.Config, profile string, target config.Target) bench.Factors {
	py, err := cfg.Resolve(profile, config.ScopePython)
	if err != nil {
		return bench.Factors{}
	}
	skip, err := cfg.PackageSkipped("mimalloc", profile)
	if err != nil {
		skip = true
	}
	libc := target.ABI
	if libc == "" {
		libc = "musl"
	}
	link := "static"
	if py.HostBuilt() {
		link = "dynamic"
		libc = "glibc"
	}
	return bench.DeriveFactors(bench.FactorOpts{
		HostBuilt:       py.HostBuilt(),
		LTOMode:         py.LTOMode,
		PythonCFlags:    py.CFlags,
		WithLTO:         !py.LTOSet || py.LTO,
		MimallocSkipped: skip,
		Libc:            libc,
		PGO:             py.PGO,
		ELFLinkage:      link,
	})
}

func vendorKey(v map[string]config.VendorPin) string {
	names := make([]string, 0, len(v))
	for n := range v {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		p := v[n]
		parts = append(parts, n+"="+p.File+":"+p.SHA256)
	}
	return strings.Join(parts, ",")
}

func vendorSuite(ctx context.Context, e *core.Env, cfg *config.Config, dir string) error {
	names := make([]string, 0, len(cfg.Bench.Vendor))
	for name := range cfg.Bench.Vendor {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		pin := cfg.Bench.Vendor[name]
		src := config.Source{
			Name:    name,
			Version: pin.Version,
			File:    pin.File,
			SHA256:  pin.SHA256,
			URLs:    pin.URLs,
		}
		path, err := sources.Fetch(ctx, e, src)
		if err != nil {
			return fmt.Errorf("kit: vendor %s: %w", name, err)
		}
		if err := copyFile(path, filepath.Join(dir, pin.File), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func installRunner(root string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("kit: locate this executable: %w", err)
	}
	return copyFile(exe, filepath.Join(root, "bin", "staticpy-bench"), 0o755)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

const kitRunScript = `#!/bin/sh
set -eu
HERE=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
exec "$HERE/bin/staticpy-bench" bench --kit "$HERE" "$@"
`

const kitREADME = `This is a staticpy benchmark kit: several already-built interpreters
and a runner. No git checkout is required.

  ./run                 measure every arm against the kit baseline
  ./run --cpu 3         pin to a logical CPU (not isolation)
  ./run --list          show arms
  ./run --suite micro   stdlib-only loops; no pip

Results land in results/<stamp>-<arch>/ with the same files as any other
session (manifest, env, report.json/md/html, skipped, timeline).
manage_benchmarks.py import does not care which --suite produced them.

python/<profile>/bin/python3.13 is a normal interpreter.
`

var _ core.Job = (*kitJob)(nil)
