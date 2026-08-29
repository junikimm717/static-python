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

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/sources"
)

// The recipe shapes a package can declare. Anything else is a typo in
// packages.toml, and is rejected rather than silently treated as autotools.
const (
	buildAutotools = "autotools"
	buildOpenSSL   = "openssl"
	buildMake      = "make"
	buildSources   = "sources"
)

// Each dependency is built into its own prefix, never a shared accumulator:
// that is what let a stale libz.a from an older version survive a version
// bump and link into everything afterwards.
func Dep(cfg *config.Config, assets fs.FS, t config.Target, profile, name string) (core.Job, error) {
	b := &depBuilder{cfg: cfg, assets: assets, target: t, profile: profile,
		memo: map[string]*depJob{}, onStack: map[string]bool{}}
	return b.job(name)
}

func Deps(cfg *config.Config, assets fs.FS, t config.Target, profile string) ([]core.Job, error) {
	b := &depBuilder{cfg: cfg, assets: assets, target: t, profile: profile,
		memo: map[string]*depJob{}, onStack: map[string]bool{}}
	var out []core.Job
	for _, name := range sortedKeys(cfg.Packages) {
		j, err := b.job(name)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, nil
}

type depBuilder struct {
	cfg     *config.Config
	assets  fs.FS
	target  config.Target
	profile string
	memo    map[string]*depJob
	onStack map[string]bool
	path    []string
}

func (b *depBuilder) job(name string) (*depJob, error) {
	if j, ok := b.memo[name]; ok {
		return j, nil
	}
	if b.onStack[name] {
		return nil, fmt.Errorf("recipe: needs cycle in packages.toml: %s", strings.Join(append(b.path, name), " -> "))
	}
	// PackageFor, not Packages[name]: a profile may override this package's
	// configure line or its postcondition, and the override has to be in place
	// before anything derives a key from either.
	pkg, err := b.cfg.PackageFor(name, b.profile)
	if err != nil {
		return nil, fmt.Errorf("recipe: %w", err)
	}
	srcName := pkg.Source
	if srcName == "" {
		srcName = pkg.Name
	}
	if srcName == "" {
		srcName = name
	}
	src, ok := b.cfg.Sources[srcName]
	if !ok {
		return nil, fmt.Errorf("recipe: package %s names source %q, which is not in sources.toml", name, srcName)
	}
	res, err := resolveScope(b.cfg, b.profile, depScope(name))
	if err != nil {
		return nil, err
	}
	id, err := toolchainFor(nil, res, b.target.Triple)
	if err != nil {
		return nil, err
	}
	j := &depJob{
		name: name, pkg: pkg, src: src, target: b.target, profile: b.profile,
		res: res, tc: id, assets: b.assets,
		tree: sources.SrcTree(src, sources.Options{Assets: b.assets}),
	}
	if j.patchHash, err = sources.PatchSetHash(b.assets, src); err != nil {
		return nil, err
	}
	if j.tgtPatchHash, err = sources.TargetPatchSetHash(b.assets, src, b.target.Triple); err != nil {
		return nil, err
	}
	if pkg.PlatformMap != "" {
		j.platform, ok = b.target.Maps[pkg.PlatformMap]
		if !ok || j.platform == "" {
			return nil, fmt.Errorf("recipe: %s needs maps.%s for target %s, which targets.toml does not set; "+
				"these platform names do not follow from the triple and have to be written down",
				name, pkg.PlatformMap, b.target.Triple)
		}
	}

	b.onStack[name] = true
	b.path = append(b.path, name)
	for _, need := range pkg.Needs {
		n, err := b.job(need)
		if err != nil {
			return nil, err
		}
		j.needs = append(j.needs, n)
	}
	b.onStack[name] = false
	b.path = b.path[:len(b.path)-1]

	b.memo[name] = j
	return j, nil
}

type depJob struct {
	name         string
	pkg          config.Package
	src          config.Source
	target       config.Target
	profile      string
	res          config.Resolved
	tc           ToolchainID
	assets       fs.FS
	tree         core.Job
	needs        []*depJob
	patchHash    string
	tgtPatchHash string
	platform     string
}

func (j *depJob) Name() string { return "dep" }

func (j *depJob) Slug() string {
	return "dep:" + j.profile + ":" + j.target.Triple + ":" + j.name
}

func (j *depJob) Deps() []core.Job {
	out := []core.Job{j.tree}
	for _, n := range j.needs {
		out = append(out, n)
	}
	return out
}

func (j *depJob) shape() string {
	if j.pkg.Build == "" {
		return buildAutotools
	}
	return j.pkg.Build
}

func (j *depJob) KeyInputs() map[string]string {
	var needs []string
	for _, n := range j.needs {
		needs = append(needs, n.name)
	}
	in := map[string]string{
		"recipe_version": strconv.Itoa(Version),
		"package":        j.name,
		"build":          j.shape(),
		"source":         j.src.Name,
		"source_version": j.src.Version,
		"source_sha256":  j.src.SHA256,
		"patches":        j.patchHash,
		"target":         j.target.Triple,
		"configure":      strings.Join(j.pkg.Configure, "\x00"),
		"needs":          strings.Join(needs, "\x00"),
		// The postcondition is part of the recipe: tightening it has to make
		// the artifact that never satisfied it invalid.
		"provides": strings.Join(j.pkg.Provides, "\x00"),
	}
	if j.tgtPatchHash != "none" {
		in["target_patches"] = j.tgtPatchHash
	}
	if j.platform != "" {
		in["platform"] = j.platform
	}
	if j.shape() == buildSources {
		in["sources"] = strings.Join(j.pkg.Sources, "\x00")
		in["cflags"] = strings.Join(j.pkg.CFlags, "\x00")
		in["headers"] = strings.Join(j.pkg.Headers, "\x00")
		in["libname"] = j.pkg.Libname
		in["object"] = j.pkg.Object
		in["keep_globals"] = strings.Join(j.pkg.KeepGlobals, "\x00")
	}
	for k, v := range j.res.KeyInputs() {
		in["profile."+k] = v
	}
	for k, v := range j.tc.keyInputs() {
		in[k] = v
	}
	return in
}

func (j *depJob) ArtifactDir(e *core.Env) string {
	return e.Path(core.DirArtifact, artifactName(j.Slug()))
}

func (j *depJob) Provenance() map[string]string { return j.tc.Provenance() }

// view is every prefix this package compiles and links against: its direct
// needs and theirs, deepest last so a direct need's headers win.
func (j *depJob) view(e *core.Env) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(*depJob)
	walk = func(d *depJob) {
		for _, n := range d.needs {
			if seen[n.name] {
				continue
			}
			seen[n.name] = true
			out = append(out, n.ArtifactDir(e))
			walk(n)
		}
	}
	walk(j)
	return out
}

