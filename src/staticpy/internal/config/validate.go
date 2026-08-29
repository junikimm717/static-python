package config

import (
	"fmt"
	"sort"
	"strings"
)

var (
	buildShapes  = map[string]bool{"autotools": true, "openssl": true, "sources": true, "make": true}
	editActions  = map[string]bool{"insert_after": true, "insert_before": true, "replace_line": true, "delete_line": true}
	targetStatus = map[string]bool{"proven": true, "experimental": true}
	pgoModes     = map[string]bool{"off": true, "native-only": true, "on": true}
	moduleSets   = map[string]bool{"minimal": true, "full": true}
)

// Validate reports the first inconsistency across the merged config. It runs at
// the end of Load, so no caller ever sees a Config that names something absent.
func (c *Config) Validate() error {
	for _, check := range []func() error{
		c.validateSources,
		c.validateTargets,
		c.validatePackages,
		c.validateProfiles,
		c.validatePyPackages,
		c.validateBundles,
	} {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) Target(triple string) (Target, error) {
	t, ok := c.Targets[triple]
	if !ok {
		return Target{}, fmt.Errorf("unknown target %q (supported: %s)", triple, keysOf(c.Targets))
	}
	return t, nil
}

func (c *Config) validateSources() error {
	for key, s := range c.Sources {
		where := fmt.Sprintf("source %q", key)
		if s.Name != key {
			return fmt.Errorf("%s: name is %q; the table key and name must match", where, s.Name)
		}
		if s.Version == "" || s.File == "" {
			return fmt.Errorf("%s: version and file are required", where)
		}
		if len(s.URLs) == 0 {
			return fmt.Errorf("%s: no urls", where)
		}
		if !isSHA256(s.SHA256) {
			return fmt.Errorf("%s: sha256 %q is not 64 lowercase hex digits", where, s.SHA256)
		}
		// A mistyped triple is otherwise silent: nothing applies, no key moves.
		for triple, names := range s.TargetPatches {
			if _, ok := c.Targets[triple]; !ok {
				return fmt.Errorf("%s: target_patches names %q, which is not a target in targets.toml (have %s)",
					where, triple, keysOf(c.Targets))
			}
			if len(names) == 0 {
				return fmt.Errorf("%s: target_patches.%s is empty; drop the key instead", where, triple)
			}
		}
		for i, e := range s.Edits {
			ew := fmt.Sprintf("%s: edit %d", where, i)
			if e.File == "" || e.Anchor == "" {
				return fmt.Errorf("%s: file and anchor are required", ew)
			}
			if !editActions[e.Action] {
				return fmt.Errorf("%s: unknown action %q (want %s)", ew, e.Action, keysOf(editActions))
			}
			if e.Text != "" && e.TextFile != "" {
				return fmt.Errorf("%s: text and text_file are mutually exclusive", ew)
			}
			if e.Action != "delete_line" && e.Text == "" && e.TextFile == "" {
				return fmt.Errorf("%s: %s needs text or text_file", ew, e.Action)
			}
			if e.Action == "delete_line" && (e.Text != "" || e.TextFile != "") {
				return fmt.Errorf("%s: delete_line takes no text", ew)
			}
			if e.MustMatch < 0 {
				return fmt.Errorf("%s: must_match is negative", ew)
			}
			if strings.TrimSpace(e.Why) == "" {
				return fmt.Errorf("%s: needs a why; an edit nobody can explain is an edit nobody can retire", ew)
			}
		}
	}
	return nil
}

func (c *Config) validateTargets() error {
	for key, t := range c.Targets {
		where := fmt.Sprintf("target %q", key)
		if t.Triple != key {
			return fmt.Errorf("%s: triple is %q; the table key and triple must match", where, t.Triple)
		}
		if t.Arch == "" || t.ABI == "" {
			return fmt.Errorf("%s: arch and abi are required", where)
		}
		if t.Bits != 32 && t.Bits != 64 {
			return fmt.Errorf("%s: bits is %d, want 32 or 64", where, t.Bits)
		}
		if !targetStatus[t.Status] {
			return fmt.Errorf("%s: status %q, want proven or experimental", where, t.Status)
		}
	}
	return nil
}

func (c *Config) validatePackages() error {
	for key, p := range c.Packages {
		where := fmt.Sprintf("package %q", key)
		if p.Name != key {
			return fmt.Errorf("%s: name is %q; the table key and name must match", where, p.Name)
		}
		src := p.Source
		if src == "" {
			src = p.Name
		}
		if _, ok := c.Sources[src]; !ok {
			return fmt.Errorf("%s: source %q is not defined in %s (have %s)", where, src, sourcesFile, keysOf(c.Sources))
		}
		build := p.Build
		if build == "" {
			build = "autotools"
		}
		if !buildShapes[build] {
			return fmt.Errorf("%s: unknown build %q (want %s)", where, build, keysOf(buildShapes))
		}
		if build == "sources" && len(p.Sources) == 0 {
			return fmt.Errorf("%s: build = \"sources\" needs a sources list", where)
		}
		if build != "sources" && len(p.Sources) > 0 {
			return fmt.Errorf("%s: sources is only used by build = \"sources\"", where)
		}
		for _, n := range p.Needs {
			if _, ok := c.Packages[n]; !ok {
				return fmt.Errorf("%s: needs %q, which is not a package (have %s)", where, n, keysOf(c.Packages))
			}
		}
		// A variant keyed on a profile that does not exist is dead config: it
		// looks like the package was overridden while the build silently uses the
		// original. Same reasoning as a *_remove that matches nothing.
		for prof, v := range p.Variants {
			if _, ok := c.Profiles[prof]; !ok {
				return fmt.Errorf("%s: [package.%s.profile.%s] names no profile (have %s)",
					where, key, prof, keysOf(c.Profiles))
			}
			if v.Configure == nil && v.Provides == nil && v.MakeVars == nil && !v.Skip {
				return fmt.Errorf("%s: [package.%s.profile.%s] overrides nothing; drop it, or set configure, provides, make_vars or skip",
					where, key, prof)
			}
		}
		if p.PlatformMap == "" {
			continue
		}
		for triple, t := range c.Targets {
			if t.Maps[p.PlatformMap] == "" {
				return fmt.Errorf("%s: platform_map %q has no maps.%s on target %q",
					where, p.PlatformMap, p.PlatformMap, triple)
			}
		}
	}
	return nil
}

var toolchainSources = map[string]bool{ToolchainProvisioned: true, ToolchainHost: true}

func (c *Config) validateProfiles() error {
	for name, p := range c.Profiles {
		if p.Toolchain != "" && !toolchainSources[p.Toolchain] {
			return fmt.Errorf("profile %q: toolchain %q, want %q or %q",
				name, p.Toolchain, ToolchainProvisioned, ToolchainHost)
		}
	}
	names := sortedKeys(c.Profiles)
	// Structure first, resolution second: a profile that only declares a scope
	// table replaces the whole entry it overlays, so an unresolvable inherited
	// flag list is usually a symptom of a mistake reported below.
	for _, name := range names {
		p := c.Profiles[name]
		if err := c.checkProfileValues(p, fmt.Sprintf("profile %q", name)); err != nil {
			return err
		}
		for _, s := range sortedKeys(p.Scopes) {
			where := fmt.Sprintf("profile %q scope %q", name, s)
			if _, err := scopeLayers(s); err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
			if pkg, ok := strings.CutPrefix(s, ScopeDeps+"."); ok {
				if _, known := c.Packages[pkg]; !known {
					return fmt.Errorf("%s: %q is not a package (have %s)", where, pkg, keysOf(c.Packages))
				}
			}
			if err := c.checkProfileValues(p.Scopes[s], where); err != nil {
				return err
			}
		}
	}
	// Resolve every scope now so a *_remove that matches nothing fails at load
	// rather than hours later, when that one scope finally builds.
	for _, name := range names {
		scopes := append([]string{ScopeDeps, ScopePython, ScopePyhost}, depScopes(sortedKeys(c.Profiles[name].Scopes))...)
		for _, s := range scopes {
			if _, err := c.Resolve(name, s); err != nil {
				return err
			}
		}
	}
	return nil
}

func depScopes(scopes []string) []string {
	var out []string
	for _, s := range scopes {
		if strings.HasPrefix(s, ScopeDeps+".") {
			out = append(out, s)
		}
	}
	return out
}

func (c *Config) checkProfileValues(p Profile, where string) error {
	if p.PGO != "" && !pgoModes[p.PGO] {
		return fmt.Errorf("%s: pgo %q, want %s", where, p.PGO, keysOf(pgoModes))
	}
	if p.Modules != "" && !moduleSets[p.Modules] {
		return fmt.Errorf("%s: modules %q, want %s", where, p.Modules, keysOf(moduleSets))
	}
	if p.Bundle != "" {
		if _, ok := c.Bundles[p.Bundle]; !ok {
			return fmt.Errorf("%s: bundle %q is not defined (have %s)", where, p.Bundle, keysOf(c.Bundles))
		}
	}
	lists := map[string][]string{
		"cflags":         p.CFlags,
		"cflags_add":     p.CFlagsAdd,
		"cxxflags":       p.CXXFlags,
		"ldflags":        p.LDFlags,
		"ldflags_add":    p.LDFlagsAdd,
		"cflags_remove":  p.CFlagsRemove,
		"ldflags_remove": p.LDFlagsRemove,
	}
	for _, field := range sortedKeys(lists) {
		for _, f := range lists[field] {
			if hasAbsPath(f) {
				return fmt.Errorf("%s: %s %q contains an absolute path; -I/-L paths belong to the job, "+
					"because anything absolute here would end up in the job key", where, field, f)
			}
		}
	}
	return nil
}

func (c *Config) validatePyPackages() error {
	for key, p := range c.PyPackages {
		where := fmt.Sprintf("pkg %q", key)
		if p.Name != key {
			return fmt.Errorf("%s: name is %q; the table key and name must match", where, p.Name)
		}
		for _, n := range p.Needs {
			if _, ok := c.Packages[n]; !ok {
				return fmt.Errorf("%s: needs %q, which is not a package (have %s)", where, n, keysOf(c.Packages))
			}
		}
	}
	return nil
}

func (c *Config) validateBundles() error {
	for key, b := range c.Bundles {
		for _, p := range b.Packages {
			if _, ok := c.PyPackages[p]; !ok {
				return fmt.Errorf("bundle %q: %q is not a [pkg.*] entry (have %s)", key, p, keysOf(c.PyPackages))
			}
		}
	}
	if err := c.validateExpect(); err != nil {
		return err
	}
	return nil
}

// In any of the shapes flags use: /x, -I/x, -L/x, -Wl,-rpath,/x, --sysroot=/x.
func hasAbsPath(flag string) bool {
	for i := 0; i < len(flag); i++ {
		if flag[i] != '/' {
			continue
		}
		if i == 0 {
			return true
		}
		switch flag[i-1] {
		case '=', ',', ':':
			return true
		}
		if i == 2 && flag[0] == '-' {
			return true
		}
	}
	return false
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func keysOf[V any](m map[string]V) string { return strings.Join(sortedKeys(m), ", ") }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// An expect key names a scope or a target, and a typo in one is otherwise
// invisible: the lookup simply finds nothing and every test it meant to cover
// is reported as an unexpected failure.
func (c *Config) validateExpect() error {
	for _, key := range sortedKeys(c.Expect) {
		name := key
		if triple, runner, ok := strings.Cut(key, ":"); ok {
			if runner != "native" && runner != "qemu" {
				return fmt.Errorf("config: expect key %q: runner must be \"native\" or \"qemu\"", key)
			}
			name = triple
		}
		if name == "all" || name == "static" || name == "native" || name == "qemu" {
			continue
		}
		if _, ok := c.Targets[name]; !ok {
			return fmt.Errorf("config: expect key %q names no target; use a triple from targets.toml, or one of the scopes \"all\", \"static\", \"native\", \"qemu\"", key)
		}
	}
	return nil
}
