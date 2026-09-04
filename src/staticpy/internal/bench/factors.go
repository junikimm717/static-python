package bench

import (
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

const (
	LTOWholeGraph = "whole-graph"
	LTOPerDep     = "per-dep"
	LTONone       = "none"

	AllocMimalloc = "mimalloc"
	AllocMusl     = "musl"
	AllocGlibc    = "glibc"
)

// Factors are the axes a profile name is too coarse to record. "default"
// can grow a different LTO strategy next month; a later reader has to see
// what this binary actually was.
type Factors struct {
	Linkage   string `json:"linkage"`
	Libc      string `json:"libc,omitempty"`
	LTO       string `json:"lto,omitempty"`
	Allocator string `json:"allocator,omitempty"`
	PGO       bool   `json:"pgo"`
	Toolchain string `json:"toolchain,omitempty"`
}

// FactorOpts is the resolved profile shape DeriveFactors reads. The
// profile name is deliberately not an input.
type FactorOpts struct {
	HostBuilt       bool
	LTOMode         string
	PythonCFlags    []string
	WithLTO         bool
	MimallocSkipped bool
	Libc            string
	PGO             string
	Toolchain       string
	ELFLinkage      string
}

func DeriveFactors(o FactorOpts) Factors {
	f := Factors{
		Linkage:   o.ELFLinkage,
		Libc:      o.Libc,
		Allocator: allocatorOf(o),
		PGO:       o.PGO == "on" || o.PGO == "native-only",
		Toolchain: o.Toolchain,
	}
	if f.Linkage == "" {
		if o.HostBuilt {
			f.Linkage = "dynamic"
		} else {
			f.Linkage = "static"
		}
	}
	f.LTO = ltoOf(o)
	return f
}

func ltoOf(o FactorOpts) string {
	if o.LTOMode == config.LTOModePerDep {
		return LTOPerDep
	}
	if o.HostBuilt {
		if o.WithLTO {
			return LTOWholeGraph
		}
		return LTONone
	}
	if hasLTOFlag(o.PythonCFlags) {
		return LTOWholeGraph
	}
	return LTONone
}

func allocatorOf(o FactorOpts) string {
	if !o.MimallocSkipped {
		return AllocMimalloc
	}
	if o.HostBuilt {
		if o.Libc == "musl" {
			return AllocMusl
		}
		return AllocGlibc
	}
	return AllocMusl
}

func hasLTOFlag(flags []string) bool {
	for _, f := range flags {
		if f == "-flto" || strings.HasPrefix(f, "-flto=") {
			return true
		}
	}
	return false
}
