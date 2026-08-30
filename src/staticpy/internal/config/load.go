package config

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Options selects which layers Load stacks.
type Options struct {
	// RepoRoot enables the <RepoRoot>/config layer when that directory exists.
	RepoRoot string
	// Dir is an explicit --config directory, applied last. It must exist.
	Dir string
	// SourcesDir overrides where sources.toml and patches/ are read from. Empty
	// means the embedded copy, which is the only way a pinned checksum can be
	// trusted: if any config/ lying next to the binary could redefine a sha256,
	// pinning would only be documenting what was downloaded.
	SourcesDir string
}

// OriginEmbedded is the Config.Origin value for a file that came from the
// binary rather than from disk.
const OriginEmbedded = "embedded"

const (
	sourcesFile = "sources.toml"
	patchesDir  = "patches"
)

type layer struct {
	fsys fs.FS
	// dir is the absolute path this layer was read from, empty when embedded.
	dir string
}

func diskLayer(dir string) (layer, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return layer{}, fmt.Errorf("config dir %s: %w", dir, err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return layer{}, fmt.Errorf("config dir %s: %w", abs, err)
	}
	if !st.IsDir() {
		return layer{}, fmt.Errorf("config dir %s: not a directory", abs)
	}
	return layer{fsys: os.DirFS(abs), dir: abs}, nil
}

func (l layer) origin(name string) string {
	if l.dir == "" {
		return OriginEmbedded
	}
	return filepath.Join(l.dir, name)
}

func (l layer) read(name string) (string, error) {
	b, err := fs.ReadFile(l.fsys, name)
	if err != nil {
		return "", fmt.Errorf("%s: %w", l.origin(name), err)
	}
	return string(b), nil
}

