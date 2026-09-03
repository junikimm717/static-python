// Package config is the data half of the build system: every knob that can be
// turned without a Go compiler in the loop.
//
// Files are embedded as defaults and overlaid from disk, so a released binary
// works alone and a checkout wins over it. Resolution order is
// embedded -> --config <dir>, with sources.toml and patches/
// deliberately excluded from the automatic layer; see Load.
//
// This file is the contract the rest of the tree compiles against.
package config

// Mirrors are tried in order.
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
	// TargetPatches are keyed by triple and applied on top of Patches, to the
	// private copy a build stages rather than to the shared srctree. A diff
	// listed in Patches moves the tree's key and with it every target; one
	// keyed here reaches that target's key and nothing else.
	TargetPatches map[string][]string `toml:"target_patches"`
	// Edits are content-anchored fixups for things a version-pinned diff cannot
	// survive; see Edit.
	Edits []Edit `toml:"edits"`
}

// Edit is a content-anchored source fixup, and is the exception rather than the
// tool of choice: patches/ holds real diffs, and `patch` already fails loudly
// when its context moves.
//
// Reach for an Edit only when the edit must survive an upstream version bump
// unreviewed. Every source here is sha256-pinned, so for a given pin a diff can
// never spuriously fail — which means a diff that breaks on a bump is a signal
// worth having, not a cost. The one place that trade goes the other way is the
// ctypes injection: CPython patch releases are frequent and the injection is
// mechanical, so an anchor (one line) beats diff context (three) there.
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
	MustMatch int `toml:"must_match"`
	// Regex matches Anchor as a regular expression instead of comparing whole
	// lines literally. Off by default: an anchor is a line of somebody else's
	// source, and characters like ( and ) are ordinary there while a regex
	// would read them as syntax and match nothing at all.
	Regex bool   `toml:"regex"`
	Why   string `toml:"why"`
}

// A native library, built into its own prefix so a stale artifact cannot
// survive a version bump in a shared one.
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
	Sources []string `toml:"sources"`
	CFlags  []string `toml:"cflags"`
	Headers []string `toml:"headers"`
	Libname string   `toml:"libname"`
	// Object collapses the compiled sources into one relocatable object at
	// this path, in addition to the archive. An interpreter links objects
	// directly: an archive member is only pulled to resolve an undefined
	// symbol, so it can never override a definition libc already supplies.
	Object string `toml:"object"`
	// KeepGlobals are the only symbols left global in Object; everything else
	// is localised so a whole allocator's internals cannot collide with
	// whatever else is in the link.
	KeepGlobals []string `toml:"keep_globals"`
	// MakeVars are passed on the make command line, where they beat any
	// assignment in the makefile. @BUILD_CC@ expands to a compiler whose output
	// runs on the build machine, for packages that compile helper programs and
	// then execute them mid-build.
	MakeVars []string `toml:"make_vars"`
	Needs    []string `toml:"needs"`
	Provides []string `toml:"provides"` // artifact paths that must exist afterwards
	// Keyed by profile name: [package.libffi.profile.reference]. The static
	// build wants --disable-shared and a .a while a host-linked variant wants
	// the opposite, and that is a per-package decision, so it belongs here
	// rather than in a flag list.
	Variants map[string]PackageVariant `toml:"profile"`
}

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
	// MakeVars are passed on CPython's make command line, where they beat the
	// makefile's own assignment. They reach the key only when set, so giving one
	// target a variable leaves every other target's key untouched.
	MakeVars []string `toml:"make_vars"`
}

// PackageVariant replaces, rather than merges into, the fields it sets: a
// shared build's configure line is not the static one with extras, and a
// half-overridden argument list produces a library that links but is not what
// was asked for.
type PackageVariant struct {
	Configure []string `toml:"configure"`
	Provides  []string `toml:"provides"`
	MakeVars  []string `toml:"make_vars"`
	// Drops the package from this profile. mimalloc is the case: it exists to
	// beat musl's allocator at static link, and a reference build that shipped
	// it would not be the stock interpreter it is measured against. A pointer
	// so a child can un-skip; a bool can only set true in the inherit walk.
	Skip *bool `toml:"skip"`
}

