package recipe

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/assets"
	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/ensure"
	"github.com/junikimm717/static-python/src/staticpy/internal/gen"
	"github.com/junikimm717/static-python/src/staticpy/internal/sources"
)

// bootstrapProfile is the only profile pyhost is ever built with. pyhost is a
// means and not an output, so tying it to the requested profile would give
// every profile its own copy of a binary nobody ships.
const bootstrapProfile = "bootstrap"

// pythonSource is the pinned CPython release every job in this file works from.
func pythonSource(cfg *config.Config) (config.Source, error) {
	s, ok := cfg.Sources["python"]
	if !ok {
		return config.Source{}, fmt.Errorf("recipe: sources.toml declares no [source.python]")
	}
	return s, nil
}

// PyHost is the interpreter the build machine runs during a cross build, and
// the one gen.StaticAPI uses to read Misc/stable_abi.toml.
//
// It is built with the *native* gccfactory toolchain and linked static: that
// toolchain targets musl while the build machine is usually glibc, so a dynamic
// binary would look for a loader that is not installed. Static is safe here
// because pyhost never imports a .so — every module it uses is a builtin.
func PyHost(cfg *config.Config, srcAssets fs.FS, host config.Target) (core.Job, error) {
	src, err := pythonSource(cfg)
	if err != nil {
		return nil, err
	}
	res, err := resolveScope(cfg, bootstrapProfile, config.ScopePyhost)
	if err != nil {
		return nil, err
	}
	setup, err := gen.SetupLocal(res, nil)
	if err != nil {
		return nil, fmt.Errorf("recipe: pyhost Setup.local: %w", err)
	}
	tc, err := ToolchainNative(nil, host.Triple)
	if err != nil {
		return nil, fmt.Errorf("recipe: pyhost needs a native toolchain for %s: %w", host.Triple, err)
	}
	return &pyHost{
		srctree: sources.SrcTree(src, sources.Options{Assets: srcAssets}),
		host:    host,
		version: src.Version,
		res:     res,
		setup:   setup,
		tc:      tc,
	}, nil
}

type pyHost struct {
	srctree core.Job
	host    config.Target
	version string
	res     config.Resolved
	setup   []byte

	// tc is filled in during Build; core reads it back through Provenance so a
	// build that had to fingerprint its compiler is distinguishable from one
	// that did not.
	tc ToolchainID
}

func (j *pyHost) Provenance() map[string]string { return j.tc.Provenance() }

func (j *pyHost) Name() string { return "pyhost" }

func (j *pyHost) Slug() string { return "pyhost:" + j.version }

func (j *pyHost) Deps() []core.Job { return []core.Job{j.srctree} }

func (j *pyHost) KeyInputs() map[string]string {
	in := map[string]string{
		"recipe_version": strconv.Itoa(Version),
		"python_version": j.version,
		"setup_local":    hashBytes(j.setup),
		// The slug carries no triple, so the machine it runs on has to reach the
		// key: a dist/ shared between two architectures must not hand one of
		// them the other's interpreter.
		"host": j.host.Triple,
		// pyhost's only dep is the srctree, so nothing else carries the compiler
		// into its key. Without this a toolchain re-publish rebuilds every dep
		// but silently reuses a build-python from the old one.
		"toolchain":        j.tc.Key,
		"toolchain_source": j.tc.Source,
	}
	for k, v := range j.res.KeyInputs() {
		in["profile_"+k] = v
	}
	return in
}

func (j *pyHost) ArtifactDir(e *core.Env) string {
	return e.Path(core.DirArtifact, artifactName(j.Slug()))
}

