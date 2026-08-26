package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// HeartbeatInterval is how often a builder refreshes its heartbeat file.
const HeartbeatInterval = 5 * time.Second

// It is advisory: it drives `staticpy status` and lock-wait messages, never
// correctness.
type Heartbeat struct {
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Key       string    `json:"key"`
	PID       int       `json:"pid"`
	Host      string    `json:"host"`
	Step      string    `json:"step"`
	LogDir    string    `json:"log_dir"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (e *Env) HeartbeatPath(slug string) string {
	return e.Path(DirState, "heartbeats", lockFileName(slug)+".json")
}

func ReadHeartbeat(e *Env, slug string) (*Heartbeat, error) {
	b, err := os.ReadFile(e.HeartbeatPath(slug))
	if err != nil {
		return nil, err
	}
	var h Heartbeat
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// Being refreshed very recently, not just having a live pid, also covers a
// builder on another machine sharing dist/.
func (h *Heartbeat) Live() bool {
	if h == nil {
		return false
	}
	return pidAlive(h.PID) || time.Since(h.UpdatedAt) < 4*HeartbeatInterval
}

// beat owns the heartbeat file for one build lease.
type beat struct {
	stop chan struct{}
	wg   sync.WaitGroup
}

func startHeartbeat(e *Env, n *node, r *Runner, started time.Time) *beat {
	host, _ := os.Hostname()
	b := &beat{stop: make(chan struct{})}
	write := func() {
		h := Heartbeat{
			Slug: n.slug, Name: n.job.Name(), Key: n.key,
			PID: os.Getpid(), Host: host, Step: r.CurrentStep(),
			LogDir: r.Dir(), StartedAt: started, UpdatedAt: time.Now().UTC(),
		}
		if err := writeJSONAtomic(e.HeartbeatPath(n.slug), h); err != nil {
			e.Log.Debug("heartbeat write failed", "job", n.slug, "err", err)
		}
	}
	write()
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		t := time.NewTicker(HeartbeatInterval)
		defer t.Stop()
		for {
			select {
			case <-b.stop:
				return
			case <-t.C:
				write()
			}
		}
	}()
	return b
}

func (b *beat) close(e *Env, slug string) {
	close(b.stop)
	b.wg.Wait()
	if err := os.Remove(e.HeartbeatPath(slug)); err != nil && !os.IsNotExist(err) {
		e.Log.Debug("heartbeat remove failed", "job", slug, "err", err)
	}
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.%s.tmp", path, os.Getpid(), randHex(4))
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
