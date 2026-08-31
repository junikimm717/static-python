package cli

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

var cmdConfig = &command{
	Name:     "config",
	Short:    "show the resolved configuration, or write it out to edit",
	Synopsis: "staticpy config show [--profile NAME] [--scope SCOPE] | staticpy config dump <dir>",
	Long: `Configuration lives in one place: config/ at the repo root, which is a
symlink to the tree go:embed compiles into this binary. Editing it and running
any command rebuilds the binary around the change; there is no second copy to
keep in step.

--config <dir> layers another directory on top, winning per entry, so a profile
redefined there replaces the embedded one of the same name and profiles only
the embedded set knows about survive untouched.

sources.toml and patches/ are deliberately NOT part of that stack. They come
from the binary unless --sources <dir> is passed explicitly: if any config
directory lying next to the binary could redefine a sha256, pinning would only
be documenting what was downloaded rather than constraining it.

show [--profile NAME] [--scope SCOPE]
  Prints what the current flags actually resolve to, and which file each layer
  came from. A profile is flattened per scope, so the same profile answers
  differently for the interpreter than for a dependency:

    deps         every native library
    deps.<pkg>   one of them, layered on top of deps
    python       the shipped interpreter
    pyhost       the throwaway host interpreter a cross build freezes with

  With no --scope, every scope is shown.

dump <dir>
  Writes the resolved configuration out as TOML, so someone holding only a
  released binary can start editing without cloning the repository:

    staticpy config dump ./myconfig
    staticpy --config ./myconfig build

  This is the merged view re-serialised. The values are exact; the commentary
  that explains them in the repository's own config/ tree is not carried over,
  because it does not survive parsing.`,
	Run: runConfig,
}

func runConfig(g *Global, args []string) error {
	if len(args) == 0 {
		return usagef("need a subcommand: show or dump")
	}
	switch args[0] {
	case "show":
		return runConfigShow(g, args[1:])
	case "dump":
		return runConfigDump(g, args[1:])
	}
	return usagef("unknown subcommand %q: want show or dump", args[0])
}

func runConfigShow(g *Global, args []string) error {
	fs := g.flagSet("config")
	scope := fs.String("scope", "", "flatten for this scope only: deps, deps.<pkg>, python, pyhost")
	if err := parse(fs, args); err != nil {
		return finish("config", err)
	}
	cfg, err := g.load()
	if err != nil {
		return err
	}
	if _, ok := cfg.Profiles[g.Profile]; !ok {
		return fmt.Errorf("unknown profile %q.\nDefined profiles: %s", g.Profile, strings.Join(sortedKeys(cfg.Profiles), ", "))
	}
	scopes := []string{config.ScopeDeps, config.ScopePython, config.ScopePyhost}
	if *scope != "" {
		scopes = []string{*scope}
	}

	resolved := map[string]config.Resolved{}
	for _, s := range scopes {
		r, err := cfg.Resolve(g.Profile, s)
		if err != nil {
			return err
		}
		resolved[s] = r
	}
	if g.JSON {
		return emitJSON(map[string]any{
			"profile":  g.Profile,
			"inherits": inheritChain(cfg, g.Profile),
			"origin":   cfg.Origin,
			"scopes":   resolved,
		})
	}

	fmt.Printf("%s %s\n", bold("profile:"), g.Profile)
	if chain := inheritChain(cfg, g.Profile); len(chain) > 1 {
		fmt.Printf("%s\n", dim("inherits: "+strings.Join(chain, " <- ")))
	}
	fmt.Printf("\n%s\n", bold("LAYERS"))
	ot := newTable("FILE", "CAME FROM")
	for _, name := range sortedKeys(cfg.Origin) {
		origin := cfg.Origin[name]
		if origin == config.OriginEmbedded {
			origin = dim("embedded in this binary")
		}
		ot.add(name, origin)
	}
	ot.render(os.Stdout)

	for _, s := range scopes {
		r := resolved[s]
		fmt.Printf("\n%s\n", bold("SCOPE "+s))
		t := newTable("KEY", "VALUE")
		t.add("cflags", strings.Join(r.CFlags, " "))
		t.add("cxxflags", strings.Join(r.CXXFlags, " "))
		t.add("ldflags", strings.Join(r.LDFlags, " "))
		t.add("strip", fmt.Sprint(r.Strip))
		t.add("debug_symbols", fmt.Sprint(r.Debug))
		if r.LTOMode != "" {
			t.add("lto_mode", r.LTOMode)
		}
		t.add("pgo", r.PGO)
		t.add("profile_task", r.ProfileTask)
		t.add("test_modules", fmt.Sprint(r.TestModules))
		t.add("modules", r.Modules)
		t.add("bundle", r.Bundle)
		t.render(os.Stdout)
	}
	fmt.Printf("\n%s\n", dim("these values, not the file they came from, are what job keys hash: two configurations that resolve alike share their artifacts"))
	return nil
}