func (j *pyHost) Build(ctx context.Context, e *core.Env, r *core.Runner, work, stage string) error {
	src := filepath.Join(work, "src")
	r.Step("copying the CPython tree")
	if err := copyTree(j.srctree.ArtifactDir(e), src); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(src, core.ManifestName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.WriteFile(filepath.Join(src, "Modules", "Setup.local"), j.setup, 0o644); err != nil {
		return err
	}

	prefix := j.ArtifactDir(e)
	// No dependency view: pyhost links nothing but libc. deepfreeze imports only
	// the standard library, and freeze_modules reaches for hashlib, which falls
	// back to the builtin _sha2 and _md5 when _hashlib is absent — so there is
	// no OpenSSL here, and not even zlib.
	te, err := newToolenv(e, j.host, j.res, prefix, nil)
	if err != nil {
		return err
	}
	if j.tc, err = ToolchainNative(e, j.host.Triple); err != nil {
		return err
	}

	r.Step("configuring pyhost")
	args := []string{
		"./configure",
		"--prefix=" + prefix,
		"--exec-prefix=" + prefix,
		"--build=" + j.host.Triple,
		"--disable-shared",
		// Every stdlib extension is a builtin, never a .so. configure defaults
		// this to shared and only forces static for wasm -- which needs it for
		// the same reason we do, no dlopen. Without it CPython links modules
		// with `-shared` on top of our -static LDFLAGS and the contradiction
		// surfaces as "undefined reference to main".
		"MODULE_BUILDTYPE=static",
		"--with-ensurepip=no",
		"--disable-test-modules",
	}
	if err := r.Run(ctx, te.cmd("pyhost-configure", src, args, nil)); err != nil {
		return err
	}

	r.Step("building pyhost")
	makeArgs := []string{"make", "-j", strconv.Itoa(e.MakeJobs())}
	if err := r.Run(ctx, te.cmd("pyhost-make", src, makeArgs, nil)); err != nil {
		return err
	}
	return installPrefix(ctx, r, te, src, work, stage, prefix, nil)
}

// PyNative builds the shipped interpreter for the machine it runs on.
func PyNative(cfg *config.Config, srcAssets fs.FS, target config.Target, profile, bundle string) (core.Job, error) {
	return newPyBuild(cfg, srcAssets, target, target, profile, bundle, false)
}

// PyCross builds the shipped interpreter for a target the build machine cannot
// execute.
func PyCross(cfg *config.Config, srcAssets fs.FS, host, target config.Target, profile, bundle string) (core.Job, error) {
	return newPyBuild(cfg, srcAssets, host, target, profile, bundle, true)
}

// Native and cross differ by four configure arguments, so keeping them one
// job is what stops the cross path from drifting away from the native one
// that gets all the testing.
type pyBuild struct {
	cross bool

	srctree   core.Job
	sysroot   core.Job
	staticapi core.Job
	probe     core.Job
	// buildPython is pyhost, and only pyhost. A cross build needs a runnable
	// same-version interpreter to freeze bytecode with, and that is all it
	// needs; waiting on a full PGO release build of the host would gate one
	// target on an hour of unrelated work.
	buildPython core.Job

	host    config.Target
	target  config.Target
	profile string
	version string
	bundle  string
	res     config.Resolved
	setup   []byte

	tc ToolchainID
}

func (j *pyBuild) Provenance() map[string]string { return j.tc.Provenance() }

func newPyBuild(cfg *config.Config, srcAssets fs.FS, host, target config.Target, profile, bundle string, cross bool) (core.Job, error) {
	src, err := pythonSource(cfg)
	if err != nil {
		return nil, err
	}
	res, err := resolveScope(cfg, profile, config.ScopePython)
	if err != nil {
		return nil, err
	}
	if bundle == "" {
		bundle = res.Bundle
	}
	extra, err := bundleModules(cfg, bundle)
	if err != nil {
		return nil, err
	}
	setup, err := gen.SetupLocal(res, extra)
	if err != nil {
		return nil, fmt.Errorf("recipe: Setup.local for %s: %w", target.Triple, err)
	}

	srctree := sources.SrcTree(src, sources.Options{Assets: srcAssets})
	pyhost, err := PyHost(cfg, srcAssets, host)
	if err != nil {
		return nil, err
	}
	sysroot, err := Sysroot(cfg, srcAssets, target, profile)
	if err != nil {
		return nil, err
	}
	probe, err := Probe(cfg, srcAssets, target)
	if err != nil {
		return nil, err
	}

	j := &pyBuild{
		cross:     cross,
		srctree:   srctree,
		sysroot:   sysroot,
		staticapi: gen.NewStaticAPI(srctree, pyhost, src.Version),
		probe:     probe,
		host:      host,
		target:    target,
		profile:   profile,
		version:   src.Version,
		bundle:    bundle,
		res:       res,
		setup:     setup,
	}
	j.buildPython = pyhost
	return j, nil
}

func (j *pyBuild) Name() string {
	if j.cross {
		return "pycross"
	}
	return "pynative"
}

func (j *pyBuild) Slug() string {
	if j.cross {
		return fmt.Sprintf("pycross:%s:%s:%s", j.profile, j.host.Triple, j.target.Triple)
	}
	return fmt.Sprintf("pynative:%s:%s", j.profile, j.target.Triple)
}

func (j *pyBuild) Deps() []core.Job {
	deps := []core.Job{j.srctree, j.sysroot, j.staticapi, j.probe}
	if j.buildPython != nil {
		deps = append(deps, j.buildPython)
	}
	return deps
}

func (j *pyBuild) KeyInputs() map[string]string {
	in := map[string]string{
		"recipe_version": strconv.Itoa(Version),
		"python_version": j.version,
		"setup_local":    hashBytes(j.setup),
		"bundle":         j.bundle,
		"target":         j.target.Triple,
		"arch":           j.target.Arch,
		"bits":           strconv.Itoa(j.target.Bits),
		"libatomic":      strconv.FormatBool(j.target.Libatomic),
		"uint128":        strconv.FormatBool(j.target.UInt128),
		"configure":      strings.Join(j.decisionFlags(), " "),
		"staticapi_c":    assets.Hash("staticapi/staticapi.c"),
	}
	if j.cross {
		in["build"] = j.host.Triple
	}
	if len(j.target.MakeVars) > 0 {
		in["make_vars"] = strings.Join(j.target.MakeVars, " ")
	}
	for k, v := range j.res.KeyInputs() {
		in["profile_"+k] = v
	}
	return in
}

func (j *pyBuild) ArtifactDir(e *core.Env) string {
	return e.Path(core.DirArtifact, artifactName(j.Slug()))
}

// pgo reports whether this build trains itself. CPython runs PROFILE_TASK as
// `./python $(PROFILE_TASK)` with no HOSTRUNNER in front of it, so a cross build
// cannot train at all — "on" means the same thing as "native-only" here.
func (j *pyBuild) pgo() bool {
	if j.cross {
		return false
	}
	return j.res.PGO == "on" || j.res.PGO == "native-only"
}

// Only these reach the job key: an absolute prefix would make the cache
// machine-specific, and the dependency keys already cover what lives behind
// it.
func (j *pyBuild) decisionFlags() []string {
	flags := []string{"--disable-shared", "--with-ensurepip=no", "MODULE_BUILDTYPE=static"}
	if j.cross {
		flags = append(flags, "--build="+j.host.Triple, "--host="+j.target.Triple)
	} else {
		flags = append(flags, "--build="+j.target.Triple)
	}
	// Native too: it costs nothing (staticapi already needs pyhost) and it stops
	// Programs/_freeze_module from being linked at all. That link pulls in
	// $(MODOBJS) against a stubbed getpath_noop.o, so a static _testinternalcapi
	// breaks the bootstrap with an undefined _Py_Get_Getpath_CodeObject.
	flags = append(flags, "--with-build-python")
	// Lib/test ships with the _test* builtins or not at all: --disable-test-modules
	// governs both. It is what lets whoever has real riscv32 or s390x hardware
	// run the suite there, which is the one thing qemu cannot answer.
	if !j.res.TestModules {
		flags = append(flags, "--disable-test-modules")
	}
	if j.pgo() {
		flags = append(flags, "--enable-optimizations")
	}
	return flags
}

func (j *pyBuild) Build(ctx context.Context, e *core.Env, r *core.Runner, work, stage string) error {
	src := filepath.Join(work, "src")
	r.Step("copying the CPython tree")
	if err := copyTree(j.srctree.ArtifactDir(e), src); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(src, core.ManifestName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := installStaticAPI(j.staticapi.ArtifactDir(e), src); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(src, "Modules", "Setup.local"), j.setup, 0o644); err != nil {
		return err
	}

	sysroot := j.sysroot.ArtifactDir(e)
	prefix := j.ArtifactDir(e)
	te, err := newToolenv(e, j.target, j.res, prefix, []string{sysroot})
	if err != nil {
		return err
	}
	if j.tc, err = Toolchain(e, j.target.Triple); err != nil {
		return err
	}

	// The probe's config.site is fed to the native build too, so a native and a
	// cross build of the same triple answer the ABI questions identically
	// instead of one of them guessing differently.
	probeDir := j.probe.ArtifactDir(e)
	extra := map[string]string{"CONFIG_SITE": ConfigSite(probeDir)}
	if j.target.Libatomic {
		// On these targets the 64-bit _Py_atomic_* operations land in libatomic
		// rather than in the compiler's builtins, and CPython's own link line
		// does not ask for it.
		extra["LDFLAGS"] = strings.Join(append(te.ldflags(), "-latomic"), " ")
	}

	args := []string{"./configure", "--prefix=" + prefix, "--exec-prefix=" + prefix, "--with-openssl=" + sysroot}
	args = append(args, j.decisionFlags()...)
	// --with-build-python sets PYTHON_FOR_BUILD, PYTHON_FOR_FREEZE,
	// PYTHON_FOR_REGEN and FREEZE_MODULE_BOOTSTRAP as a consistent set. The old
	// Makefile instead faked config.status and sed'd the native build's Makefile,
	// which produced a _sysconfigdata describing the build machine rather than
	// the target.
	py, perr := pyhostInterpreter(j.buildPython.ArtifactDir(e))
	if perr != nil {
		return perr
	}
	args = replaceFlag(args, "--with-build-python", "--with-build-python="+py)
	if j.cross {
		if hr := hostRunner(e, j.target); hr != "" {
			args = append(args, "HOSTRUNNER="+hr)
		}
	}

	r.Step("configuring " + j.target.Triple)
	if err := r.Run(ctx, te.cmd(j.Name()+"-configure", src, args, extra)); err != nil {
		return err
	}

	if err := appendPyconfig(PyconfigPatches(probeDir, j.target), filepath.Join(src, "pyconfig.h")); err != nil {
		return err
	}

	r.Step("building " + j.target.Triple)
	makeArgs := []string{"make", "-j", strconv.Itoa(e.MakeJobs())}
	if j.pgo() {
		makeArgs = append(makeArgs, "PROFILE_TASK="+j.res.ProfileTask)
	}
	makeArgs = append(makeArgs, j.target.MakeVars...)
	if err := r.Run(ctx, te.cmd(j.Name()+"-make", src, makeArgs, extra)); err != nil {
		return err
	}
	return installPrefix(ctx, r, te, src, work, stage, prefix, extra)
}

// --prefix is the final artifact path rather than the stage, because it is
// baked into the interpreter: a binary configured against a staging directory
// would look for its stdlib somewhere that no longer exists once the artifact
// is published.
func installPrefix(ctx context.Context, r *core.Runner, te *toolenv, src, work, stage, prefix string, extra map[string]string) error {
	destdir := filepath.Join(work, "destdir")
	r.Step("installing")
	// bininstall is commoninstall plus the python3 symlink.
	args := []string{"make", "bininstall", "DESTDIR=" + destdir}
	if err := r.Run(ctx, te.cmd("make-install", src, args, extra)); err != nil {
		return err
	}
	if err := movePrefixInto(filepath.Join(destdir, prefix), stage); err != nil {
		return err
	}
	return trimInterpreter(stage)
}

// trimInterpreter drops what only a build of a third-party extension could use. A
// static interpreter cannot dlopen one, so libpython.a and the config-* makefile
// are tens of megabytes nobody on the target can spend.
func trimInterpreter(stage string) error {
	patterns := []string{
		filepath.Join(stage, "lib", "libpython*.a"),
		filepath.Join(stage, "lib", "python*", "config-*"),
	}
	for _, p := range patterns {
		matches, err := filepath.Glob(p)
		if err != nil {
			return err
		}
		for _, m := range matches {
			if err := os.RemoveAll(m); err != nil {
				return err
			}
		}
	}
	return nil
}

// replaceFlag swaps a placeholder flag for its resolved form, keeping the
// argument order the key was computed from.
func replaceFlag(args []string, want, with string) []string {
	for i, a := range args {
		if a == want {
			args[i] = with
			return args
		}
	}
	return append(args, with)
}

// hostRunner is what configure puts in front of a target binary it needs to
// execute. With --with-build-python nothing in `all` or `install` runs one, so
// it stays out of the job key: it is a safety net, not an input.
func hostRunner(e *core.Env, t config.Target) string {
	qemu := e.Qemu[t.Triple]
	if qemu == "" {
		return ""
	}
	if sysroot, err := ensure.Sysroot(e, t); err == nil {
		return qemu + " -L " + sysroot
	}
	return qemu
}

// makesetup resolves a relative source against $(srcdir)/Modules, so both
// files have to be in the source tree and not in a build directory.
func installStaticAPI(artifact, src string) error {
	dst := filepath.Join(src, "Modules", "staticapi")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"symbols.c", "symbols.h"} {
		from, err := findStaticAPIFile(artifact, name)
		if err != nil {
			return err
		}
		if err := copyFile(from, filepath.Join(dst, name), 0o644); err != nil {
			return err
		}
	}
	return assets.WriteTo(filepath.Join(src, "Modules"), "staticapi/staticapi.c")
}

