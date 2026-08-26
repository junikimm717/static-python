// Package sources turns a pinned config.Source into a patched, edited source
// tree on disk.
//
// The integrity rules the rest of the build leans on:
//
//   - A tarball in dist/src is trusted only once its .done marker exists, and
//     that marker is written only after sha256 verification.
//   - A file whose hash does not match the pin is deleted, never reused.
//   - An edit whose anchor has moved fails the build instead of no-opping.
package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

const attemptsPerURL = 3

// A mirror that accepts the connection and then goes silent would otherwise
// hang forever: ResponseHeaderTimeout covers the headers and nothing covers
// the body. CPython's tarball gates every other job, so one mute mirror can
// stall the whole build.
const stallTimeout = 90 * time.Second

func backoff(attempt int) time.Duration { return time.Duration(1<<attempt) * time.Second }

var client = &http.Client{
	// No overall timeout: Python-3.13.tar.xz-sized downloads over a slow mirror
	// are legitimate. Progress is bounded by the dial, header and stall
	// timeouts instead.
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 60 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   60 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
		ExpectContinueTimeout: 5 * time.Second,
		MaxIdleConns:          16,
	},
}

var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Slug names the patch directory, the artifact directory and the job log dir.
func Slug(s config.Source) string { return s.Name + "-" + s.Version }

// An unpinned or short sha256 is the one failure mode that would let
// unverified bytes through the whole pipeline, so it is checked before
// anything touches the network.
func ValidateSource(s config.Source) error {
	switch {
	case s.Name == "":
		return fmt.Errorf("sources: entry with empty name")
	case s.Version == "":
		return fmt.Errorf("sources: %s: empty version", s.Name)
	case s.File == "":
		return fmt.Errorf("sources: %s: empty file", s.Name)
	case !sha256Re.MatchString(s.SHA256):
		return fmt.Errorf("sources: %s: sha256 %q is not 64 lowercase hex chars", s.Name, s.SHA256)
	}
	for _, u := range s.URLs {
		if u == "" {
			return fmt.Errorf("sources: %s: empty url", s.Name)
		}
	}
	return nil
}

// CacheDir is where every fetched archive lands.
func CacheDir(e *core.Env) string { return e.Path(core.DirSrc) }

// Re-pinning a version never collides with the file the old pin left behind.
func Path(e *core.Env, s config.Source) string {
	return filepath.Join(CacheDir(e), shortSum(s)+"-"+s.File)
}

func shortSum(s config.Source) string {
	if len(s.SHA256) < 16 {
		return s.SHA256
	}
	return s.SHA256[:16]
}

func donePath(e *core.Env, s config.Source) string { return Path(e, s) + ".done" }

func lockPath(e *core.Env, s config.Source) string {
	return filepath.Join(CacheDir(e), "."+shortSum(s)+".lock")
}

// Without taking the lock or hashing anything.
func Fetched(e *core.Env, s config.Source) bool { return verified(e, s) }

// Fetch downloads s into dist/src and returns the archive path, trying URLs in
// order. It is idempotent and safe against concurrent workers and concurrent
// staticpy processes: everything past the fast path happens under an exclusive
// flock on a per-source lock file, and the download lands in a temp file in the
// same directory that is verified before being renamed into place.
func Fetch(ctx context.Context, e *core.Env, s config.Source) (string, error) {
	if err := ValidateSource(s); err != nil {
		return "", err
	}
	dir := CacheDir(e)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("sources: creating %s: %w", dir, err)
	}
	dst := Path(e, s)

	// Fast path: a .done marker means some process already verified this exact
	// content, so we do not re-hash a 25 MB tarball on every build.
	if verified(e, s) {
		return dst, nil
	}

	unlock, err := lockExclusive(ctx, lockPath(e, s))
	if err != nil {
		return "", err
	}
	defer unlock()

	// Another process may have finished while we waited on the lock.
	if verified(e, s) {
		return dst, nil
	}
	// The file can exist without a marker: an older run, or a marker someone
	// deleted. Hash it, and mark it only if it matches.
	switch sum, err := hashFile(dst); {
	case err == nil && sum == s.SHA256:
		return dst, mark(e, s)
	case err == nil:
		// A mismatched file is removed rather than left for the next attempt to
		// reuse: keeping it would make every later run re-fail on the same
		// poisoned bytes.
		if rmErr := os.Remove(dst); rmErr != nil {
			return "", fmt.Errorf("sources: removing corrupt %s: %w", dst, rmErr)
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", err
	}

	// Env.Offline serves only what dist/src already holds, so a build on a
	// disconnected machine fails with something actionable instead of blocking
	// on a dial timeout.
	if e.Offline {
		return "", fmt.Errorf("sources: %s (%s) is not in %s and --offline was given; "+
			"run `staticpy fetch` with network access first", s.File, s.Name, dir)
	}
	if len(s.URLs) == 0 {
		return "", fmt.Errorf("sources: %s has no urls to fetch from", s.Name)
	}

	var problems []string
	for _, u := range s.URLs {
		for attempt := 0; attempt < attemptsPerURL; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(backoff(attempt)):
				}
			}
			err := download(ctx, u, dst, s)
			if err == nil {
				return dst, mark(e, s)
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			problems = append(problems, fmt.Sprintf("  %s (try %d): %v", u, attempt+1, err))
			// A bad checksum or a 4xx will not fix itself; move to the next
			// mirror instead of burning the backoff.
			var ce *ChecksumError
			var pe *permanentError
			if errors.As(err, &ce) || errors.As(err, &pe) {
				break
			}
		}
	}
	return "", fmt.Errorf("sources: could not fetch %s from any of %d mirrors:\n%s",
		s.File, len(s.URLs), strings.Join(problems, "\n"))
}

