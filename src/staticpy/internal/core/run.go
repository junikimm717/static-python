package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// errDepChanged means a dependency was republished with a different key while
// we were waiting for its read lock; the job's own key is therefore stale and
// the whole attempt must restart.
var errDepChanged = errors.New("core: dependency changed under us")

var errSkipped = errors.New("skipped: dependency failed")

// trashWG tracks background deletions of replaced artifacts so Run can wait
// for a quiet filesystem before returning.
var trashWG sync.WaitGroup

// Provenancer is implemented by jobs that took an input on trust rather than
// verifying it. What it returns lands in the manifest, so a build that took the
// weaker path never looks identical to one that did not.
type Provenancer interface {
	Provenance() map[string]string
}

// It reports each node's state in dependency-first order.
func Plan(e *Env, jobs []Job) ([]PlanNode, error) {
	nodes, err := resolve(jobs)
	if err != nil {
		return nil, err
	}
	out := make([]PlanNode, 0, len(nodes))
	for _, n := range nodes {
		hb, _ := ReadHeartbeat(e, n.slug)
		out = append(out, PlanNode{
			Job:      n.job,
			Key:      n.key,
			Valid:    validAt(e, n.job.ArtifactDir(e), n.key),
			Building: hb.Live(),
		})
	}
	return out, nil
}

// Nodes are deduped by slug, built in dependency order with up to
// e.MaxWorkers concurrency, and skipped entirely when their artifact already
// matches their key. The first failure cancels the rest and is returned
// wrapped with the failing job's slug.
func Run(ctx context.Context, e *Env, jobs []Job) error {
	if err := e.EnsureDirs(); err != nil {
		return err
	}
	nodes, err := resolve(jobs)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, e.Workers())
	var mu sync.Mutex
	var firstErr error
	fail := func(n *node, err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = fmt.Errorf("job %s failed: %w", n.slug, err)
			cancel()
		}
	}

	var wg sync.WaitGroup
	for _, n := range nodes {
		wg.Add(1)
		go func(n *node) {
			defer wg.Done()
			defer close(n.done)
			for _, d := range n.deps {
				<-d.done
				if d.err != nil {
					n.err = fmt.Errorf("%w (%s)", errSkipped, d.slug)
					return
				}
			}
			if runCtx.Err() != nil {
				n.err = runCtx.Err()
				return
			}
			select {
			case sem <- struct{}{}:
			case <-runCtx.Done():
				n.err = runCtx.Err()
				return
			}
			defer func() { <-sem }()

			if err := buildNode(runCtx, e, n); err != nil {
				n.err = err
				if !errors.Is(err, context.Canceled) {
					fail(n, err)
				}
			}
		}(n)
	}
	wg.Wait()
	trashWG.Wait()

	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// It retries if a dependency is republished out from under us between our
// validity check and taking its read lock.
func buildNode(ctx context.Context, e *Env, n *node) error {
	const attempts = 3
	for i := 1; ; i++ {
		err := tryBuild(ctx, e, n)
		if errors.Is(err, errDepChanged) && i < attempts {
			e.Log.Warn("dependency changed, retrying", "job", n.slug, "attempt", i)
			continue
		}
		if errors.Is(err, errDepChanged) {
			return fmt.Errorf("dependencies kept changing after %d attempts (another build is fighting us)", attempts)
		}
		return err
	}
}

func tryBuild(ctx context.Context, e *Env, n *node) error {
	artifact := n.job.ArtifactDir(e)
	if validAt(e, artifact, n.key) {
		e.Log.Info("up to date", "job", n.slug, "key", short(n.key))
		return nil
	}

	// Both an in-process mutex and flock: the mutex keeps sibling goroutines
	// from spinning on the lock file, flock covers other processes.
	mu := slugMutex(n.slug)
	mu.Lock()
	defer mu.Unlock()

	ex, err := acquire(ctx, e, n.slug, true)
	if err != nil {
		return err
	}
	defer ex.release()

	// Someone may have built it while we waited.
	if validAt(e, artifact, n.key) {
		e.Log.Info("up to date (built by another worker)", "job", n.slug, "key", short(n.key))
		return nil
	}

	var held leases
	defer func() { held.release() }()
	for _, d := range n.deps { // n.deps is sorted by slug
		sh, err := acquire(ctx, e, d.slug, false)
		if err != nil {
			return err
		}
		held = append(held, sh)
	}
	for _, d := range n.deps {
		if !validAt(e, d.job.ArtifactDir(e), d.key) {
			return fmt.Errorf("%w: %s", errDepChanged, d.slug)
		}
	}

	r, err := NewRunner(e, n.slug)
	if err != nil {
		return err
	}
	defer r.Close()

	started := time.Now()
	hb := startHeartbeat(e, n, r, started.UTC())
	defer hb.close(e, n.slug)

	work := e.Path(DirWork, scratchName(n.slug))
	stage := e.Path(DirStaging, scratchName(n.slug))
	for _, d := range []string{work, stage} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	e.Log.Info("building", "job", n.slug, "key", short(n.key), "logs", r.Dir())
	err = n.job.Build(ctx, e, r, work, stage)
	if err != nil {
		os.RemoveAll(stage)
		e.Log.Error("build failed", "job", n.slug, "work", work, "logs", r.Dir())
		return err
	}

	dur := time.Since(started)
	if err := stampManifest(e, n, stage, dur); err != nil {
		os.RemoveAll(stage)
		return err
	}
	if err := publish(e, stage, artifact); err != nil {
		os.RemoveAll(stage)
		return err
	}
	if e.KeepWork || os.Getenv("STATICPY_KEEP_WORK") != "" {
		e.Log.Info("keeping work tree", "job", n.slug, "work", work)
	} else {
		os.RemoveAll(work)
	}
	r.Step("done")
	e.Log.Info("built", "job", n.slug, "key", short(n.key), "duration", dur.Round(time.Millisecond))
	return nil
}

// It is the last thing written, so a directory carrying a manifest is by
// construction complete.
func stampManifest(e *Env, n *node, stage string, dur time.Duration) error {
	host, _ := os.Hostname()
	deps := map[string]string{}
	for _, d := range n.deps {
		deps[d.slug] = d.key
	}
	var prov map[string]string
	if p, ok := n.job.(Provenancer); ok {
		if got := p.Provenance(); len(got) > 0 {
			prov = make(map[string]string, len(got))
			for k, v := range got {
				prov[k] = v
			}
		}
	}
	return writeManifest(stage, &Manifest{
		Key:         n.key,
		Slug:        n.slug,
		Name:        n.job.Name(),
		Inputs:      n.job.KeyInputs(),
		Deps:        deps,
		CompletedAt: time.Now().UTC(),
		BuiltBy:     fmt.Sprintf("%s:%d", host, os.Getpid()),
		Duration:    dur.Round(time.Millisecond).String(),
		Provenance:  prov,
	})
}

// publish swaps stage into place with renames only, so readers see either the
// old artifact or the new one and never a half-populated directory. Caller
// must hold the job's exclusive lock.
func publish(e *Env, stage, artifact string) error {
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(artifact); err == nil {
		trash := e.Path(DirTrash, fmt.Sprintf("%s.%d.%s", filepath.Base(artifact), os.Getpid(), randHex(4)))
		if err := os.Rename(artifact, trash); err != nil {
			return fmt.Errorf("move old artifact aside: %w", err)
		}
		trashWG.Add(1)
		go func() {
			defer trashWG.Done()
			os.RemoveAll(trash)
		}()
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stage, artifact); err != nil {
		return fmt.Errorf("publish %s: %w", artifact, err)
	}
	return nil
}
