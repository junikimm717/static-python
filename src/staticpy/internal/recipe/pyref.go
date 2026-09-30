package recipe

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/sources"
)

// The dynamic reference interpreter: stock CPython, built by this machine's gcc
// against shared copies of the same pinned dependency versions the static
// build uses, so version skew can never explain a gap between them.
//
// One job for every dependency plus CPython, because a shared library bakes in
// its --prefix (see rootfsMode). Changing one library rebuilds the whole
// baseline, but nothing depends on a measuring stick, so nothing else pays.
//
// After install every RUNPATH is rewritten $ORIGIN-relative so the rootfs can
// be copied; shrinking the .dynstr string is safe because $ORIGIN/../lib is
// shorter than dist/artifacts/pyref_.../rootfs/lib.
func PyRef(cfg *config.Config, assets fs.FS, target config.Target, profile string) (core.Job, error) {
	res, err := resolveScope(cfg, profile, config.ScopePython)
	if err != nil {
		return nil, err
	}
	if !res.HostBuilt() {
		return nil, fmt.Errorf("recipe: profile %q has toolchain %q; pyref exists to build with the machine's own compiler and libc, so it needs a host-built profile",
			profile, res.Toolchain)
	}
	tc, err := toolchainFor(nil, res, target.Triple)
	if err != nil {
		return nil, err
	}
	src, err := pythonSource(cfg)
	if err != nil {
		return nil, err
	}

	b := &depBuilder{cfg: cfg, assets: assets, target: target, profile: profile,
		memo: map[string]*depJob{}, onStack: map[string]bool{}}
	var order []*depJob
	seen := map[string]bool{}
	var add func(string) error
	add = func(name string) error {
		if seen[name] {
			return nil
		}
		skip, err := cfg.PackageSkipped(name, profile)
		if err != nil {
			return err
		}
		if skip {
			seen[name] = true
			return nil
		}
		d, err := b.job(name)
		if err != nil {
			return err
		}
		seen[name] = true
		// Needs first: a package links against what it needs, and here that
		// means the files have to already be in the shared rootfs.
		for _, n := range d.needs {
			if err := add(n.name); err != nil {
				return err
			}
		}
		order = append(order, d)
		return nil
	}
	for _, name := range sortedKeys(cfg.Packages) {
		if err := add(name); err != nil {
			return nil, err
		}
	}

	j := &pyRef{
		cfg: cfg, assets: assets, target: target, profile: profile,
		res: res, tc: tc, src: src, deps: order,
		version: src.Version,
		tree:    sources.SrcTree(src, sources.Options{Assets: assets}),
	}
	j.abi = pyABI(src.Version)
	return j, nil
}

// Unset means the recipe default (pass --with-lto). Static builds ignore
// this: their LTO is the flag lists.
func withLTO(res config.Resolved) bool {
	return res.EffectiveLTO()
}

// The major.minor CPython installs its binary and libdir under.
func pyABI(version string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return version
	}
	return parts[0] + "." + parts[1]
}

type pyRef struct {
	cfg     *config.Config
	assets  fs.FS
	target  config.Target
	profile string
	res     config.Resolved
	tc      ToolchainID
	src     config.Source
	tree    core.Job
	deps    []*depJob
	version string
	abi     string
}

func (j *pyRef) Name() string { return "pyref" }

func (j *pyRef) Slug() string { return "pyref:" + j.profile + ":" + j.target.Triple }

func (j *pyRef) Deps() []core.Job {
	out := []core.Job{j.tree}
	seen := map[string]bool{j.tree.Slug(): true}
	for _, d := range j.deps {
		if seen[d.tree.Slug()] {
			continue
		}
		seen[d.tree.Slug()] = true
		out = append(out, d.tree)
	}
	return out
}

func (j *pyRef) KeyInputs() map[string]string {
	in := map[string]string{
		"recipe_version": strconv.Itoa(Version),
		"python_version": j.src.Version,
		"python_sha256":  j.src.SHA256,
		"target":         j.target.Triple,
	}
	// Every dependency is built inside this job, so its inputs are this job's
	// inputs. Folding each one in keeps a configure-flag change on any of them
	// invalidating the interpreter, which is the whole reason a key exists.
	var names []string
	for _, d := range j.deps {
		names = append(names, d.name)
		for k, v := range d.KeyInputs() {
			in["dep."+d.name+"."+k] = v
		}
	}
	in["packages"] = strings.Join(names, "\x00")
	for k, v := range j.res.KeyInputs() {
		in["profile."+k] = v
	}
	for k, v := range j.tc.keyInputs() {
		in[k] = v
	}
	return in
}

