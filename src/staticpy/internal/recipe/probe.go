package recipe

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/assets"
	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/ensure"
)

const (
	patcherAsset = "patcher.c"
	// ConfigSiteName is the cache CPython's configure reads through CONFIG_SITE.
	ConfigSiteName = "config.site"
)

// PyconfigPatchAsset is the per-target header fragment holding the facts the
// probe cannot produce: inline-asm availability, atomics quirks, anything that
// is a decision rather than a measurement.
func PyconfigPatchAsset(t config.Target) string {
	return path.Join("pyconfig", t.Triple+"-patches.h")
}

// ConfigSite and PyconfigPatches locate the probe's two outputs inside its
// artifact directory.
func ConfigSite(dir string) string { return filepath.Join(dir, ConfigSiteName) }

func PyconfigPatches(dir string, t config.Target) string {
	return filepath.Join(dir, t.Triple+"-patches.h")
}

// Probe measures the target's ABI by compiling patcher.c with the target
// toolchain and running the result. Its output is a config.site, so CPython's
// own configure computes a correct pyconfig.h rather than being bypassed and
// the header patched afterwards.
//
// It is profile-free: sizes and alignments are properties of the ABI, not of
// the flags a build happens to use.
func Probe(cfg *config.Config, srcAssets fs.FS, t config.Target) (core.Job, error) {
	id, err := Toolchain(nil, t.Triple)
	if err != nil {
		return nil, err
	}
	patchAsset := PyconfigPatchAsset(t)
	patches, err := assets.Get(patchAsset)
	if err != nil {
		return nil, fmt.Errorf("recipe: no pyconfig fragment for %s: %w. "+
			"Adding an architecture is a row in targets.toml plus an assets/files/%s", t.Triple, err, patchAsset)
	}
	if _, err := assets.Get(patcherAsset); err != nil {
		return nil, err
	}
	return &probeJob{target: t, tc: id, patches: patches,
		patchHash: hashBytes(patches), patcherHash: assets.Hash(patcherAsset)}, nil
}

type probeJob struct {
	target      config.Target
	tc          ToolchainID
	patches     []byte
	patchHash   string
	patcherHash string
}

func (j *probeJob) Name() string { return "probe" }

func (j *probeJob) Slug() string { return "probe:" + j.target.Triple }

func (j *probeJob) Deps() []core.Job { return nil }

func (j *probeJob) KeyInputs() map[string]string {
	in := map[string]string{
		"recipe_version": strconv.Itoa(Version),
		"target":         j.target.Triple,
		"patcher":        j.patcherHash,
		"pyconfig_patch": j.patchHash,
	}
	for k, v := range j.tc.keyInputs() {
		in[k] = v
	}
	return in
}

func (j *probeJob) ArtifactDir(e *core.Env) string {
	return e.Path(core.DirArtifact, artifactName(j.Slug()))
}

func (j *probeJob) Provenance() map[string]string { return j.tc.Provenance() }

func (j *probeJob) Build(ctx context.Context, e *core.Env, r *core.Runner, work, stage string) error {
	tools, err := toolsFor(j.tc.Dir, j.target.Triple)
	if err != nil {
		return err
	}
	srcPath := filepath.Join(work, patcherAsset)
	if err := os.WriteFile(srcPath, assets.MustGet(patcherAsset), 0o644); err != nil {
		return err
	}
	bin := filepath.Join(work, "patcher")

	// No profile flags: the probe must report the ABI, and it has to be a
	// static binary because qemu-user is the only way to run it for most
	// targets.
	r.Step("compiling the ABI probe for " + j.target.Triple)
	if err := r.Run(ctx, core.Cmd{
		Dir:    work,
		Name:   "patcher-cc",
		Args:   []string{tools.CC, "-static", "-O0", "-o", bin, srcPath},
		EnvAdd: map[string]string{"PATH": core.PathSentinel + j.target.Triple, "LC_ALL": "C"},
	}); err != nil {
		return err
	}

	l, err := ensure.NewLauncher(e, j.target)
	if err != nil {
		return err
	}
	res, err := l.Run(ctx, r, "running the ABI probe for "+j.target.Triple, work, bin)
	if err != nil {
		return err
	}
	if !res.OK() {
		return fmt.Errorf("recipe: the ABI probe for %s did not run: %s\n%s",
			j.target.Triple, l.FailDetail(res), res.Combined())
	}

	probed, order := parseDefines(res.Stdout)
	if err := requireProbed(j.target, probed, res.Stdout); err != nil {
		return err
	}
	// The fragment is allowed to hold nothing but a comment: several targets
	// need no correction at all.
	fromAsset, _ := parseDefines(string(j.patches))
	reportOverrides(e, j.target, probed, fromAsset)

	site, err := configSite(j.target, probed, fromAsset)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, ConfigSiteName), site, 0o644); err != nil {
		return err
	}
	return os.WriteFile(PyconfigPatches(stage, j.target),
		pyconfigFragment(j.target, res.Stdout, order, fromAsset, j.patches), 0o644)
}