func (j *depJob) Build(ctx context.Context, e *core.Env, r *core.Runner, work, stage string) error {
	// The install prefix is the final artifact directory, not the staging
	// directory: openssl and libtool bake their prefix into what they install,
	// and a pid-tagged staging path is gone by the time anyone reads it.
	prefix := j.ArtifactDir(e)
	te, err := newToolenv(e, j.target, j.res, prefix, j.view(e))
	if err != nil {
		return err
	}

	src := filepath.Join(work, "src")
	r.Step("staging " + sources.Slug(j.src))
	if err := copyTree(j.tree.ArtifactDir(e), src); err != nil {
		return fmt.Errorf("recipe: staging %s: %w", sources.Slug(j.src), err)
	}
	if err := os.Remove(filepath.Join(src, core.ManifestName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := sources.ApplyTargetPatches(ctx, r, j.assets, j.src, j.target.Triple, src, work); err != nil {
		return err
	}

	switch j.shape() {
	case buildAutotools:
		err = j.autotools(ctx, e, r, te, src, stage, prefix)
	case buildOpenSSL:
		err = j.openssl(ctx, e, r, te, src, stage, prefix)
	case buildMake:
		err = j.plainMake(ctx, e, r, te, src, stage)
	case buildSources:
		err = j.fromSources(ctx, r, te, src, stage)
	default:
		err = fmt.Errorf("recipe: package %s declares build = %q; valid shapes are %q, %q, %q and %q",
			j.name, j.pkg.Build, buildAutotools, buildOpenSSL, buildMake, buildSources)
	}
	if err != nil {
		return err
	}
	return j.assertProvides(stage)
}

func (j *depJob) makeJobs(e *core.Env) string { return "-j" + strconv.Itoa(e.MakeJobs()) }

// A package that builds a helper and then runs it needs that helper to run
// *here*, not on the target. @BUILD_CC@ is the native toolchain with -static,
// so the result executes whatever libc the build machine has -- ours is
// musl-targeting and the host may well be glibc.
func (j *depJob) makeVars(e *core.Env) []string {
	if len(j.pkg.MakeVars) == 0 {
		return nil
	}
	cc := "cc"
	if tc, err := ToolchainNative(e, e.Host); err == nil {
		cc = tc.CC + " -static"
	}
	out := make([]string, 0, len(j.pkg.MakeVars))
	for _, v := range j.pkg.MakeVars {
		out = append(out, strings.ReplaceAll(v, "@BUILD_CC@", cc))
	}
	return out
}

func (j *depJob) autotools(ctx context.Context, e *core.Env, r *core.Runner, te *toolenv, src, stage, prefix string) error {
	args := []string{"./configure",
		"--prefix=" + prefix,
		"--exec-prefix=" + prefix,
	}
	// --host is what puts autotools into cross mode, where it may not run a test
	// program and falls back to guessing. It would also send configure looking
	// for a triple-prefixed gcc that does not exist, since a host compiler
	// answers to its distro's spelling (x86_64-redhat-linux) rather than ours.
	if !j.res.HostBuilt() {
		args = append(args, "--host="+j.target.Triple)
	}
	args = append(args, j.pkg.Configure...)

	r.Step("configuring " + j.name)
	if err := r.Run(ctx, te.cmd(j.name+"-configure", src, args, nil)); err != nil {
		return err
	}
	r.Step("building " + j.name)
	mk := append([]string{"make", j.makeJobs(e)}, j.makeVars(e)...)
	if err := r.Run(ctx, te.cmd(j.name+"-make", src, mk, nil)); err != nil {
		return err
	}
	r.Step("installing " + j.name)
	// DESTDIR goes on the command line, never the environment: a makefile
	// assignment beats an environment variable in make's precedence, and
	// ncurses ships `DESTDIR=@DESTDIR@`, so an exported one is silently
	// discarded and the install writes straight to the absolute prefix.
	if err := r.Run(ctx, te.cmd(j.name+"-install", src,
		[]string{"make", "install", "DESTDIR=" + stage}, map[string]string{
			// ncurses' install compiles the terminfo database with tic, which is a
			// target binary we cannot run when cross-compiling. The database is
			// not what we ship — libncursesw.a is — so install must not gate on it.
			"TIC_PATH": "true",
		})); err != nil {
		return err
	}
	return hoistDestdir(stage, prefix, j.name)
}

func (j *depJob) openssl(ctx context.Context, e *core.Env, r *core.Runner, te *toolenv, src, stage, prefix string) error {
	args := []string{"./Configure", j.platform}
	args = append(args, j.pkg.Configure...)
	args = append(args, "--prefix="+prefix, "--openssldir="+prefix)

	r.Step("configuring " + j.name)
	if err := r.Run(ctx, te.cmd(j.name+"-configure", src, args, nil)); err != nil {
		return err
	}
	r.Step("building " + j.name)
	mk := append([]string{"make", j.makeJobs(e)}, j.makeVars(e)...)
	if err := r.Run(ctx, te.cmd(j.name+"-make", src, mk, nil)); err != nil {
		return err
	}
	// install_sw, not install: the docs target needs perl's pod2man and
	// installs nothing a linker or a compiler will ever look at.
	r.Step("installing " + j.name)
	if err := r.Run(ctx, te.cmd(j.name+"-install", src,
		[]string{"make", "install_sw", "DESTDIR=" + stage}, nil)); err != nil {
		return err
	}
	return hoistDestdir(stage, prefix, j.name)
}

// The Makefile is assumed to honour CC/AR/CFLAGS from the environment (bzip2
// only does because sources.toml deletes the assignments that would
// otherwise win).
func (j *depJob) plainMake(ctx context.Context, e *core.Env, r *core.Runner, te *toolenv, src, stage string) error {
	// A hand-written Makefile's default target usually runs a self-test, which
	// executes the binaries we just cross-compiled. Build exactly the archives
	// the package promises instead.
	args := []string{"make", j.makeJobs(e)}
	for _, p := range j.pkg.Provides {
		if strings.HasSuffix(p, ".a") {
			args = append(args, path.Base(p))
		}
	}
	r.Step("building " + j.name)
	if err := r.Run(ctx, te.cmd(j.name+"-make", src, args, nil)); err != nil {
		return err
	}
	// No DESTDIR: a Makefile this old has never heard of it, so the staging
	// directory has to be the prefix it installs into.
	r.Step("installing " + j.name)
	return r.Run(ctx, te.cmd(j.name+"-install", src, []string{"make", "install"},
		map[string]string{"PREFIX": stage}))
}

// libuuid is built by compiling a declared file list and archiving it: the
// real util-linux build needs meson, ninja, flex and bison on the host to
// produce one small library CPython uses for three symbols, and the
// cross.ini / -Dfeature=disabled dance on top of that.
func (j *depJob) fromSources(ctx context.Context, r *core.Runner, te *toolenv, src, stage string) error {
	if j.pkg.Libname == "" {
		return fmt.Errorf("recipe: package %s has build = %q but no libname, so there is no archive to write",
			j.name, buildSources)
	}
	if len(j.pkg.Sources) == 0 {
		return fmt.Errorf("recipe: package %s has build = %q but lists no sources", j.name, buildSources)
	}

	// One cc -c over the whole list drops the objects next to their sources,
	// named after them, so two files with the same basename would silently
	// overwrite each other's object.
	objs := make([]string, 0, len(j.pkg.Sources))
	byBase := map[string]string{}
	for _, s := range j.pkg.Sources {
		base := strings.TrimSuffix(path.Base(s), path.Ext(s)) + ".o"
		if prev, ok := byBase[base]; ok {
			return fmt.Errorf("recipe: package %s lists %s and %s, which compile to the same object %s",
				j.name, prev, s, base)
		}
		byBase[base] = s
		objs = append(objs, base)
	}

	args := []string{te.tools.CC, "-c"}
	args = append(args, te.cflags()...)
	args = append(args, j.pkg.CFlags...)
	args = append(args, j.pkg.Sources...)
	r.Step("compiling " + j.name)
	if err := r.Run(ctx, te.cmd(j.name+"-cc", src, args, nil)); err != nil {
		return err
	}

	lib := filepath.Join(stage, "lib", "lib"+j.pkg.Libname+".a")
	if err := os.MkdirAll(filepath.Dir(lib), 0o755); err != nil {
		return err
	}
	r.Step("archiving " + j.name)
	if err := r.Run(ctx, te.cmd(j.name+"-ar", src, append([]string{te.tools.AR, "rcs", lib}, objs...), nil)); err != nil {
		return err
	}

	if j.pkg.Object != "" {
		if err := j.mergeObject(ctx, r, te, src, stage, objs); err != nil {
			return err
		}
	}

	// Headers land under include/<libname>/ so `#include <uuid.h>` resolves
	// through the -I<prefix>/include/uuid the flags add, the way util-linux's
	// own install lays them out.
	for _, h := range j.pkg.Headers {
		dst := filepath.Join(stage, "include", j.pkg.Libname, path.Base(h))
		if err := copyFile(filepath.Join(src, filepath.FromSlash(h)), dst, 0o644); err != nil {
			return fmt.Errorf("recipe: installing header %s for %s: %w", h, j.name, err)
		}
	}
	return nil
}

// hoistDestdir lifts <stage><prefix> to <stage>, so the artifact is the
// installed tree itself rather than a deep path mirroring the store.
func hoistDestdir(stage, prefix, pkg string) error {
	staged := filepath.Join(stage, prefix)
	ents, err := os.ReadDir(staged)
	if os.IsNotExist(err) {
		return fmt.Errorf("recipe: %s installed nothing under %s: its install target ignored DESTDIR, "+
			"so it wrote outside the artifact it was supposed to fill", pkg, staged)
	}
	if err != nil {
		return err
	}
	for _, ent := range ents {
		if err := os.Rename(filepath.Join(staged, ent.Name()), filepath.Join(stage, ent.Name())); err != nil {
			return fmt.Errorf("recipe: %s: hoisting %s out of the DESTDIR path: %w", pkg, ent.Name(), err)
		}
	}
	for dir := staged; dir != stage && strings.HasPrefix(dir, stage+string(os.PathSeparator)); dir = filepath.Dir(dir) {
		if err := os.Remove(dir); err != nil {
			left, _ := os.ReadDir(dir)
			var names []string
			for _, l := range left {
				names = append(names, l.Name())
			}
			return fmt.Errorf("recipe: %s installed outside --prefix: %s still holds %s. "+
				"Those files would be dropped, so fix the package's configure arguments rather than shipping without them",
				pkg, dir, strings.Join(names, ", "))
		}
	}
	return nil
}

// assertProvides is the postcondition that catches the failure mode a green
// configure and a green make cannot: an install that produced no library.
func (j *depJob) assertProvides(stage string) error {
	for _, p := range j.pkg.Provides {
		clean := filepath.Clean(filepath.FromSlash(p))
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return fmt.Errorf("recipe: package %s declares provides %q; it must be a path inside the prefix", j.name, p)
		}
		if _, err := os.Lstat(filepath.Join(stage, clean)); err != nil {
			return fmt.Errorf("recipe: %s built and installed without producing %s. Installed instead: %s",
				j.name, p, strings.Join(installedSummary(stage), ", "))
		}
	}
	return nil
}

// installedSummary lists what the install actually produced, so a missing
// library is diagnosable from the error alone (wrong libdir, wrong name).
func installedSummary(stage string) []string {
	var out []string
	for _, dir := range []string{"lib", "lib64", "include"} {
		ents, err := os.ReadDir(filepath.Join(stage, dir))
		if err != nil {
			continue
		}
		for _, ent := range ents {
			out = append(out, path.Join(dir, ent.Name()))
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return []string{"(nothing)"}
	}
	if len(out) > 40 {
		out = append(out[:40], "...")
	}
	return out
}

// mergeObject collapses a package's objects into one relocatable object and
// localises everything except KeepGlobals.
//
// Both halves matter for an allocator. A single object is what makes the
// override unconditional: the linker pulls an archive member only to resolve
// an undefined symbol, and musl's malloc is a weak definition inside libc.a
// that already satisfies every reference. Localising the rest keeps the
// allocator's internals from colliding with the copy CPython bundles in
// obmalloc.o.
func (j *depJob) mergeObject(ctx context.Context, r *core.Runner, te *toolenv, src, stage string, objs []string) error {
	if len(j.pkg.KeepGlobals) == 0 {
		return fmt.Errorf("recipe: package %s sets object but no keep_globals; every symbol would stay global and collide", j.name)
	}
	dst := filepath.Join(stage, filepath.FromSlash(j.pkg.Object))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	merged := "staticpy-merged.o"
	r.Step("merging " + j.name)
	if err := r.Run(ctx, te.cmd(j.name+"-ld-r", src,
		append([]string{te.tools.LD, "-r", "-o", merged}, objs...), nil)); err != nil {
		return err
	}

	// A symbol file rather than a flag per name: the list is long enough that
	// the argv would be the least readable thing in the log.
	symFile := "staticpy-keep-globals.txt"
	if err := os.WriteFile(filepath.Join(src, symFile),
		[]byte(strings.Join(j.pkg.KeepGlobals, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	r.Step("localising " + j.name)
	return r.Run(ctx, te.cmd(j.name+"-objcopy", src, []string{
		te.tools.Objcopy, "--keep-global-symbols=" + symFile, merged, dst,
	}, nil))
}