// Scopes layer on top of the profile-wide values, so a change confined to one
// scope rebuilds only what that scope reaches.
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
	// Host-built LTO is CPython's --with-lto, not the flag lists the static
	// build uses. Unset means on (the recipe default).
	LTO *bool `toml:"lto"`
	// LTOMode selects how LTO IR is consumed. Empty is whole-program at the
	// python link (slim archives, one WHOPR). "per-dep" runs LTO on each
	// library to native code first, so CPython cannot inline across that
	// boundary.
	LTOMode string `toml:"lto_mode"`
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
	// A profile property and not a target one because the same triple can be
	// both: on a musl host, a host-built variant and the shipped static build
	// are both x86_64-linux-musl, and only the profile tells them apart.
	Toolchain string `toml:"toolchain"`

	// Scopes are "deps", "deps.<pkg>", "python", "pyhost".
	Scopes map[string]Profile `toml:"-"`
}

// Jobs hash this, never the file it came from, so adding an unrelated profile
// invalidates nothing.
type Resolved struct {
	Profile string
	Scope   string

	CFlags   []string
	CXXFlags []string
	LDFlags  []string

	Strip       bool
	Debug       bool
	LTO         bool
	LTOSet      bool
	LTOMode     string
	PGO         string
	ProfileTask string
	TestModules bool
	Modules     string
	Bundle      string
	Toolchain   string
}

func (r Resolved) HostBuilt() bool { return r.Toolchain == ToolchainHost }

// EffectiveLTO is what the recipe will pass: unset is on, because that is
// CPython's --with-lto default for a host-built profile.
func (r Resolved) EffectiveLTO() bool { return !r.LTOSet || r.LTO }

// LTOModeWholeGraph is slim IR in every archive and one WHOPR at the python
// link. Empty in the file means this.
const LTOModeWholeGraph = "whole-graph"

// LTO each static archive to native code after install, rather than leaving
// slim IR for the python link to WHOPR over.
const LTOModePerDep = "per-dep"

// It must contain no absolute path and no value that varies between runs.
func (r Resolved) KeyInputs() map[string]string { return r.keyInputs() }

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
	// Ignore is handed to regrtest as -i, which matches test cases and methods
	// rather than files. Skip and Fail are judged against what regrtest reports,
	// and it reports whole files, so a single impossible method can only be
	// expressed here -- the alternative is declaring its entire file expected to
	// fail, which hides every future regression in it.
	Ignore []TestEntry `toml:"ignore"`
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
	Bench      BenchConfig           `toml:"bench"`
	Kits       map[string]Kit        `toml:"kit"`

	// Origin records, per file, where the winning copy came from: "embedded" or
	// an absolute path. It feeds Manifest.Provenance so a build assembled from
	// an overlay is distinguishable from one that was not.
	Origin map[string]string `toml:"-"`
}

// Pins for the pyperformance suite. The lineup is whatever --interp names;
// there is no baked-in matrix, because the comparison set keeps changing.
type BenchConfig struct {
	Pyperformance string               `toml:"pyperformance"`
	Pyperf        string               `toml:"pyperf"`
	Vendor        map[string]VendorPin `toml:"vendor"`
}

// VendorPin is a sha256-pinned sdist shipped inside a kit so the quiet box
// can pip-install the suite without PyPI for those two packages.
type VendorPin struct {
	File   string   `toml:"file"`
	SHA256 string   `toml:"sha256"`
	URLs   []string `toml:"urls"`
}

// Kit is a named comparison set: several profiles packed together with a
// runner so a quiet machine can unzip and measure without a checkout.
type Kit struct {
	Baseline string   `toml:"baseline"`
	Arms     []string `toml:"arms"`
}
