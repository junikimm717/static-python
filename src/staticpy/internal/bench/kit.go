package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// KitDoc is kit.json: the lineup a quiet machine measures without a checkout.
type KitDoc struct {
	Protocol      int      `json:"protocol"`
	KitVersion    string   `json:"kit_version"`
	GitRevision   string   `json:"git_revision,omitempty"`
	PythonVersion string   `json:"python_version"`
	Triple        string   `json:"triple"`
	Baseline      string   `json:"baseline"`
	Suite         string   `json:"suite,omitempty"`
	Pins          Pins     `json:"pins"`
	Arms          []KitArm `json:"arms"`
}

type KitArm struct {
	Label        string   `json:"label"`
	Path         string   `json:"path"`
	ArtifactKey  string   `json:"artifact_key,omitempty"`
	BinarySHA256 string   `json:"binary_sha256,omitempty"`
	Factors      *Factors `json:"factors,omitempty"`
}

// LoadKit reads kit.json from dir. dir is the unpacked top directory
// (the one that contains run and python/).
func LoadKit(dir string) (*KitDoc, error) {
	path := filepath.Join(dir, "kit.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("kit: %w", err)
	}
	var doc KitDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("kit: %s: %w", path, err)
	}
	if doc.Protocol != Protocol {
		return nil, fmt.Errorf("kit: protocol %d, this runner speaks %d", doc.Protocol, Protocol)
	}
	if len(doc.Arms) == 0 {
		return nil, fmt.Errorf("kit: %s names no arms", path)
	}
	if doc.Baseline == "" {
		return nil, fmt.Errorf("kit: %s has no baseline", path)
	}
	return &doc, nil
}

// ResolveArms turns kit-relative paths into absolute interpreter binaries.
func (k *KitDoc) ResolveArms(root string) (order []string, paths map[string]string, err error) {
	paths = map[string]string{}
	seen := map[string]bool{}
	for _, a := range k.Arms {
		if a.Label == "" || a.Path == "" {
			return nil, nil, fmt.Errorf("kit: arm missing label or path")
		}
		if seen[a.Label] {
			return nil, nil, fmt.Errorf("kit: arm %q listed twice", a.Label)
		}
		seen[a.Label] = true
		p := a.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, filepath.FromSlash(a.Path))
		}
		order = append(order, a.Label)
		paths[a.Label] = p
	}
	if !seen[k.Baseline] {
		return nil, nil, fmt.Errorf("kit: baseline %q is not in arms", k.Baseline)
	}
	return order, paths, nil
}

func (k *KitDoc) MatchesThisMachine() error {
	if k.Triple == "" {
		return nil
	}
	arch := runtime.GOARCH
	want, ok := goarchOfTriple(k.Triple)
	if !ok {
		return nil
	}
	if arch != want {
		return fmt.Errorf("kit is for %s (GOARCH %s); this binary is %s", k.Triple, want, arch)
	}
	return nil
}

func goarchOfTriple(triple string) (string, bool) {
	switch {
	case strings.HasPrefix(triple, "x86_64-"):
		return "amd64", true
	case strings.HasPrefix(triple, "aarch64-"):
		return "arm64", true
	case strings.HasPrefix(triple, "i386-") || strings.HasPrefix(triple, "i686-"):
		return "386", true
	case strings.HasPrefix(triple, "riscv64-"):
		return "riscv64", true
	case strings.HasPrefix(triple, "powerpc64le-"):
		return "ppc64le", true
	case strings.HasPrefix(triple, "powerpc64-"):
		return "ppc64", true
	case strings.HasPrefix(triple, "s390x-"):
		return "s390x", true
	default:
		return "", false
	}
}
