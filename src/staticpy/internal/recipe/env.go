package recipe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/hostcc"
)

// Keeping the spelling in one place means a scope rename cannot half-apply.
func depScope(pkg string) string { return config.ScopeDeps + "." + pkg }

// core hands KeyInputs no Env and recipe.Plan takes none, but a key that left
// the compiler out would survive a toolchain re-publish and keep serving
// artifacts built by a compiler nobody can name any more. The CLI binds the Env
// once before it plans; jobs then resolve their toolchain identity while they
// are being constructed.
var bound atomic.Pointer[core.Env]

func Bind(e *core.Env) { bound.Store(e) }

// resolveScope is the only call the recipe package makes into profile
// resolution, so the whole package moves together if that API changes.
func resolveScope(cfg *config.Config, profile, scope string) (config.Resolved, error) {
	return cfg.Resolve(profile, scope)
}

// Slugs carry ':' to stay readable on the CLI; paths get '_', matching what
// core does for locks.
func artifactName(slug string) string {
	out := []rune(slug)
	for i, r := range out {
		if r == ':' || r == '/' {
			out[i] = '_'
		}
	}
	return string(out)
}

// The two ways a toolchain's identity can be established. A gccfactory
// manifest is a key someone else computed and published; a probe is this
// machine looking at a compiler and taking its word for it.
const (
	toolchainVerified = "gccfactory"
	toolchainProbed   = "probe"
	// Neither: no tree to inspect and no manifest to read, only the machine's
	// own compiler and headers. Always recorded in provenance, because nothing
	// built this way is reproducible elsewhere.
	toolchainHost = "host"

	gccfactoryManifest = ".gccfactory.json"
)

// Not one of core's provisioned kinds, and the only kind with no directory.
const KindHost = "host"

// ToolchainID is what a job folds into its key to stand for "the compiler that
// produced this". Every recipe uses it, so re-publishing a toolchain
// invalidates the whole tree below it rather than leaving artifacts that were
// built by a compiler nobody can name any more.
type ToolchainID struct {
	Triple string
	Kind   string
	Dir    string
	CC     string
	// Key is gccfactory's Merkle key, or a digest of the probe below.
	Key    string
	Source string
	// Probe is the human-readable form of a probed identity; empty when the
	// toolchain carried a manifest.
	Probe string
}

func (id ToolchainID) keyInputs() map[string]string {
	return map[string]string{
		"toolchain":        id.Key,
		"toolchain_source": id.Source,
	}
}

// Provenance is empty for a toolchain that identified itself, and non-empty
// for one we had to fingerprint. core stamps it into the manifest, so a build
// assembled on trust never looks identical to one that was verified.
func (id ToolchainID) Provenance() map[string]string {
	if id.Source == toolchainVerified {
		return nil
	}
	return map[string]string{
		"toolchain":        id.Triple + "-" + id.Kind,
		"toolchain_source": id.Source,
		"toolchain_key":    id.Key,
		"toolchain_dir":    id.Dir,
		"toolchain_probe":  id.Probe,
	}
}

var toolchainCache sync.Map // toolchain dir -> ToolchainID

// Cross is preferred over native exactly as core.Env.PathFor prefers it, so
// the CC a job names and the PATH the Runner composes can never come from two
// different trees.
func Toolchain(e *core.Env, triple string) (ToolchainID, error) {
	return toolchainOf(e, triple, core.KindCross, core.KindNative)
}

// pyhost has to *run* on the build machine, so a cross tree carrying the same
// triple is the wrong compiler even though the name matches; picking it would
// produce a build-python that cannot execute, and --with-build-python's
// version check is where that surfaces.
func ToolchainNative(e *core.Env, triple string) (ToolchainID, error) {
	return toolchainOf(e, triple, core.KindNative)
}

func toolchainOf(e *core.Env, triple string, kinds ...string) (ToolchainID, error) {
	if e == nil {
		e = bound.Load()
	}
	if e == nil {
		return ToolchainID{}, fmt.Errorf("recipe: no build environment, so the toolchain for %s cannot be resolved; "+
			"call recipe.Bind(env) before planning", triple)
	}
	var dir, kind string
	var err error
	for _, k := range kinds {
		if dir, err = e.ToolchainDir(triple, k); err == nil {
			kind = k
			break
		}
	}
	if kind == "" {
		return ToolchainID{}, err
	}
	if v, ok := toolchainCache.Load(dir); ok {
		return v.(ToolchainID), nil
	}
	id := ToolchainID{Triple: triple, Kind: kind, Dir: dir}
	tools, err := toolsFor(dir, triple)
	if err != nil {
		return ToolchainID{}, err
	}
	id.CC = tools.CC

	key, err := gccfactoryKey(dir)
	switch {
	case err != nil:
		return ToolchainID{}, err
	case key != "":
		id.Key, id.Source = key, toolchainVerified
	default:
		if err := probeToolchain(&id); err != nil {
			return ToolchainID{}, err
		}
	}
	toolchainCache.Store(dir, id)
	return id, nil
}

