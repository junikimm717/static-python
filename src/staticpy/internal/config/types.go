// Package config is the data half of the build system: every knob that can be
// turned without a Go compiler in the loop.
//
// Files are embedded as defaults and overlaid from disk, so a released binary
// works alone and a checkout wins over it. Resolution order is
// embedded -> <repo>/config -> --config <dir>, with sources.toml and patches/
// deliberately excluded from the automatic layer; see Load.
//
// This file is the contract the rest of the tree compiles against.
package config

// Source is one pinned upstream input. Mirrors are tried in order.
type Source struct {
	Name    string   `toml:"name"`
	Version string   `toml:"version"`
	File    string   `toml:"file"`
	URLs    []string `toml:"urls"`
	SHA256  string   `toml:"sha256"`
	// TopDir is the directory the archive unpacks into, stripped to produce a
	// flat srctree. Empty means the archive has no wrapper directory.
	TopDir string `toml:"topdir"`
	// Patches are filenames under patches/<name>-<version>/, applied in order.
	Patches []string `toml:"patches"`
	// Edits are content-anchored fixups for things a version-pinned diff cannot
	// survive; see Edit.
	Edits []Edit `toml:"edits"`
}

// Edit is a content-anchored source fixup. Unlike a diff it survives an
// upstream patch release, and unlike sed it fails loudly when its anchor moves:
// a build that silently skipped an edit ships a subtly wrong interpreter.
type Edit struct {
	File   string `toml:"file"`
	Anchor string `toml:"anchor"`
	// Action is one of "insert_after", "insert_before", "replace_line",
	// "delete_line".
	Action string `toml:"action"`
	// Text is the replacement or inserted content. TextFile reads it from an
	// asset instead, for multi-line insertions.
	Text     string `toml:"text"`
	TextFile string `toml:"text_file"`
	// MustMatch is how many times Anchor is required to match. Zero means
	// exactly once. Any other count fails the job.
	MustMatch int    `toml:"must_match"`
	Why       string `toml:"why"`
}

// Package is a native library dependency built into its own prefix.
type Package struct {
	Name   string `toml:"name"`
	Source string `toml:"source"` // key into Sources; defaults to Name
	// Build selects the recipe shape: "autotools" (default), "openssl",
	// "sources" (compile a declared file list, no configure step), "make".
	Build     string   `toml:"build"`
	Configure []string `toml:"configure"`
	// PlatformMap names a per-target lookup in Target.Maps, e.g. openssl's
	// linux-ppc64le. Empty means the package takes plain --host=<triple>.
	PlatformMap string `toml:"platform_map"`
	// Sources and CFlags are used only by Build == "sources".
	Sources  []string `toml:"sources"`
	CFlags   []string `toml:"cflags"`
	Headers  []string `toml:"headers"`
	Libname  string   `toml:"libname"`
	Needs    []string `toml:"needs"`
	Provides []string `toml:"provides"` // artifact paths that must exist afterwards
}

// Target is one supported triple and everything that is true only of it.
// Adding an architecture is a row here plus a pyconfig asset.
type Target struct {
	Triple string `toml:"triple"`
	Arch   string `toml:"arch"`
	ABI    string `toml:"abi"`
	Bits   int    `toml:"bits"`
	// Libatomic forces -latomic where 64-bit atomics need a libatomic.
	Libatomic bool   `toml:"libatomic"`
	UInt128   bool   `toml:"uint128"`
	Qemu      string `toml:"qemu"`
	// Status is "proven" or "experimental"; experimental targets do not gate CI.
	Status string `toml:"status"`
	// Maps holds per-package platform names, e.g. maps.openssl = "linux-ppc64le".
	Maps map[string]string `toml:"maps"`
}