func inheritChain(cfg *config.Config, name string) []string {
	var out []string
	seen := map[string]bool{}
	for name != "" && !seen[name] {
		seen[name] = true
		out = append(out, name)
		name = cfg.Profiles[name].Inherit
	}
	return out
}

func runConfigDump(g *Global, args []string) error {
	fs := g.flagSet("config")
	force := fs.Bool("force", false, "overwrite files that already exist in the target directory")
	if err := parse(fs, args); err != nil {
		return finish("config", err)
	}
	if fs.NArg() == 0 {
		return usagef("need a directory to write into, e.g. `staticpy config dump ./myconfig`")
	}
	dir := fs.Arg(0)
	cfg, err := g.load()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	files := []struct {
		name  string
		build func() (map[string]any, error)
	}{
		{"targets.toml", func() (map[string]any, error) { return section("target", cfg.Targets) }},
		{"packages.toml", func() (map[string]any, error) { return section("package", cfg.Packages) }},
		{"profiles.toml", func() (map[string]any, error) { return profileSection(cfg) }},
		{"bundles.toml", func() (map[string]any, error) {
			return mergeSections(sec{"bundle", cfg.Bundles}, sec{"pkg", cfg.PyPackages})
		}},
		{"tests.toml", func() (map[string]any, error) { return section("expect", cfg.Expect) }},
		{"sources.toml", func() (map[string]any, error) { return section("source", cfg.Sources) }},
	}
	var written []string
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if _, err := os.Stat(path); err == nil && !*force {
			return fmt.Errorf("%s already exists; pass --force to overwrite it", path)
		}
		doc, err := f.build()
		if err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
		var buf bytes.Buffer
		fmt.Fprintf(&buf, dumpHeader, f.name)
		if len(doc) > 0 {
			if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
				return fmt.Errorf("%s: %w", f.name, err)
			}
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			return err
		}
		written = append(written, f.name)
	}

	patches, err := dumpPatches(cfg, dir)
	if err != nil {
		return err
	}

	fmt.Printf("%s %d file%s into %s\n  %s\n", green("wrote"), len(written), plural(len(written)), dir, strings.Join(written, "  "))
	if patches > 0 {
		fmt.Printf("  patches/  (%d file%s)\n", patches, plural(patches))
	}
	fmt.Printf("\nEdit them and pass the directory back:\n  %s\n\n", cyan("staticpy --config "+dir+" build"))
	fmt.Printf("%s\n", dim("targets/packages/profiles/bundles/tests are overlaid on top of the embedded defaults, so a file you delete falls back rather than disappearing."))
	fmt.Printf("%s\n", dim("sources.toml and patches/ are different: they are the pinned supply chain and are read only from this binary unless you also pass --sources "+dir+"."))
	return nil
}