func (j *pyRef) ArtifactDir(e *core.Env) string {
	return e.Path(core.DirArtifact, artifactName(j.Slug())+hostPublishSuffix(j.tc))
}

// Always non-empty: a host-built artifact is reproducible nowhere, and that has
// to be recorded rather than inferred from the profile name.
func (j *pyRef) Provenance() map[string]string { return j.tc.Provenance() }

func (j *pyRef) Build(ctx context.Context, e *core.Env, r *core.Runner, work, stage string) error {
	// The prefix everything is configured with is where the artifact will be
	// after publication, not where it is being built. The shadow is that same
	// absolute path rooted under work, so a .pc file naming the prefix resolves
	// to the staged copy once PKG_CONFIG_SYSROOT_DIR is set to the shadow root.
	art := j.ArtifactDir(e)
	if err := rejectForeignHostPrefix(art, j.tc.Key); err != nil {
		return err
	}
	rootfs := filepath.Join(art, "rootfs")
	shadowRoot := filepath.Join(work, "shadow")
	shadowRootfs := filepath.Join(shadowRoot, rootfs)
	if err := os.MkdirAll(shadowRootfs, 0o755); err != nil {
		return err
	}
	mode := &rootfsMode{prefix: rootfs, view: shadowRootfs, sysroot: shadowRoot}

	for _, d := range j.deps {
		d.roots = mode
		dwork := filepath.Join(work, "dep", d.name)
		dstage := filepath.Join(work, "depstage", d.name)
		for _, p := range []string{dwork, dstage} {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		}
		r.Step("dependency " + d.name)
		if err := d.Build(ctx, e, r, dwork, dstage); err != nil {
			return fmt.Errorf("recipe: pyref dependency %s: %w", d.name, err)
		}
		// Each shape leaves dstage holding the prefix's contents flat, so the
		// merge is what makes the next package see this one.
		if err := copyTree(dstage, shadowRootfs); err != nil {
			return fmt.Errorf("recipe: merging %s into the rootfs: %w", d.name, err)
		}
		if err := os.RemoveAll(dstage); err != nil {
			return err
		}
	}

	if err := j.cpython(ctx, e, r, work, shadowRoot, shadowRootfs, rootfs); err != nil {
		return err
	}

	r.Step("rewriting RUNPATH to $ORIGIN")
	if err := rewriteRootfsRpaths(shadowRootfs); err != nil {
		return err
	}

	// One move, after everything is installed: the artifact is published by a
	// single rename, so it is never observable half-built.
	dst := filepath.Join(stage, "rootfs")
	if err := os.Rename(shadowRootfs, dst); err != nil {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		if err := copyTree(shadowRootfs, dst); err != nil {
			return err
		}
	}
	if err := j.assertUsable(dst); err != nil {
		return err
	}
	return j.assertModules(ctx, r, dst)
}

