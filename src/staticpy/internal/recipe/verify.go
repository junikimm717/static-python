package recipe

import (
	"io/fs"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/ensure"
)

// Everything it does lives in internal/ensure; this is only where the
// expectations get resolved, because the runner is half their key and only
// the recipe layer knows which one this build will use.
func Verify(cfg *config.Config, _ fs.FS, target config.Target, profile, level string, dep core.Job, bundle string) (core.Job, error) {
	lvl, err := ensure.ParseLevel(level)
	if err != nil {
		return nil, err
	}
	src, err := pythonSource(cfg)
	if err != nil {
		return nil, err
	}
	runner := ensure.RunnerQemu
	if ensure.IsNativeTarget(target) {
		runner = ensure.RunnerNative
	}
	res, err := resolveScope(cfg, profile, config.ScopePython)
	if err != nil {
		return nil, err
	}
	expect := ensure.LookupExpect(cfg.Expect, target.Triple, runner, !res.HostBuilt())
	opts := ensure.Options{
		WantVersion: src.Version,
		WantDynamic: res.HostBuilt(),
		Bundle:      bundle,
	}
	plan, err := planBundle(cfg, bundle)
	if err != nil {
		return nil, err
	}
	if len(plan.Imports) > 0 {
		opts.Modules = append(append([]string{}, ensure.ProbeModules...), plan.Imports...)
	}
	return ensure.NewJob(dep, target, profile, lvl, expect, opts), nil
}
