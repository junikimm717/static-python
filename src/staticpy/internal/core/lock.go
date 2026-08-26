package core

import (
	"context"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
)

// flock is per open-file-description, so the *os.File must stay open for the
// whole lease. Two opens in the same process contend with each other, which is
// what makes the shared-read discipline work across goroutines too; the
// per-slug mutex on top makes exclusive intent explicit and avoids spinning.
type flockLease struct {
	f    *os.File
	path string
	excl bool
}

var (
	slugMuMu sync.Mutex
	slugMus  = map[string]*sync.Mutex{}
)

func slugMutex(slug string) *sync.Mutex {
	slugMuMu.Lock()
	defer slugMuMu.Unlock()
	m, ok := slugMus[slug]
	if !ok {
		m = &sync.Mutex{}
		slugMus[slug] = m
	}
	return m
}

// acquire takes a shared or exclusive flock on dist/locks/<slug>.lock. It
// polls with LOCK_NB rather than blocking in the kernel so that ctx
// cancellation works and so that we can report who we are waiting on.
func acquire(ctx context.Context, e *Env, slug string, excl bool) (*flockLease, error) {
	path := e.LockPath(slug)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	how := syscall.LOCK_SH
	if excl {
		how = syscall.LOCK_EX
	}
	announced := false
	for {
		err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB)
		if err == nil {
			return &flockLease{f: f, path: path, excl: excl}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN && err != syscall.EINTR {
			f.Close()
			return nil, fmt.Errorf("flock %s: %w", path, err)
		}
		if !announced && err != syscall.EINTR {
			announced = true
			kind := "shared"
			if excl {
				kind = "exclusive"
			}
			holder := ""
			if hb, herr := ReadHeartbeat(e, slug); herr == nil {
				holder = fmt.Sprintf("%s pid %d (step %s, since %s)", hb.Host, hb.PID, hb.Step, hb.StartedAt.Format(time.RFC3339))
			}
			e.Log.Info("waiting for lock", "job", slug, "mode", kind, "held_by", holder)
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (l *flockLease) release() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}

// leases is an ordered set of held locks, released as a group.
type leases []*flockLease

func (ls leases) release() {
	for i := len(ls) - 1; i >= 0; i-- {
		ls[i].release()
	}
}

// TryReadLease takes a shared flock on a job's lock file without waiting. It
// lets a reader of a published artifact tell "ready" apart from "being
// republished right now" instead of racing the rename. ok=false means someone
// holds it exclusively; the returned func must be called to release.
func TryReadLease(e *Env, slug string) (release func(), ok bool, err error) {
	path := e.LockPath(slug)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("flock %s: %w", path, err)
	}
	l := &flockLease{f: f, path: path}
	return l.release, true, nil
}