func (j *pyRef) cpython(ctx context.Context, e *core.Env, r *core.Runner, work, shadowRoot, shadowRootfs, rootfs string) error {
	src := filepath.Join(work, "python")
	r.Step("copying the CPython tree")
	if err := copyTree(j.tree.ArtifactDir(e), src); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(src, core.ManifestName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := sources.ApplyTargetPatches(ctx, r, j.assets, j.src, j.target.Triple, src, work); err != nil {
		return err
	}
	if err := unstaticCtypes(src); err != nil {
		return err
	}

	te, err := newToolenv(e, j.target, j.res, rootfs, []string{shadowRootfs})
	if err != nil {
		return err
	}
	te.pcSysroot = shadowRoot

	args := []string{"./configure",
		"--prefix=" + rootfs,
		"--enable-shared",
		"--without-ensurepip",
		"--with-openssl=" + shadowRootfs,
		// The build finds OpenSSL in the shadow; the rpath below is what finds
		// it at run time. Letting configure add its own would bake the shadow.
		"--with-openssl-rpath=no",
		"--enable-loadable-sqlite-extensions",
		"--with-computed-gotos",
	}
	if j.res.PGO != "off" {
		args = append(args, "--enable-optimizations")
	}
	if withLTO(j.res) {
		args = append(args, "--with-lto")
	}
	// Same LIBS= path as pynative; see staticpy-traps for the glibc interposition.
	objs, oerr := sysrootObjects(shadowRootfs)
	if oerr != nil {
		return oerr
	}
	if len(objs) > 0 {
		args = append(args, "LIBS="+strings.Join(objs, " "))
	}

	// LDFLAGS finds the libraries now. LDFLAGS_NODIST rpaths the shadow
	// so a rebuild's -lreadline NEEDED walk does not prefer the previous
	// prefix (staticpy-traps PYREF_RPATH). rewriteRootfsRpaths then
	// shrinks it to $ORIGIN.
	extra := map[string]string{
		"LDFLAGS_NODIST": "-Wl,-rpath," + filepath.Join(shadowRootfs, "lib") + ":" + filepath.Join(shadowRootfs, "lib64"),
		// CPython imports every extension it builds, through the loader, not
		// -L. This covers any ELF that missed our rpath, and stops a miss from
		// succeeding against /lib64.
		"LD_LIBRARY_PATH": filepath.Join(shadowRootfs, "lib") + ":" + filepath.Join(shadowRootfs, "lib64"),
	}
	r.Step("configuring CPython")
	if err := r.Run(ctx, te.cmd("python-configure", src, args, extra)); err != nil {
		return err
	}
	if err := assertCPythonOptFlags(filepath.Join(src, "Makefile"), withLTO(j.res), j.res.PGO != "off"); err != nil {
		return err
	}

	r.Step("building CPython")
	mk := []string{"make", "-j" + strconv.Itoa(e.MakeJobs())}
	if task := j.res.ProfileTask; task != "" && j.res.PGO != "off" {
		mk = append(mk, "PROFILE_TASK="+task)
	}
	if err := r.Run(ctx, te.cmd("python-make", src, mk, extra)); err != nil {
		return err
	}

	r.Step("installing CPython")
	// DESTDIR is the shadow root, so CPython lands in the same rootfs the
	// dependencies were merged into and the whole thing moves as one tree.
	return r.Run(ctx, te.cmd("python-install", src,
		[]string{"make", "install", "DESTDIR=" + shadowRoot}, extra))
}

// CPython 3.13 decides LTO/PGO by matching *gcc* against $CC. CC=/usr/bin/cc
// leaves CONFIG_ARGS claiming --with-lto --enable-optimizations and the
// Makefile empty. See staticpy-traps.
func assertCPythonOptFlags(makefile string, wantLTO, wantPGO bool) error {
	raw, err := os.ReadFile(makefile)
	if err != nil {
		return fmt.Errorf("recipe: pyref: reading configure Makefile: %w", err)
	}
	vars := parseMakeAssignments(string(raw))
	if wantLTO {
		joined := vars["LTOFLAGS"] + " " + vars["CONFIGURE_CFLAGS_NODIST"] + " " + vars["CFLAGS_NODIST"]
		if !strings.Contains(joined, "-flto") {
			return fmt.Errorf("recipe: pyref passed --with-lto but configure put no -flto in the Makefile (LTOFLAGS=%q). CPython 3.13 matches *gcc* against $CC; /usr/bin/cc silently drops LTO. See staticpy-traps", vars["LTOFLAGS"])
		}
	}
	if wantPGO && strings.TrimSpace(vars["PGO_PROF_USE_FLAG"]) == "" {
		return fmt.Errorf("recipe: pyref passed --enable-optimizations but PGO_PROF_USE_FLAG is empty. CPython 3.13 matches *gcc* against CC_BASENAME; cc silently skips PGO. See staticpy-traps")
	}
	return nil
}

func parseMakeAssignments(text string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || strings.ContainsAny(key, " \t:+?") {
			continue
		}
		val = strings.TrimSpace(val)
		if i := strings.IndexByte(val, '#'); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		out[key] = val
	}
	return out
}

// An interpreter that cannot import the modules it was linked against is not a
// baseline, and finding that out during a benchmark run rather than here wastes
// the whole build.
func (j *pyRef) assertUsable(rootfs string) error {
	bin := filepath.Join(rootfs, "bin", "python"+j.abi)
	if fi, err := os.Stat(bin); err != nil || fi.Mode()&0o111 == 0 {
		return fmt.Errorf("recipe: pyref installed no executable at %s", bin)
	}
	lib := filepath.Join(rootfs, "lib")
	ents, err := os.ReadDir(lib)
	if err != nil {
		return fmt.Errorf("recipe: pyref installed no %s: %w", lib, err)
	}
	for _, ent := range ents {
		if strings.HasPrefix(ent.Name(), "libpython"+j.abi) {
			return nil
		}
	}
	return fmt.Errorf("recipe: pyref installed no libpython%s in %s, so --enable-shared did not take", j.abi, lib)
}