// define is one line of the probe's output or of the per-target fragment.
// Value is empty for an #undef.
type define struct {
	Value string
	Undef bool
}

func parseDefines(text string) (map[string]define, []string) {
	out := map[string]define{}
	var order []string
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		var d define
		switch fields[0] {
		case "#define":
			d.Value = strings.Join(fields[2:], " ")
		case "#undef":
			d.Undef = true
		default:
			continue
		}
		name := fields[1]
		if _, dup := out[name]; !dup {
			order = append(order, name)
		}
		out[name] = d
	}
	return out, order
}

// probedMacros are the measurements CPython's configure cannot make for a
// target it cannot run. Losing one silently is how pyconfig.h ends up with a
// host-sized long.
var probedMacros = []string{
	"SIZEOF_INT", "SIZEOF_LONG", "SIZEOF_LONG_LONG", "SIZEOF_VOID_P", "SIZEOF_SHORT",
	"SIZEOF_FLOAT", "SIZEOF_DOUBLE", "SIZEOF_LONG_DOUBLE", "SIZEOF_FPOS_T", "SIZEOF_SIZE_T",
	"SIZEOF_SSIZE_T", "SIZEOF_PID_T", "SIZEOF_UINTPTR_T", "SIZEOF_TIME_T", "SIZEOF_WCHAR_T", "SIZEOF__BOOL",
	"SIZEOF_OFF_T",
	"ALIGNOF_INT", "ALIGNOF_LONG", "ALIGNOF_LONG_LONG", "ALIGNOF_VOID_P", "ALIGNOF_FLOAT",
	"ALIGNOF_DOUBLE", "ALIGNOF_LONG_DOUBLE", "ALIGNOF_SIZE_T", "ALIGNOF_WCHAR_T", "ALIGNOF__BOOL",
}

// probedBooleans must appear in the probe output but may legitimately be an
// #undef, which is a real answer rather than a missing one. These are the
// questions configure settles by running a program, so on a cross build they
// are otherwise guessed: ac_cv_aligned_required in particular defaults to
// "yes", which silently costs every little-endian target the faster hash.
var probedBooleans = []string{
	"HAVE_GCC_UINT128_T", "HAVE_GCC_ASM_FOR_X64", "HAVE_GCC_ASM_FOR_X87",
	"WORDS_BIGENDIAN", "HAVE_ALIGNED_REQUIRED",
	"DOUBLE_IS_BIG_ENDIAN_IEEE754", "DOUBLE_IS_LITTLE_ENDIAN_IEEE754",
}

// Inverted means the variable answers the opposite question, as with the
// little-endian half of the float-endianness pair.
var boolCache = map[string]struct {
	Var      string
	Inverted bool
}{
	"HAVE_GCC_UINT128_T":              {"ac_cv_type___uint128_t", false},
	"HAVE_GCC_ASM_FOR_X64":            {"ac_cv_gcc_asm_for_x64", false},
	"HAVE_GCC_ASM_FOR_X87":            {"ac_cv_gcc_asm_for_x87", false},
	"WORDS_BIGENDIAN":                 {"ac_cv_c_bigendian", false},
	"HAVE_ALIGNED_REQUIRED":           {"ac_cv_aligned_required", false},
	"DOUBLE_IS_BIG_ENDIAN_IEEE754":    {"ax_cv_c_float_words_bigendian", false},
	"DOUBLE_IS_LITTLE_ENDIAN_IEEE754": {"ax_cv_c_float_words_bigendian", true},
}

