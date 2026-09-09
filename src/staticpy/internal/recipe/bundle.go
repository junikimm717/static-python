package recipe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/gen"
	"github.com/junikimm717/static-python/src/staticpy/internal/sources"
)

// bundlePlan is the resolved [bundle.X]: packages, rewritten C modules, smoke
// imports, and the srctree jobs that fetch each sdist.
type bundlePlan struct {
	Name     string
	Packages []config.PyPackage
	Modules  []config.PyModule
	Imports  []string
	Trees    []core.Job
	Files    []gen.BundleFile
}

func planBundle(cfg *config.Config, name string) (*bundlePlan, error) {
	if name == "" {
		return &bundlePlan{}, nil
	}
	b, ok := cfg.Bundles[name]
	if !ok {
		return nil, fmt.Errorf("recipe: unknown bundle %q (have %s)", name, strings.Join(sortedKeys(cfg.Bundles), ", "))
	}
	p := &bundlePlan{Name: name}
	seenImport := map[string]bool{}
	for _, pkgName := range b.Packages {
		pkg, ok := cfg.PyPackages[pkgName]
		if !ok {
			return nil, fmt.Errorf("recipe: bundle %q names package %q, which no [pkg.*] table declares", name, pkgName)
		}
		p.Packages = append(p.Packages, pkg)
		p.Trees = append(p.Trees, sources.SrcTree(pkg.AsSource(), sources.Options{}))
		for _, m := range pkg.Modules {
			rewritten, files, err := gen.RewriteModule(pkg.Name, m)
			if err != nil {
				return nil, err
			}
			p.Modules = append(p.Modules, rewritten)
			p.Files = append(p.Files, files...)
		}
		for _, imp := range pkg.ImportNames() {
			if seenImport[imp] {
				continue
			}
			seenImport[imp] = true
			p.Imports = append(p.Imports, imp)
		}
	}
	return p, nil
}

func withBundle(slug, bundle string) string {
	if bundle == "" {
		return slug
	}
	return slug + ":" + bundle
}

func (p *bundlePlan) specHash() string {
	if p == nil || p.Name == "" {
		return ""
	}
	type spec struct {
		Name      string   `json:"name"`
		Version   string   `json:"version"`
		SHA256    string   `json:"sha256"`
		PurePaths []string `json:"pure_paths"`
		Imports   []string `json:"imports"`
		Modules   []string `json:"modules"`
		Wrappers  []string `json:"wrappers"`
	}
	var docs []spec
	for _, pkg := range p.Packages {
		var mods []string
		for _, m := range pkg.Modules {
			mods = append(mods, m.Name+"\x00"+strings.Join(m.Sources, "\x00")+"\x00"+m.Init)
		}
		var wraps []string
		for _, f := range p.Files {
			if strings.HasPrefix(f.Path, "_bundle/"+pkg.Name+"/") {
				wraps = append(wraps, f.Path+"\x00"+string(f.Content))
			}
		}
		docs = append(docs, spec{
			Name: pkg.Name, Version: pkg.Version, SHA256: pkg.SdistSHA256,
			PurePaths: pkg.PurePaths, Imports: pkg.ImportNames(), Modules: mods,
			Wrappers: wraps,
		})
	}
	b, err := json.Marshal(docs)
	if err != nil {
		return err.Error()
	}
	return hashBytes(b)
}

func stageBundle(cpythonSrc string, e *core.Env, p *bundlePlan) error {
	if p == nil || p.Name == "" {
		return nil
	}
	for i, pkg := range p.Packages {
		dst := filepath.Join(cpythonSrc, "Modules", "_bundle", pkg.Name)
		if err := copyTree(p.Trees[i].ArtifactDir(e), dst); err != nil {
			return fmt.Errorf("recipe: staging bundle package %s: %w", pkg.Name, err)
		}
		if err := os.Remove(filepath.Join(dst, core.ManifestName)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, f := range p.Files {
		dst := filepath.Join(cpythonSrc, "Modules", filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, f.Content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func installBundle(stage, abi string, e *core.Env, p *bundlePlan) error {
	if p == nil || p.Name == "" {
		return nil
	}
	site := filepath.Join(stage, "lib", "python"+abi, "site-packages")
	if err := os.MkdirAll(site, 0o755); err != nil {
		return err
	}
	for i, pkg := range p.Packages {
		tree := p.Trees[i].ArtifactDir(e)
		for _, rel := range pkg.PurePaths {
			if err := installPurePath(tree, site, rel); err != nil {
				return fmt.Errorf("recipe: bundle %s: %w", pkg.Name, err)
			}
		}
	}
	shims, err := gen.Shims(p.Modules)
	if err != nil {
		return err
	}
	return gen.WriteShims(site, shims)
}

func installPurePath(tree, site, rel string) error {
	from := filepath.Join(tree, filepath.FromSlash(rel))
	to := filepath.Join(site, filepath.Base(rel))
	st, err := os.Stat(from)
	if err != nil {
		return fmt.Errorf("pure path %s: %w", rel, err)
	}
	if st.IsDir() {
		return copyTree(from, to)
	}
	return copyFile(from, to, st.Mode().Perm())
}

func pythonABIFromVersion(version string) (string, error) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return "", fmt.Errorf("recipe: python version %q has no major.minor", version)
	}
	return parts[0] + "." + parts[1], nil
}