// gccfactoryKey returns the published Merkle key, or "" when the tree carries
// no manifest at all. A manifest that exists but names no key is an error: it
// means the publisher changed shape, and guessing would key every artifact on
// the empty string.
func gccfactoryKey(dir string) (string, error) {
	path := filepath.Join(dir, gccfactoryManifest)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("recipe: reading %s: %w", path, err)
	}
	var m struct {
		Key       string `json:"key"`
		MerkleKey string `json:"merkle_key"`
		Merkle    string `json:"merkle"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return "", fmt.Errorf("recipe: parsing %s: %w", path, err)
	}
	for _, k := range []string{m.Key, m.MerkleKey, m.Merkle} {
		if k != "" {
			return k, nil
		}
	}
	return "", fmt.Errorf("recipe: %s has no key, merkle_key or merkle field; "+
		"re-publish the toolchain from gccfactory, or delete the file to fall back to fingerprinting the driver", path)
}

// probeToolchain fingerprints a compiler that did not come from gccfactory —
// musl.cc, or something hand-built. It is weaker than a published key (the
// driver can call a different cc1 tomorrow), which is why it is reported as
// provenance rather than treated as equivalent.
func probeToolchain(id *ToolchainID) error {
	version, err := ccOutput(id.CC, "-dumpversion")
	if err != nil {
		return err
	}
	machine, err := ccOutput(id.CC, "-dumpmachine")
	if err != nil {
		return err
	}
	sum, err := sha256File(id.CC)
	if err != nil {
		return fmt.Errorf("recipe: hashing %s: %w", id.CC, err)
	}
	id.Probe = fmt.Sprintf("gcc %s targeting %s, driver sha256 %s", version, machine, sum[:16])
	id.Source = toolchainProbed
	h := sha256.Sum256([]byte(strings.Join([]string{toolchainProbed, version, machine, sum}, "\x00")))
	id.Key = hex.EncodeToString(h[:])
	return nil
}

func ccOutput(cc string, arg string) (string, error) {
	cmd := exec.Command(cc, arg)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("recipe: %s %s failed: %w; the toolchain at %s does not look usable",
			cc, arg, err, filepath.Dir(filepath.Dir(cc)))
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return "", fmt.Errorf("recipe: %s %s printed nothing", cc, arg)
	}
	return text, nil
}

// tools are the binaries a build drives directly. Everything else it finds on
// the PATH the Runner composes.
type tools struct {
	CC      string
	CXX     string
	AR      string
	RANLIB  string
	NM      string
	LD      string
	Objcopy string
}

func toolsFor(dir, triple string) (tools, error) {
	var t tools
	var err error
	if t.CC, err = pickTool(dir, triple, "gcc", "cc"); err != nil {
		return t, err
	}
	// gcc-ar/gcc-ranlib load GCC's LTO plugin, so an archive of slim LTO
	// objects keeps a usable symbol index. Plain ar indexes them as empty and
	// the link fails hundreds of lines later on undefined symbols.
	if t.AR, err = pickTool(dir, triple, "gcc-ar", "ar"); err != nil {
		return t, err
	}
	if t.RANLIB, err = pickTool(dir, triple, "gcc-ranlib", "ranlib"); err != nil {
		return t, err
	}
	if t.NM, err = pickTool(dir, triple, "gcc-nm", "nm"); err != nil {
		return t, err
	}
	if t.Objcopy, err = pickTool(dir, triple, "objcopy"); err != nil {
		return t, err
	}
	if t.LD, err = pickTool(dir, triple, "ld"); err != nil {
		return t, err
	}
	t.CXX, _ = pickTool(dir, triple, "g++", "c++") // absent on a C-only tree
	return t, nil
}

// pickTool prefers the triple-prefixed name so a toolchain that also carries
// unprefixed aliases cannot resolve to the host's tool of the same name.
func pickTool(dir, triple string, names ...string) (string, error) {
	var tried []string
	for _, n := range names {
		for _, cand := range []string{triple + "-" + n, n} {
			p := filepath.Join(dir, "bin", cand)
			tried = append(tried, p)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("recipe: no %s in the %s toolchain; looked for %s",
		names[0], triple, strings.Join(tried, ", "))
}

// toolenv is the compiler environment one job hands to every command it runs,
// driven from the resolved profile and the composed dependency view rather than
// from hard-coded paths.
type toolenv struct {
	target config.Target
	tools  tools
	res    config.Resolved
	// dir is the toolchain root, prefix is where this job installs, and view
	// lists the prefixes that supply headers and libraries.
	dir    string
	prefix string
	view   []string
}

func newToolenv(e *core.Env, t config.Target, res config.Resolved, prefix string, view []string) (*toolenv, error) {
	if res.HostBuilt() {
		id, err := ToolchainHost(context.Background())
		if err != nil {
			return nil, err
		}
		tl, err := hostToolsFor(id.CC)
		if err != nil {
			return nil, err
		}
		// dir stays empty: there is no tree to point at.
		return &toolenv{target: t, tools: tl, res: res, prefix: prefix, view: view}, nil
	}
	id, err := Toolchain(e, t.Triple)
	if err != nil {
		return nil, err
	}
	tl, err := toolsFor(id.Dir, t.Triple)
	if err != nil {
		return nil, err
	}
	return &toolenv{target: t, tools: tl, res: res, dir: id.Dir, prefix: prefix, view: view}, nil
}

func (te *toolenv) cflags() []string {
	out := make([]string, 0, len(te.view)*3+len(te.res.CFlags))
	for _, v := range te.view {
		out = append(out,
			"-I"+filepath.Join(v, "include"),
			// util-linux installs uuid.h under include/uuid and ncurses puts
			// the wide headers under include/ncursesw, but CPython includes
			// both as <uuid.h> and <curses.h>.
			"-I"+filepath.Join(v, "include", "uuid"),
			"-I"+filepath.Join(v, "include", "ncursesw"))
	}
	return append(out, te.res.CFlags...)
}

func (te *toolenv) ldflags() []string {
	out := append([]string{}, te.res.LDFlags...)
	if te.res.Strip {
		out = append(out, "-s")
	}
	for _, v := range te.view {
		// lib64 as well as lib: OpenSSL's platform configs disagree about
		// which one they install into, and the loser is silently not found.
		out = append(out, "-L"+filepath.Join(v, "lib"), "-L"+filepath.Join(v, "lib64"))
	}
	if te.dir == "" {
		// Joining an empty dir would produce a relative -L that silently
		// resolves against the build cwd.
		return out
	}
	return append(out, "-L"+filepath.Join(te.dir, te.target.Triple, "lib"))
}

func (te *toolenv) pkgConfigPath() string {
	var dirs []string
	for _, v := range te.view {
		dirs = append(dirs,
			filepath.Join(v, "lib", "pkgconfig"),
			filepath.Join(v, "lib64", "pkgconfig"),
			filepath.Join(v, "share", "pkgconfig"))
	}
	if len(dirs) == 0 {
		// An *empty* PKG_CONFIG_LIBDIR does not mean "search nothing", it means
		// "use the compiled-in default", which is the host's /usr/lib/pkgconfig.
		// That is how pyhost -- a build with no sysroot at all -- came to detect
		// the host's zlib, define USE_ZLIB_CRC32, and then fail compiling
		// binascii.c against a toolchain that has no zlib.h.
		return filepath.Join(te.prefix, ".no-pkg-config")
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

func (te *toolenv) vars() map[string]string {
	pc := te.pkgConfigPath()
	env := map[string]string{
		"CC":     te.tools.CC,
		"AR":     te.tools.AR,
		"RANLIB": te.tools.RANLIB,
		"NM":     te.tools.NM,
		"LD":     te.tools.LD,
		// zlib's hand-rolled configure takes no --host; CHOST is how it learns
		// the cross prefix, and every autotools script ignores it.
		"CHOST":    te.target.Triple,
		"CFLAGS":   strings.Join(te.cflags(), " "),
		"CPPFLAGS": strings.Join(te.cflags(), " "),
		"CXXFLAGS": strings.Join(append(te.cflags(), te.res.CXXFlags...), " "),
		"LDFLAGS":  strings.Join(te.ldflags(), " "),
		// Pinned, not merely prepended: a configure script that reaches
		// /usr/lib/pkgconfig mixes the host's libffi or libuuid into a static
		// cross build, and the result only fails at link time on another
		// machine. LIBDIR replaces the default search path outright.
		"PKG_CONFIG_LIBDIR":      pc,
		"PKG_CONFIG_PATH":        pc,
		"PKG_CONFIG_SYSROOT_DIR": "",
		// The Runner substitutes the hermetic PATH; composing one here would
		// reintroduce whatever the developer has installed.
		"PATH": core.PathSentinel + te.target.Triple,
		// Deterministic diagnostics, and a few configure scripts parse them.
		"LC_ALL": "C",
	}
	if te.tools.CXX != "" {
		env["CXX"] = te.tools.CXX
	}
	return env
}

func (te *toolenv) cmd(name, dir string, args []string, extra map[string]string) core.Cmd {
	env := te.vars()
	for k, v := range extra {
		env[k] = v
	}
	return core.Cmd{Dir: dir, Args: args, Name: name, EnvAdd: env}
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyTree copies src to dst preserving modes, symlinks and mtimes. Build
// trees are copied rather than hard-linked: configure and make write all over
// their source tree, and a hard link would edit the shared srctree artifact.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			os.Remove(target)
			return os.Symlink(link, target)
		case !info.Mode().IsRegular():
			return fmt.Errorf("recipe: %s is neither a file, a directory nor a symlink; a source tree should not contain one", p)
		}
		if err := copyFile(p, target, info.Mode().Perm()); err != nil {
			return err
		}
		return os.Chtimes(target, info.ModTime(), info.ModTime())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// maxRewrite bounds what we are willing to read into memory looking for baked
// prefixes. Nothing that carries one (.pc, .la, a -config script) is large.
const maxRewrite = 4 << 20

// isText reports whether b is something a prefix rewrite can safely operate
// on. Rewriting a binary would change its length and corrupt it.
func isText(b []byte) bool {
	if len(b) > maxRewrite || !utf8.Valid(b) {
		return false
	}
	for _, c := range b {
		if c == 0 {
			return false
		}
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The one toolchain staticpy does not provision, so the one whose identity has
// to be derived rather than read: hostcc fingerprints the driver and the libc
// headers together, because a distro glibc upgrade changes what the artifact is
// without touching the compiler.
func ToolchainHost(ctx context.Context) (ToolchainID, error) {
	cc, err := hostcc.Find()
	if err != nil {
		return ToolchainID{}, err
	}
	if v, ok := toolchainCache.Load(hostCacheKey + cc); ok {
		return v.(ToolchainID), nil
	}
	id, err := hostcc.Identify(ctx, cc)
	if err != nil {
		return ToolchainID{}, err
	}
	out := ToolchainID{
		Triple: id.Triple,
		Kind:   KindHost,
		CC:     cc,
		Key:    id.Key,
		Source: toolchainHost,
		Probe:  id.Describe(),
	}
	toolchainCache.Store(hostCacheKey+cc, out)
	return out, nil
}

// The cache is shared with provisioned toolchains, which key on a directory.
// A prefix keeps a compiler path from ever colliding with one.
const hostCacheKey = "host\x00"

// Unlike a provisioned tree there is no triple prefix to prefer, so an
// unprefixed name is the only candidate and a missing one is fatal rather than
// something to fall back from.
func hostToolsFor(cc string) (tools, error) {
	t := tools{CC: cc}
	var err error
	// gcc-ar and friends for the LTO plugin, for the reason toolsFor gives.
	if t.AR, err = lookHostTool("gcc-ar", "ar"); err != nil {
		return t, err
	}
	if t.RANLIB, err = lookHostTool("gcc-ranlib", "ranlib"); err != nil {
		return t, err
	}
	if t.NM, err = lookHostTool("gcc-nm", "nm"); err != nil {
		return t, err
	}
	if t.Objcopy, err = lookHostTool("objcopy"); err != nil {
		return t, err
	}
	if t.LD, err = lookHostTool("ld"); err != nil {
		return t, err
	}
	t.CXX, _ = lookHostTool("g++", "c++")
	return t, nil
}

func lookHostTool(names ...string) (string, error) {
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("recipe: no %s on PATH; the host-built profile needs the machine's own binutils", names[0])
}

// Dispatching here is what keeps every job family from having to know that
// host-built profiles exist.
func toolchainFor(e *core.Env, res config.Resolved, triple string) (ToolchainID, error) {
	if res.HostBuilt() {
		return ToolchainHost(context.Background())
	}
	return Toolchain(e, triple)
}