// permanentError marks a failure that a retry cannot fix.
type permanentError struct{ error }

func (e *permanentError) Unwrap() error { return e.error }

// ChecksumError reports a body whose hash did not match the pin.
type ChecksumError struct {
	Source config.Source
	URL    string
	Actual string
}

func (e *ChecksumError) Error() string {
	return fmt.Sprintf("sha256 mismatch for %s from %s: expected %s, got %s",
		e.Source.File, e.URL, e.Source.SHA256, e.Actual)
}

func download(ctx context.Context, url, dst string, s config.Source) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "staticpy/1 (+sources)")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("HTTP %s", resp.Status)
		if c := resp.StatusCode; c >= 400 && c < 500 && c != 408 && c != 429 {
			return &permanentError{err} // retrying a 404 only wastes time
		}
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+s.File+".part-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	h := sha256.New()
	var stalled atomic.Bool
	timer := time.AfterFunc(stallTimeout, func() { stalled.Store(true); cancel() })
	defer timer.Stop()
	if _, err = io.Copy(io.MultiWriter(tmp, h), &stallReader{r: resp.Body, timer: timer}); err != nil {
		if stalled.Load() {
			return fmt.Errorf("stalled: no data for %s", stallTimeout)
		}
		return fmt.Errorf("reading body: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	// Verified before the rename, so a file at the real path is always a file
	// whose bytes matched the pin.
	if got := hex.EncodeToString(h.Sum(nil)); got != s.SHA256 {
		err = &ChecksumError{Source: s, URL: url, Actual: got}
		return err
	}
	if err = os.Chmod(tmpName, 0o444); err != nil {
		return err
	}
	if err = os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("sources: publishing %s: %w", dst, err)
	}
	return nil
}

func verified(e *core.Env, s config.Source) bool {
	if _, err := os.Stat(donePath(e, s)); err != nil {
		return false
	}
	fi, err := os.Stat(Path(e, s))
	return err == nil && fi.Mode().IsRegular()
}

// mark writes the .done marker. Every caller reaches it only after the bytes
// on disk have been hashed against the pin.
func mark(e *core.Env, s config.Source) error {
	p := donePath(e, s)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("sources: writing marker %s: %w", p, err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s  %s\n", s.SHA256, s.File)
	return err
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("sources: hashing %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Lock files are never removed: deleting one would break flock identity for a
// process already holding it open.
func lockExclusive(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("sources: opening lock %s: %w", path, err)
	}
	// Flock blocks in the kernel, so run it off-thread to stay cancellable.
	done := make(chan error, 1)
	go func() { done <- syscall.Flock(int(f.Fd()), syscall.LOCK_EX) }()
	select {
	case err := <-done:
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("sources: flock %s: %w", path, err)
		}
	case <-ctx.Done():
		// The goroutine will finish and its lock dies with the fd we close.
		go func() { <-done; f.Close() }()
		return nil, ctx.Err()
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// stallReader restarts the stall timer on every successful read, so the
// deadline measures silence rather than total transfer time: a slow mirror
// serving a large tarball is fine, a mute one is not.
type stallReader struct {
	r     io.Reader
	timer *time.Timer
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.timer.Reset(stallTimeout)
	}
	return n, err
}
