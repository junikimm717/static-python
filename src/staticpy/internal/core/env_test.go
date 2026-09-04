package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/logging"
)

func TestSlugFromScratchName(t *testing.T) {
	name := scratchName("pycross:default:x86_64-linux-musl:aarch64-linux-musl")
	got, ok := slugFromScratchName(name)
	if !ok {
		t.Fatalf("slugFromScratchName(%q) failed", name)
	}
	want := lockFileName("pycross:default:x86_64-linux-musl:aarch64-linux-musl")
	if got != want {
		t.Fatalf("slug = %q, want %q", got, want)
	}
	if _, ok := slugFromScratchName("not-a-scratch-dir"); ok {
		t.Fatal("expected reject")
	}
}

func TestGCStaleKeepsScratchWithLiveHeartbeat(t *testing.T) {
	dist := t.TempDir()
	e := &Env{Dist: dist, Log: logging.Discard()}
	if err := e.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	slug := "pynative_default_x86_64-linux-musl"
	// A pid that is not alive in this namespace, and a dir older than StaleAge.
	const deadPID = 2147483647
	name := lockFileName(slug) + ".2147483647.abcd"
	work := filepath.Join(e.Path(DirWork), name)
	if err := os.MkdirAll(filepath.Join(work, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * StaleAge)
	if err := os.Chtimes(work, old, old); err != nil {
		t.Fatal(err)
	}

	hb := Heartbeat{
		Slug:      slug,
		PID:       deadPID,
		UpdatedAt: time.Now().UTC(),
	}
	if err := writeJSONAtomic(e.HeartbeatPath(slug), hb); err != nil {
		t.Fatal(err)
	}

	e.GCStale(StaleAge)
	if _, err := os.Stat(work); err != nil {
		t.Fatalf("live-heartbeat scratch was collected: %v", err)
	}

	// Stale heartbeat + dead pid: collect.
	hb.UpdatedAt = time.Now().UTC().Add(-time.Hour)
	if err := writeJSONAtomic(e.HeartbeatPath(slug), hb); err != nil {
		t.Fatal(err)
	}
	e.GCStale(StaleAge)
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Fatalf("dead scratch still present: %v", err)
	}
}