// Each exists only because a dependency was built into the rootfs, and CPython
// does not fail when configure cannot link one -- it reports "necessary bits
// not found" and carries on. The .py wrappers are listed too: _ctypes can
// import cleanly while ctypes does not, and checking only the extension let
// that reach a published artifact once.
var modulesFromDeps = []string{
	"_ssl", "_hashlib", "_sqlite3", "_lzma", "_bz2", "_zstd", "zlib",
	"_ctypes", "_curses", "_uuid", "readline",
	"ssl", "sqlite3", "lzma", "bz2", "compression.zstd", "ctypes", "curses", "uuid", "decimal", "hashlib",
}

func (j *pyRef) assertModules(ctx context.Context, r *core.Runner, rootfs string) error {
	bin := filepath.Join(rootfs, "bin", "python"+j.abi)
	var probe strings.Builder
	probe.WriteString("import sys\nmissing=[]\n")
	for _, m := range modulesFromDeps {
		fmt.Fprintf(&probe, "try:\n import %s\nexcept Exception: missing.append(%q)\n", m, m)
	}
	probe.WriteString("print(','.join(missing))\nsys.exit(1 if missing else 0)\n")
	r.Step("checking the extension modules")
	out, err := r.Output(ctx, core.Cmd{
		Dir:  rootfs,
		Args: []string{bin, "-c", probe.String()},
		Name: "module-probe",
		// Cleared rather than inherited: a PYTHONPATH from the caller's shell
		// could satisfy an import the interpreter itself cannot.
		//
		// No LD_LIBRARY_PATH: RUNPATH is $ORIGIN-relative after the rewrite,
		// so a missing rewrite would load the host's libssl and look green.
		EnvAdd: map[string]string{
			"PYTHONNOUSERSITE": "1", "PYTHONHOME": "", "PYTHONPATH": "",
			"LD_LIBRARY_PATH": "",
		},
	})
	if err == nil {
		return nil
	}
	if missing := strings.TrimSpace(out); missing != "" {
		return fmt.Errorf("recipe: pyref built without %s. Every one of these comes from a dependency in the rootfs, "+
			"so configure could not link it: check the *python-configure* log for the failing test", missing)
	}
	return fmt.Errorf("recipe: pyref's module probe could not run at all: %w", err)
}

// Undoes the two ctypes edits sources.toml applies to the shared srctree for
// the static build (pythonapi rebound onto staticapi, dlopen stubbed). Here
// stock ctypes is the thing being measured, and the rebinding breaks `import
// ctypes` outright since no staticapi module is built. Both replacements must
// match exactly once, so an upstream move fails loudly.
func unstaticCtypes(src string) error {
	path := filepath.Join(src, "Lib", "ctypes", "__init__.py")
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("recipe: pyref: %w", err)
	}
	text := string(b)
	for _, fix := range []struct{ what, from, to string }{
		{
			what: "the staticapi rebinding",
			from: "# This code is meant to be injected into ctypes/__init__.py\n\nimport staticapi\npythonapi = StaticCDLL(staticapi)\n",
			to:   "",
		},
		{
			what: "the stubbed dlopen",
			from: "    _dlopen = lambda *a, **kw: 0",
			to:   "    from _ctypes import dlopen as _dlopen",
		},
	} {
		if n := strings.Count(text, fix.from); n != 1 {
			return fmt.Errorf("recipe: pyref: %s appears %d times in Lib/ctypes/__init__.py, want exactly 1; "+
				"the edits in sources.toml have moved and this build would ship the static interpreter's ctypes", fix.what, n)
		}
		text = strings.Replace(text, fix.from, fix.to, 1)
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func rejectForeignHostPrefix(dir, wantKey string) error {
	m, err := core.ReadManifest(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if hasRootfsLib(dir) {
				return fmt.Errorf("recipe: %s has a rootfs and no manifest; refusing to reuse a prefix that may belong to another host compiler", dir)
			}
			return nil
		}
		return fmt.Errorf("recipe: %s: %w", dir, err)
	}
	have := manifestToolchain(m)
	if have == "" || have != wantKey {
		return fmt.Errorf("recipe: %s belongs to host toolchain %s; this job is %s. Different host compilers must not share a prefix",
			dir, shortKey(have), shortKey(wantKey))
	}
	return nil
}

func manifestToolchain(m *core.Manifest) string {
	if m.Inputs != nil {
		if v := m.Inputs["toolchain"]; v != "" {
			return v
		}
	}
	if m.Provenance != nil {
		if v := m.Provenance["toolchain"]; v != "" {
			return v
		}
	}
	return ""
}

func hasRootfsLib(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "rootfs", "lib"))
	return err == nil && st.IsDir()
}

func shortKey(k string) string {
	if k == "" {
		return "unknown"
	}
	if len(k) > 12 {
		return k[:12]
	}
	return k
}