func requireProbed(t config.Target, probed map[string]define, out string) error {
	if len(probed) == 0 {
		return fmt.Errorf("recipe: the ABI probe for %s printed no #define lines at all; it printed %q",
			t.Triple, strings.TrimSpace(out))
	}
	var missing []string
	for _, m := range probedBooleans {
		if _, ok := probed[m]; !ok {
			missing = append(missing, m)
		}
	}
	for _, m := range probedMacros {
		if d, ok := probed[m]; !ok || d.Undef || d.Value == "" {
			missing = append(missing, m)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("recipe: the ABI probe for %s did not report %s; "+
			"assets/files/%s and this recipe disagree about what it prints",
			t.Triple, strings.Join(missing, ", "), patcherAsset)
	}
	return nil
}

// AS_TR_SH keeps the case of the type name, so _Bool is the one that does
// not simply lowercase.
func autoconfCache(macro string) (string, bool) {
	var kind, name string
	switch {
	case strings.HasPrefix(macro, "SIZEOF_"):
		kind, name = "sizeof", strings.TrimPrefix(macro, "SIZEOF_")
	case strings.HasPrefix(macro, "ALIGNOF_"):
		kind, name = "alignof", strings.TrimPrefix(macro, "ALIGNOF_")
	default:
		if b, ok := boolCache[macro]; ok {
			return b.Var, true
		}
		return "", false
	}
	if name == "_BOOL" {
		return "ac_cv_" + kind + "__Bool", true
	}
	return "ac_cv_" + kind + "_" + strings.ToLower(name), true
}

// reportOverrides names every field where the hand-written fragment contradicts
// what the hardware just reported. The fragment still wins -- forcing a value
// the probe could measure is occasionally deliberate -- but silence here is
// what lets a fragment rot into overriding a correct measurement with a stale
// one, which is how the blanket x87 undef survived into targets that have x87.
//
// A quiet run is also the signal that the redundant half of these fragments can
// be deleted.
func reportOverrides(e *core.Env, t config.Target, probed, fromAsset map[string]define) {
	var names []string
	for macro := range fromAsset {
		if _, measured := probed[macro]; measured {
			names = append(names, macro)
		}
	}
	sort.Strings(names)
	for _, macro := range names {
		want, got := probed[macro], fromAsset[macro]
		if want.Undef == got.Undef && want.Value == got.Value {
			e.Log.Debug("fragment repeats a measured value", "target", t.Triple, "macro", macro,
				"asset", PyconfigPatchAsset(t))
			continue
		}
		e.Log.Warn("fragment overrides what the probe measured",
			"target", t.Triple, "macro", macro,
			"probed", showDefine(want), "fragment", showDefine(got),
			"asset", PyconfigPatchAsset(t))
	}
}

func showDefine(d define) string {
	if d.Undef {
		return "undef"
	}
	if d.Value == "" {
		return "defined"
	}
	return d.Value
}

// configSite is what makes CPython's own configure produce a correct
// pyconfig.h for a target it cannot execute: every test it would have run and
// failed to run is pre-answered here.
func configSite(t config.Target, probed, fromAsset map[string]define) ([]byte, error) {
	merged := map[string]define{}
	for k, v := range probed {
		merged[k] = v
	}
	// The per-target fragment wins: it exists to state what the probe cannot
	// measure, and where it does overlap it is a deliberate correction.
	for k, v := range fromAsset {
		merged[k] = v
	}

	vars := map[string]string{}
	for macro, d := range merged {
		cache, ok := autoconfCache(macro)
		if !ok {
			continue
		}
		if b, ok := boolCache[macro]; ok {
			want := boolYesNo(!d.Undef != b.Inverted)
			// The two DOUBLE_IS_* macros answer one cache variable from opposite
			// sides. Disagreement means the probe contradicted itself, and map
			// order would otherwise decide which lie wins.
			if had, seen := vars[cache]; seen && had != want {
				return nil, fmt.Errorf("recipe: probe for %s is self-contradictory about %s: %s says %q",
					t.Triple, cache, macro, want)
			}
			vars[cache] = want
			continue
		}
		switch {
		case d.Undef:
			return nil, fmt.Errorf("recipe: assets/files/%s undefines %s, but configure has no way to "+
				"measure it when cross-compiling to %s; give it a value or drop the line",
				PyconfigPatchAsset(t), macro, t.Triple)
		default:
			vars[cache] = d.Value
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Generated by staticpy for %s from the ABI probe.\n", t.Triple)
	b.WriteString("# configure cannot run its own test programs for a target it cannot execute;\n")
	b.WriteString("# these cached answers are what make the generated pyconfig.h correct.\n")
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Fprintf(&b, "%s=%s\n", k, vars[k])
	}
	return []byte(b.String()), nil
}

func boolYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// pyconfigFragment is the legacy <triple>-patches.h: the probe's own output
// followed by the per-target asset. A macro the asset also states is dropped
// from the probe half, so the two halves can never redefine each other.
func pyconfigFragment(t config.Target, probeOut string, order []string, fromAsset map[string]define, asset []byte) []byte {
	probed, _ := parseDefines(probeOut)
	var b strings.Builder
	fmt.Fprintf(&b, "/* Generated by staticpy: ABI probe for %s. */\n", t.Triple)
	for _, name := range order {
		if _, overridden := fromAsset[name]; overridden {
			continue
		}
		d := probed[name]
		if d.Undef {
			fmt.Fprintf(&b, "#undef %s\n", name)
			continue
		}
		fmt.Fprintf(&b, "#define %s %s\n", name, d.Value)
	}
	fmt.Fprintf(&b, "\n/* assets/files/%s */\n", PyconfigPatchAsset(t))
	b.Write(asset)
	if len(asset) > 0 && asset[len(asset)-1] != '\n' {
		b.WriteByte('\n')
	}
	return []byte(b.String())
}
