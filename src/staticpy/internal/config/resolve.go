package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// deps.<pkg> layers on top of deps.
const (
	ScopeDeps   = "deps"
	ScopePython = "python"
	ScopePyhost = "pyhost"
)

// Resolve flattens a profile for one scope. It walks Inherit from the root
// down, and applies each profile's own values before that profile's scope
// layers, so a child profile fully overrides its parent rather than being
// overridden by the parent's more specific scope.
func (c *Config) Resolve(profileName, scope string) (Resolved, error) {
	chain, err := c.chain(profileName)
	if err != nil {
		return Resolved{}, err
	}
	layers, err := scopeLayers(scope)
	if err != nil {
		return Resolved{}, err
	}
	r := Resolved{Profile: profileName, Scope: scope}
	for _, name := range chain {
		p := c.Profiles[name]
		if err := apply(&r, p, fmt.Sprintf("profile %q", name)); err != nil {
			return Resolved{}, err
		}
		for _, s := range layers {
			sp, ok := p.Scopes[s]
			if !ok {
				continue
			}
			where := fmt.Sprintf("profile %q scope %q", name, s)
			if err := apply(&r, sp, where); err != nil {
				return Resolved{}, err
			}
		}
	}
	return r, nil
}

// chain lists profileName's ancestors root-first.
func (c *Config) chain(profileName string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for name := profileName; name != ""; {
		if seen[name] {
			return nil, fmt.Errorf("profile inheritance cycle: %s -> %s",
				strings.Join(out, " -> "), name)
		}
		p, ok := c.Profiles[name]
		if !ok {
			if name == profileName {
				return nil, fmt.Errorf("unknown profile %q (have %s)", name, c.profileNames())
			}
			return nil, fmt.Errorf("profile %q inherits %q, which is not defined (have %s)",
				out[len(out)-1], name, c.profileNames())
		}
		seen[name] = true
		out = append(out, name)
		name = p.Inherit
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (c *Config) profileNames() string {
	names := make([]string, 0, len(c.Profiles))
	for n := range c.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func scopeLayers(scope string) ([]string, error) {
	switch {
	case scope == ScopeDeps, scope == ScopePython, scope == ScopePyhost:
		return []string{scope}, nil
	case strings.HasPrefix(scope, ScopeDeps+"."):
		if strings.TrimPrefix(scope, ScopeDeps+".") == "" {
			return nil, fmt.Errorf("scope %q: names no package", scope)
		}
		return []string{ScopeDeps, scope}, nil
	}
	return nil, fmt.Errorf("unknown scope %q (want %s, %s.<pkg>, %s or %s)",
		scope, ScopeDeps, ScopeDeps, ScopePython, ScopePyhost)
}

// apply layers one Profile onto r: whole-list replacements first, then removals
// against what was inherited, then appends.
func apply(r *Resolved, p Profile, where string) error {
	if p.CFlags != nil {
		r.CFlags = append([]string(nil), p.CFlags...)
	}
	if p.CXXFlags != nil {
		r.CXXFlags = append([]string(nil), p.CXXFlags...)
	}
	if p.LDFlags != nil {
		r.LDFlags = append([]string(nil), p.LDFlags...)
	}

	var err error
	if r.CFlags, err = remove(r.CFlags, p.CFlagsRemove, where, "cflags"); err != nil {
		return err
	}
	if r.LDFlags, err = remove(r.LDFlags, p.LDFlagsRemove, where, "ldflags"); err != nil {
		return err
	}
	r.CFlags = append(r.CFlags, p.CFlagsAdd...)
	r.LDFlags = append(r.LDFlags, p.LDFlagsAdd...)

	if p.Strip != nil {
		r.Strip = *p.Strip
	}
	if p.Debug != nil {
		r.Debug = *p.Debug
	}
	if p.TestModules != nil {
		r.TestModules = *p.TestModules
	}
	if p.PGO != "" {
		r.PGO = p.PGO
	}
	if p.ProfileTask != "" {
		r.ProfileTask = p.ProfileTask
	}
	if p.Modules != "" {
		r.Modules = p.Modules
	}
	if p.Bundle != "" {
		r.Bundle = p.Bundle
	}
	return nil
}

// remove drops every exact match of each entry in drop. A removal that matches
// nothing is an error: a misspelled *_remove that quietly does nothing leaves a
// flag in the build while the config says it is gone.
func remove(list, drop []string, where, field string) ([]string, error) {
	for _, d := range drop {
		out := list[:0:0]
		hit := false
		for _, v := range list {
			if v == d {
				hit = true
				continue
			}
			out = append(out, v)
		}
		if !hit {
			return nil, fmt.Errorf("%s: %s_remove %q matches nothing in %v", where, field, d, list)
		}
		list = out
	}
	return list, nil
}

// Neither the profile name nor the scope is included: two configurations that
// resolve to the same flags describe the same artifact and must share it.
func (r Resolved) keyInputs() map[string]string {
	return map[string]string{
		"cflags":       strings.Join(r.CFlags, " "),
		"cxxflags":     strings.Join(r.CXXFlags, " "),
		"ldflags":      strings.Join(r.LDFlags, " "),
		"strip":        strconv.FormatBool(r.Strip),
		"debug":        strconv.FormatBool(r.Debug),
		"pgo":          r.PGO,
		"profile_task": r.ProfileTask,
		"test_modules": strconv.FormatBool(r.TestModules),
		"modules":      r.Modules,
		"bundle":       r.Bundle,
	}
}
