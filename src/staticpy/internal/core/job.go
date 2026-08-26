package core

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// keyDoc is the canonical pre-image of a job key. Field order is fixed by the
// struct; map keys are sorted by encoding/json, so the bytes are deterministic
// regardless of Go map iteration order.
type keyDoc struct {
	Name   string            `json:"name"`
	Slug   string            `json:"slug"`
	Inputs map[string]string `json:"inputs"`
	Deps   map[string]string `json:"deps"`
}

var keyCache sync.Map // slug -> key

// Key is the Merkle hash of a job: its own inputs plus the keys of all its
// dependencies. It is memoized per process by slug, so a slug must uniquely
// determine a job's content within one process.
func Key(j Job) (string, error) {
	if v, ok := keyCache.Load(j.Slug()); ok {
		return v.(string), nil
	}
	return computeKey(j, &keyWalk{memo: map[string]string{}, onStack: map[string]bool{}, cache: true})
}

type keyWalk struct {
	memo    map[string]string
	onStack map[string]bool
	path    []string
	cache   bool
}

func computeKey(j Job, w *keyWalk) (string, error) {
	slug := j.Slug()
	if slug == "" || strings.ContainsAny(slug, "/\\\x00") {
		return "", fmt.Errorf("core: invalid job slug %q", slug)
	}
	if k, ok := w.memo[slug]; ok {
		return k, nil
	}
	if w.cache {
		if v, ok := keyCache.Load(slug); ok {
			w.memo[slug] = v.(string)
			return v.(string), nil
		}
	}
	if w.onStack[slug] {
		return "", fmt.Errorf("core: dependency cycle: %s", strings.Join(append(w.path, slug), " -> "))
	}
	w.onStack[slug] = true
	w.path = append(w.path, slug)
	defer func() {
		delete(w.onStack, slug)
		w.path = w.path[:len(w.path)-1]
	}()

	deps := map[string]string{}
	for _, d := range j.Deps() {
		dk, err := computeKey(d, w)
		if err != nil {
			return "", err
		}
		if prev, ok := deps[d.Slug()]; ok && prev != dk {
			return "", fmt.Errorf("core: job %s lists dep slug %s twice with different keys", slug, d.Slug())
		}
		deps[d.Slug()] = dk
	}
	inputs := j.KeyInputs()
	if inputs == nil {
		inputs = map[string]string{}
	}
	buf, err := canonicalJSON(keyDoc{Name: j.Name(), Slug: slug, Inputs: inputs, Deps: deps})
	if err != nil {
		return "", fmt.Errorf("core: encode key doc for %s: %w", slug, err)
	}
	sum := sha256.Sum256(buf)
	k := hex.EncodeToString(sum[:])
	w.memo[slug] = k
	if w.cache {
		keyCache.Store(slug, k)
	}
	return k, nil
}

func canonicalJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func ReadManifest(dir string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Join(dir, ManifestName), err)
	}
	return &m, nil
}

// IsValid reports whether the job's artifact exists and was built from exactly
// this key.
func IsValid(e *Env, j Job) (bool, error) {
	k, err := Key(j)
	if err != nil {
		return false, err
	}
	return validAt(e, j.ArtifactDir(e), k), nil
}

func validAt(e *Env, dir, key string) bool {
	m, err := ReadManifest(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			e.Log.Debug("artifact unreadable, treating as missing", "dir", dir, "err", err)
		}
		return false
	}
	if m.Key != key {
		e.Log.Debug("artifact key mismatch", "dir", dir, "have", short(m.Key), "want", short(key))
		return false
	}
	return true
}

func short(key string) string {
	if len(key) > 12 {
		return key[:12]
	}
	return key
}

func writeManifest(dir string, m *Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, ManifestName))
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// node is a resolved DAG vertex: one job, deduped by slug, with its key.
type node struct {
	job  Job
	slug string
	key  string
	deps []*node

	done chan struct{}
	err  error
}

// resolve flattens the DAG rooted at jobs: dedupes by slug, detects cycles and
// slug collisions, computes every key, and returns nodes in dependency-first
// topological order.
func resolve(jobs []Job) ([]*node, error) {
	byslug := map[string]*node{}
	var order []*node
	onStack := map[string]bool{}
	var path []string

	var visit func(Job) (*node, error)
	visit = func(j Job) (*node, error) {
		slug := j.Slug()
		if n, ok := byslug[slug]; ok {
			if err := sameJob(n.job, j); err != nil {
				return nil, err
			}
			return n, nil
		}
		if onStack[slug] {
			return nil, fmt.Errorf("core: dependency cycle: %s", strings.Join(append(path, slug), " -> "))
		}
		onStack[slug] = true
		path = append(path, slug)
		n := &node{job: j, slug: slug, done: make(chan struct{})}
		seen := map[string]bool{}
		for _, d := range j.Deps() {
			if d == nil {
				return nil, fmt.Errorf("core: job %s has a nil dep", slug)
			}
			dn, err := visit(d)
			if err != nil {
				return nil, err
			}
			if seen[dn.slug] {
				continue
			}
			seen[dn.slug] = true
			n.deps = append(n.deps, dn)
		}
		onStack[slug] = false
		path = path[:len(path)-1]
		k, err := Key(j)
		if err != nil {
			return nil, err
		}
		n.key = k
		sort.Slice(n.deps, func(a, b int) bool { return n.deps[a].slug < n.deps[b].slug })
		byslug[slug] = n
		order = append(order, n)
		return n, nil
	}

	for _, j := range jobs {
		if j == nil {
			return nil, fmt.Errorf("core: nil job in root set")
		}
		if _, err := visit(j); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// sameJob guards against two different recipes claiming one slug, which would
// silently make them share an artifact directory.
func sameJob(a, b Job) error {
	if a == b {
		return nil
	}
	fa, err := jobFingerprint(a)
	if err != nil {
		return err
	}
	fb, err := jobFingerprint(b)
	if err != nil {
		return err
	}
	if fa != fb {
		return fmt.Errorf("core: two different jobs share slug %q:\n  %s\n  %s", a.Slug(), fa, fb)
	}
	return nil
}

func jobFingerprint(j Job) (string, error) {
	var deps []string
	for _, d := range j.Deps() {
		deps = append(deps, d.Slug())
	}
	sort.Strings(deps)
	b, err := canonicalJSON(keyDoc{
		Name:   j.Name(),
		Slug:   j.Slug(),
		Inputs: j.KeyInputs(),
		Deps:   map[string]string{"_deps": strings.Join(deps, ",")},
	})
	return strings.TrimSpace(string(b)), err
}