// Profile is a named flag set. Scopes layer on top of the profile-wide values,
// so a change confined to one scope rebuilds only what that scope reaches.
type Profile struct {
	Inherit string `toml:"inherit"`

	CFlags   []string `toml:"cflags"`
	LDFlags  []string `toml:"ldflags"`
	CXXFlags []string `toml:"cxxflags"`

	CFlagsAdd     []string `toml:"cflags_add"`
	CFlagsRemove  []string `toml:"cflags_remove"`
	LDFlagsAdd    []string `toml:"ldflags_add"`
	LDFlagsRemove []string `toml:"ldflags_remove"`

	Strip *bool `toml:"strip"`
	Debug *bool `toml:"debug_symbols"`
	// PGO is "off", "native-only", or "on".
	PGO         string `toml:"pgo"`
	ProfileTask string `toml:"profile_task"`
	// TestModules builds the _test* extension modules in. They are builtins in a
	// static interpreter, so this is a tax on every startup, not an optional
	// directory.
	TestModules *bool `toml:"test_modules"`
	// Modules selects a Setup.local module set: "minimal" or "full".
	Modules string `toml:"modules"`
	Bundle  string `toml:"bundle"`

	// Scopes are "deps", "deps.<pkg>", "python", "pyhost".
	Scopes map[string]Profile `toml:"-"`
}

// Resolved is one profile flattened for one scope. Jobs hash this, never the
// file it came from, so adding an unrelated profile invalidates nothing.
type Resolved struct {
	Profile string
	Scope   string

	CFlags   []string
	CXXFlags []string
	LDFlags  []string

	Strip       bool
	Debug       bool
	PGO         string
	ProfileTask string
	TestModules bool
	Modules     string
	Bundle      string
}

// KeyInputs is the contribution this resolved set makes to a job key. It must
// contain no absolute path and no value that varies between runs.
func (r Resolved) KeyInputs() map[string]string { return r.keyInputs() }

// Bundle is a set of Python packages compiled into the interpreter.
type Bundle struct {
	Packages []string `toml:"packages"`
}

// PyPackage is a third-party Python package linked in as builtins. A static
// interpreter cannot dlopen an extension, so every C module has to arrive this
// way or not at all.
type PyPackage struct {
	Name        string     `toml:"name"`
	Version     string     `toml:"version"`
	SdistSHA256 string     `toml:"sdist_sha256"`
	URLs        []string   `toml:"urls"`
	Needs       []string   `toml:"needs"`
	Modules     []PyModule `toml:"modules"`
	// PurePaths are directories copied into site-packages verbatim.
	PurePaths []string `toml:"pure_paths"`
}

type PyModule struct {
	// Name may be dotted. A dotted builtin is unreachable through
	// BuiltinImporter, so the generator emits a site-packages shim for it.
	Name    string   `toml:"name"`
	Sources []string `toml:"sources"`
	CFlags  []string `toml:"cflags"`
	Libs    []string `toml:"libs"`
}

// TestExpect declares what CPython's suite is expected to do on a given target
// and runner. An unexpected pass fails too: a skip list that only grows is how
// a suite quietly stops meaning anything.
type TestExpect struct {
	Skip []TestEntry `toml:"skip"`
	Fail []TestEntry `toml:"fail"`
}

type TestEntry struct {
	Test string `toml:"test"`
	Why  string `toml:"why"`
}

// Config is everything loaded, after overlay and before scope resolution.
type Config struct {
	Sources    map[string]Source     `toml:"source"`
	Packages   map[string]Package    `toml:"package"`
	Targets    map[string]Target     `toml:"target"`
	Profiles   map[string]Profile    `toml:"profile"`
	Bundles    map[string]Bundle     `toml:"bundle"`
	PyPackages map[string]PyPackage  `toml:"pkg"`
	Expect     map[string]TestExpect `toml:"expect"`

	// Origin records, per file, where the winning copy came from: "embedded" or
	// an absolute path. It feeds Manifest.Provenance so a build assembled from
	// an overlay is distinguishable from one that was not.
	Origin map[string]string `toml:"-"`
}