// dumpPatches writes out the assets sources.toml actually refers to. They
// cannot be listed, only opened by name, which is enough: an asset no source
// names is one no build reads.
func dumpPatches(cfg *config.Config, dir string) (int, error) {
	n := 0
	for _, name := range sortedKeys(cfg.Sources) {
		s := cfg.Sources[name]
		var wanted []string
		wanted = append(wanted, s.Patches...)
		for _, names := range s.TargetPatches {
			wanted = append(wanted, names...)
		}
		for _, e := range s.Edits {
			if e.TextFile != "" {
				wanted = append(wanted, e.TextFile)
			}
		}
		for _, rel := range wanted {
			b, err := cfg.OpenAsset(path.Join(config.AssetDir(s), rel))
			if err != nil {
				return n, err
			}
			dst := filepath.Join(dir, "patches", config.AssetDir(s), filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return n, err
			}
			if err := os.WriteFile(dst, b, 0o644); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

const dumpHeader = `# %s, written by ` + "`staticpy config dump`" + `.
#
# This is the configuration this binary resolved, re-serialised: the values are
# exact, but the commentary explaining them does not survive parsing. The
# annotated originals live in config/ in the staticpy repository.
#
# Pass this directory back with --config <dir>. Entries are merged per
# top-level table, so an entry you delete falls back to the embedded default
# rather than disappearing.

`

type sec struct {
	name string
	data any
}

func section(name string, data any) (map[string]any, error) {
	return mergeSections(sec{name, data})
}

// The trip through a map is what lets empty values be dropped: an emitted
// `cflags = []` would read back as "replace the inherited list with nothing",
// which is not what an absent field means.
func mergeSections(secs ...sec) (map[string]any, error) {
	out := map[string]any{}
	for _, s := range secs {
		m, err := toMap(map[string]any{s.name: s.data})
		if err != nil {
			return nil, err
		}
		if sub, ok := m[s.name].(map[string]any); ok && len(sub) > 0 {
			out[s.name] = sub
		}
	}
	return out, nil
}

// profileSection re-nests Profile.Scopes, which the struct cannot carry: TOML
// folds [profile.nolto.python] into the parent table, so it is dropped on
// decode and has to be put back by hand.
func profileSection(cfg *config.Config) (map[string]any, error) {
	profiles := map[string]any{}
	for _, name := range sortedKeys(cfg.Profiles) {
		p := cfg.Profiles[name]
		m, err := toMap(p)
		if err != nil {
			return nil, err
		}
		for _, scope := range sortedKeys(p.Scopes) {
			sm, err := toMap(p.Scopes[scope])
			if err != nil {
				return nil, err
			}
			parts := strings.Split(scope, ".")
			cur := m
			for _, part := range parts[:len(parts)-1] {
				next, ok := cur[part].(map[string]any)
				if !ok {
					next = map[string]any{}
					cur[part] = next
				}
				cur = next
			}
			last := parts[len(parts)-1]
			if existing, ok := cur[last].(map[string]any); ok {
				for k, v := range sm {
					existing[k] = v
				}
			} else {
				cur[last] = sm
			}
		}
		profiles[name] = m
	}
	if len(profiles) == 0 {
		return map[string]any{}, nil
	}
	return map[string]any{"profile": profiles}, nil
}

func toMap(v any) (map[string]any, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	var m map[string]any
	if err := toml.Unmarshal(buf.Bytes(), &m); err != nil {
		return nil, err
	}
	return stripEmpty(m), nil
}

func stripEmpty(m map[string]any) map[string]any {
	for k, v := range m {
		switch t := v.(type) {
		case string:
			if t == "" {
				delete(m, k)
			}
		case []any:
			if len(t) == 0 {
				delete(m, k)
			}
		case []map[string]any:
			if len(t) == 0 {
				delete(m, k)
			}
		case map[string]any:
			if len(stripEmpty(t)) == 0 {
				delete(m, k)
			}
		}
	}
	return m
}
