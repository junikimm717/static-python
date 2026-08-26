// Package recipe turns pinned sources into a static Python.
//
// Job families, and the DAG:
//
//	srctree:<pkg>-<ver>        extracted + patched source   (internal/sources)
//	probe:<T>                  target ABI sizes -> config.site
//	dep:<prof>:<T>:<pkg>       one native library, own prefix
//	sysroot:<prof>:<T>         the -I/-L view composed from dep artifacts
//	pyhost:<ver>               static-musl CPython that runs on the build machine
//	pynative:<prof>:<T>        the shipped interpreter, host == target
//	pycross:<prof>:<H>:<T>     the shipped interpreter, host != target
//	pack:<prof>:<T>            the distributable tarball
//
// pycross depends on pyhost, never on pynative: a cross build needs a runnable
// same-version interpreter to freeze bytecode, and that is all it needs. Making
// it wait on a full PGO release build of the host is why cross-compiling one
// target used to be gated on an hour of unrelated work.
//
// This file is the seam the CLI plans against; the constructors it calls live
// in the sibling files.
package recipe

import (
	"fmt"
	"io/fs"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// Version is the recipe generation. Bump it by hand when the *procedure*
// changes in a way the configure flags do not capture — a new step, a different
// ordering, a changed install layout. Every job key includes it, so bumping it
// rebuilds the world.
const Version = 1

type PlanOptions struct {
	Profile string
	// Host is the build machine's triple. Targets equal to it build native.
	Host    string
	Targets []string
	Bundle  string

	Verify string // "", "smoke", "core", "full"
	Pack   bool
}

// The CLI never constructs a job itself, so the shape of the graph is decided
// in exactly one place.
func Plan(cfg *config.Config, assets fs.FS, o PlanOptions) ([]core.Job, error) {
	if o.Profile == "" {
		o.Profile = "default"
	}
	if o.Host == "" {
		return nil, fmt.Errorf("recipe: no host triple; the CLI resolves it from the build machine")
	}
	host, ok := cfg.Targets[o.Host]
	if !ok {
		return nil, fmt.Errorf("recipe: host %q is not in targets.toml", o.Host)
	}
	targets := o.Targets
	if len(targets) == 0 {
		targets = []string{o.Host}
	}

	var jobs []core.Job
	for _, name := range targets {
		t, ok := cfg.Targets[name]
		if !ok {
			return nil, fmt.Errorf("recipe: target %q is not in targets.toml", name)
		}
		interp, err := Interpreter(cfg, assets, host, t, o.Profile, o.Bundle)
		if err != nil {
			return nil, err
		}
		final := interp
		if o.Verify != "" {
			if final, err = Verify(cfg, assets, t, o.Profile, o.Verify, interp); err != nil {
				return nil, err
			}
		}
		if o.Pack {
			if final, err = Pack(cfg, t, o.Profile, interp, final); err != nil {
				return nil, err
			}
		}
		jobs = append(jobs, final)
	}
	return jobs, nil
}

// No caller has to know which shape it is asking for.
func Interpreter(cfg *config.Config, assets fs.FS, host, target config.Target, profile, bundle string) (core.Job, error) {
	if host.Triple == target.Triple {
		return PyNative(cfg, assets, target, profile, bundle)
	}
	return PyCross(cfg, assets, host, target, profile, bundle)
}