func (l layer) tomls() ([]string, error) {
	ents, err := fs.ReadDir(l.fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", l.origin("."), err)
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

// Load stacks embedded defaults, <RepoRoot>/config and --config <dir>, with
// later layers winning per top-level table entry: a profile redefined on disk
// replaces the embedded one of the same name, and profiles only the embedded
// set knows about survive untouched.
func Load(opts Options) (*Config, error) {
	cfg := &Config{
		Sources:    map[string]Source{},
		Packages:   map[string]Package{},
		Targets:    map[string]Target{},
		Profiles:   map[string]Profile{},
		Bundles:    map[string]Bundle{},
		PyPackages: map[string]PyPackage{},
		Expect:     map[string]TestExpect{},
		Origin:     map[string]string{},
	}

	layers := []layer{{fsys: embedded()}}
	if opts.Dir != "" {
		l, err := diskLayer(opts.Dir)
		if err != nil {
			return nil, err
		}
		layers = append(layers, l)
	}

	for _, l := range layers {
		names, err := l.tomls()
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if name == sourcesFile {
				continue
			}
			data, err := l.read(name)
			if err != nil {
				return nil, err
			}
			if err := mergeFile(cfg, data, l.origin(name), false); err != nil {
				return nil, err
			}
			cfg.Origin[name] = l.origin(name)
		}
	}

	sl := layers[0]
	if opts.SourcesDir != "" {
		l, err := diskLayer(opts.SourcesDir)
		if err != nil {
			return nil, err
		}
		sl = l
	}
	data, err := sl.read(sourcesFile)
	if err != nil {
		return nil, err
	}
	if err := mergeFile(cfg, data, sl.origin(sourcesFile), true); err != nil {
		return nil, err
	}
	cfg.Origin[sourcesFile] = sl.origin(sourcesFile)
	cfg.Origin[patchesDir] = sl.origin(patchesDir)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func mergeFile(dst *Config, data, origin string, allowSources bool) error {
	var src Config
	md, err := toml.Decode(data, &src)
	if err != nil {
		return fmt.Errorf("%s: %w", origin, err)
	}
	for _, k := range md.Undecoded() {
		// Scope sub-tables are invisible to the struct decode; decodeScopes
		// below both reads them and rejects the misspelled ones.
		if len(k) >= 3 && k[0] == "profile" {
			continue
		}
		return fmt.Errorf("%s: unknown key %q", origin, k.String())
	}
	if !allowSources && len(src.Sources) > 0 {
		return fmt.Errorf("%s: [source] tables are only read from %s, never from an overlay", origin, sourcesFile)
	}
	if err := decodeScopes(data, &src, origin); err != nil {
		return err
	}
	mergeInto(dst.Sources, src.Sources)
	mergeInto(dst.Packages, src.Packages)
	mergeInto(dst.Targets, src.Targets)
	mergeInto(dst.Profiles, src.Profiles)
	mergeInto(dst.Bundles, src.Bundles)
	mergeInto(dst.PyPackages, src.PyPackages)
	mergeInto(dst.Expect, src.Expect)
	return nil
}

func mergeInto[V any](dst, src map[string]V) {
	for k, v := range src {
		dst[k] = v
	}
}

// decodeScopes fills Profile.Scopes, which cannot be decoded directly: TOML
// folds [profile.nolto.python] into the parent table, so a second pass over an
// untyped decode is what separates a scope from a profile-wide value.
func decodeScopes(data string, cfg *Config, origin string) error {
	var raw struct {
		Profile map[string]map[string]any `toml:"profile"`
	}
	if _, err := toml.Decode(data, &raw); err != nil {
		return fmt.Errorf("%s: %w", origin, err)
	}
	for name, fields := range raw.Profile {
		scopes := map[string]Profile{}
		for key, val := range fields {
			sub, ok := val.(map[string]any)
			if !ok {
				continue
			}
			switch key {
			case ScopePython, ScopePyhost:
				p, err := profileFromTable(sub, origin, name, key)
				if err != nil {
					return err
				}
				scopes[key] = p
			case ScopeDeps:
				flat := map[string]any{}
				for k, v := range sub {
					pkg, ok := v.(map[string]any)
					if !ok {
						flat[k] = v
						continue
					}
					s := ScopeDeps + "." + k
					p, err := profileFromTable(pkg, origin, name, s)
					if err != nil {
						return err
					}
					scopes[s] = p
				}
				p, err := profileFromTable(flat, origin, name, ScopeDeps)
				if err != nil {
					return err
				}
				scopes[ScopeDeps] = p
			default:
				return fmt.Errorf("%s: profile %q: unknown scope %q (want %s, %s.<pkg>, %s or %s)",
					origin, name, key, ScopeDeps, ScopeDeps, ScopePython, ScopePyhost)
			}
		}
		if len(scopes) == 0 {
			continue
		}
		p := cfg.Profiles[name]
		p.Scopes = scopes
		cfg.Profiles[name] = p
	}
	return nil
}

func profileFromTable(t map[string]any, origin, profile, scope string) (Profile, error) {
	where := fmt.Sprintf("%s: profile %q scope %q", origin, profile, scope)
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(t); err != nil {
		return Profile{}, fmt.Errorf("%s: %w", where, err)
	}
	var p Profile
	md, err := toml.Decode(buf.String(), &p)
	if err != nil {
		return Profile{}, fmt.Errorf("%s: %w", where, err)
	}
	for _, k := range md.Undecoded() {
		return Profile{}, fmt.Errorf("%s: unknown key %q", where, k.String())
	}
	if p.Inherit != "" {
		return Profile{}, fmt.Errorf("%s: inherit belongs on the profile, not on a scope", where)
	}
	return p, nil
}

// OpenAsset reads a file under patches/, from whichever layer sources.toml came
// from. Names are slash-separated and relative to patches/, e.g.
// "python-3.13.13/ctypes_patch_1.py".
func (c *Config) OpenAsset(name string) ([]byte, error) {
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return nil, fmt.Errorf("asset %q: must stay under %s/", name, patchesDir)
	}
	root := c.Origin[patchesDir]
	if root == "" || root == OriginEmbedded {
		b, err := fs.ReadFile(embedded(), path.Join(patchesDir, clean))
		if err != nil {
			return nil, fmt.Errorf("embedded %s/%s: %w", patchesDir, clean, err)
		}
		return b, nil
	}
	p := filepath.Join(root, filepath.FromSlash(clean))
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return b, nil
}

// AssetDir is where a source's patches and edit fragments live, relative to
// patches/.
func AssetDir(s Source) string { return s.Name + "-" + s.Version }

// EditText is the content an Edit inserts, read from patches/<source>/ when the
// edit uses text_file.
func (c *Config) EditText(s Source, e Edit) (string, error) {
	if e.TextFile == "" {
		return e.Text, nil
	}
	b, err := c.OpenAsset(path.Join(AssetDir(s), e.TextFile))
	if err != nil {
		return "", err
	}
	return string(b), nil
}
