package cli

import (
	"context"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/bench"
	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/hostcc"
	"github.com/junikimm717/static-python/src/staticpy/internal/recipe"
)

func profileForInterp(g *Global, cfg *config.Config, label string) string {
	switch label {
	case "static":
		return g.Profile
	case "reference":
		return config.ProfileReference
	case "system":
		return ""
	}
	if cfg != nil {
		if _, ok := cfg.Profiles[label]; ok {
			return label
		}
	}
	return ""
}

func enrichIdentity(g *Global, cfg *config.Config, e *core.Env, id *bench.Identity) {
	profile := profileForInterp(g, cfg, id.Label)
	elf := ""
	if id.Factors != nil {
		elf = id.Factors.Linkage
	}
	if profile == "" {
		return
	}
	opts, err := factorOpts(g, cfg, e, profile, elf)
	if err != nil {
		if id.Factors == nil {
			id.Factors = &bench.Factors{Linkage: elf}
		}
		return
	}
	f := bench.DeriveFactors(opts)
	id.Factors = &f
}

func factorOpts(g *Global, cfg *config.Config, e *core.Env, profile, elf string) (bench.FactorOpts, error) {
	py, err := cfg.Resolve(profile, config.ScopePython)
	if err != nil {
		return bench.FactorOpts{}, err
	}
	skip, err := cfg.PackageSkipped("mimalloc", profile)
	if err != nil {
		return bench.FactorOpts{}, err
	}
	host, _ := g.HostTriple(cfg)
	o := bench.FactorOpts{
		HostBuilt:       py.HostBuilt(),
		LTOMode:         py.LTOMode,
		PythonCFlags:    py.CFlags,
		WithLTO:         !py.LTOSet || py.LTO,
		MimallocSkipped: skip,
		Libc:            libcFactor(cfg, host, py.HostBuilt()),
		PGO:             py.PGO,
		ELFLinkage:      elf,
		Toolchain:       toolchainFactor(e, py, host),
	}
	return o, nil
}

func libcFactor(cfg *config.Config, host string, hostBuilt bool) string {
	if hostBuilt {
		return hostLibcName()
	}
	if t, ok := cfg.Targets[host]; ok && t.ABI != "" {
		return t.ABI
	}
	return "musl"
}

func hostLibcName() string {
	cc, err := hostcc.Find()
	if err != nil {
		return "glibc"
	}
	id, err := hostcc.Identify(context.Background(), cc)
	if err != nil {
		return "glibc"
	}
	if strings.HasPrefix(id.Libc, "musl") {
		return "musl"
	}
	return "glibc"
}

func toolchainFactor(e *core.Env, py config.Resolved, host string) string {
	if e == nil {
		return ""
	}
	recipe.Bind(e)
	triple := host
	if triple == "" {
		triple = e.Host
	}
	id, err := recipeToolchain(py, triple)
	if err != nil {
		return ""
	}
	return id.Factor()
}

func recipeToolchain(py config.Resolved, triple string) (recipe.ToolchainID, error) {
	if py.HostBuilt() {
		return recipe.ToolchainHost(context.Background())
	}
	return recipe.Toolchain(nil, triple)
}