// findStaticAPIFile tolerates both layouts gen publishes: symbols.c sits at the
// artifact root, symbols.h under staticapi/.
func findStaticAPIFile(artifact, name string) (string, error) {
	for _, p := range []string{
		filepath.Join(artifact, name),
		filepath.Join(artifact, "staticapi", name),
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("recipe: staticapi artifact %s holds no %s", artifact, name)
}

func pyhostInterpreter(dir string) (string, error) {
	for _, rel := range []string{"bin/python3", "bin/python"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	m, _ := filepath.Glob(filepath.Join(dir, "bin", "python3.*"))
	if len(m) > 0 {
		sort.Strings(m)
		return m[0], nil
	}
	return "", fmt.Errorf("recipe: pyhost artifact %s holds no interpreter", dir)
}

// A static interpreter cannot dlopen anything, so a bundled C module either
// arrives as a builtin or not at all.
func bundleModules(cfg *config.Config, bundle string) ([]config.PyModule, error) {
	if bundle == "" {
		return nil, nil
	}
	b, ok := cfg.Bundles[bundle]
	if !ok {
		return nil, fmt.Errorf("recipe: unknown bundle %q", bundle)
	}
	var out []config.PyModule
	for _, name := range b.Packages {
		pkg, ok := cfg.PyPackages[name]
		if !ok {
			return nil, fmt.Errorf("recipe: bundle %q names package %q, which no [pkg.*] table declares", bundle, name)
		}
		out = append(out, pkg.Modules...)
	}
	return out, nil
}

// Inline-asm availability and the atomics quirks are decisions rather than
// measurements, so configure cannot reach them and they arrive as a patch.
func appendPyconfig(from, header string) error {
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(header, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func movePrefixInto(from, stage string) error {
	ents, err := os.ReadDir(from)
	if err != nil {
		return fmt.Errorf("recipe: install produced nothing at %s: %w", from, err)
	}
	if len(ents) == 0 {
		return fmt.Errorf("recipe: install produced an empty prefix at %s", from)
	}
	for _, ent := range ents {
		if err := os.Rename(filepath.Join(from, ent.Name()), filepath.Join(stage, ent.Name())); err != nil {
			return err
		}
	}
	return nil
}

var (
	_ core.Job = (*pyHost)(nil)
	_ core.Job = (*pyBuild)(nil)
)
