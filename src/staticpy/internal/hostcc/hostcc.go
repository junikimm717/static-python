// Package hostcc gates the reference build on a host toolchain that can
// actually produce it.
//
// The reference interpreter is the one thing staticpy builds with the host's
// own compiler and libc rather than a provisioned toolchain, so it is the one
// thing that can fail for reasons the rest of the build system never sees.
package hostcc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Architectures the reference build is verified on. Not a capability claim:
// an unverified reference interpreter is worse than none, because its numbers
// still look authoritative.
var supported = map[string]string{
	"amd64": "x86_64",
	"arm64": "aarch64",
}

func SupportedArch() (string, error) {
	if a, ok := supported[runtime.GOARCH]; ok {
		return a, nil
	}
	return "", fmt.Errorf("the reference build is only supported on x86_64 and aarch64; this machine is %s.\nThe static build is unaffected: `staticpy build` works on every configured target", runtime.GOARCH)
}

// Find resolves the host C compiler, honouring CC.
func Find() (string, error) {
	var tried []string
	if cc := strings.TrimSpace(os.Getenv("CC")); cc != "" {
		if p, err := exec.LookPath(cc); err == nil {
			return p, nil
		}
		tried = append(tried, cc+" (from $CC)")
	}
	for _, name := range []string{"cc", "gcc", "clang"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
		tried = append(tried, name)
	}
	return "", fmt.Errorf("no host C compiler found (tried %s).\nThe reference build needs one: it is compiled against this machine's own libc, not a provisioned toolchain.\nInstall gcc or clang, or set CC", strings.Join(tried, ", "))
}

// Report is what doctor prints and what --reference gates on.
type Report struct {
	Arch    string
	CC      string
	Compile error
	Shared  error
	Headers error
}

func (r Report) OK() bool { return r.Compile == nil && r.Shared == nil && r.Headers == nil }

// Probe proves the compiler works rather than merely existing.
//
// The shared-library link is the check that matters and the one a --version
// probe misses: every dependency of the reference interpreter is built shared,
// so a toolchain that cannot produce a .so fails deep in a dependency build
// instead of here.
func Probe(ctx context.Context, cc string) Report {
	r := Report{CC: cc}
	if a, err := SupportedArch(); err == nil {
		r.Arch = a
	} else {
		r.Compile = err
		return r
	}
	dir, err := os.MkdirTemp("", "staticpy-hostcc-")
	if err != nil {
		r.Compile = err
		return r
	}
	defer os.RemoveAll(dir)

	src := filepath.Join(dir, "t.c")
	if err := os.WriteFile(src, []byte("int main(void){return 0;}\n"), 0o644); err != nil {
		r.Compile = err
		return r
	}
	r.Compile = run(ctx, dir, cc, src, "-o", filepath.Join(dir, "t"))

	shared := filepath.Join(dir, "s.c")
	if err := os.WriteFile(shared, []byte("int f(void){return 1;}\n"), 0o644); err != nil {
		r.Shared = err
	} else {
		r.Shared = run(ctx, dir, cc, "-shared", "-fPIC", shared, "-o", filepath.Join(dir, "libs.so"))
	}

	hdr := filepath.Join(dir, "h.c")
	if err := os.WriteFile(hdr, []byte("#include <stdio.h>\n#include <stdlib.h>\nint main(void){return 0;}\n"), 0o644); err != nil {
		r.Headers = err
	} else {
		r.Headers = run(ctx, dir, cc, hdr, "-o", filepath.Join(dir, "h"))
	}
	return r
}

func run(ctx context.Context, dir, cc string, args ...string) error {
	cmd := exec.CommandContext(ctx, cc, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 400 {
			msg = msg[:400] + " ..."
		}
		return fmt.Errorf("%s %s: %v\n%s", filepath.Base(cc), strings.Join(args, " "), err, msg)
	}
	return nil
}

// Gate is the fail-fast entry point: it runs before anything is fetched, so a
// missing toolchain costs nothing but the check.
func Gate(ctx context.Context) (Report, error) {
	if _, err := SupportedArch(); err != nil {
		return Report{}, err
	}
	cc, err := Find()
	if err != nil {
		return Report{}, err
	}
	r := Probe(ctx, cc)
	switch {
	case r.Compile != nil:
		return r, fmt.Errorf("host compiler %s cannot build a plain executable:\n%w", cc, r.Compile)
	case r.Shared != nil:
		return r, fmt.Errorf("host compiler %s cannot link a shared library, which every reference dependency needs:\n%w", cc, r.Shared)
	case r.Headers != nil:
		return r, fmt.Errorf("host libc development headers are missing (stdio.h/stdlib.h did not resolve):\n%w", r.Headers)
	}
	return r, nil
}

// Identity is what a job key records about the host toolchain.
//
// The static build takes its compiler's identity from a gccfactory manifest or,
// failing that, a probe of the driver. Neither is available here, and the key
// has to name the libc as well as the compiler: a distro glibc upgrade changes
// what the interpreter is without touching gcc, and an artifact that survived
// it would be served under a key that no longer describes it.
type Identity struct {
	CC      string
	Version string
	Machine string
	// Triple is the logical name, e.g. x86_64-linux-gnu, not gcc's
	// distro-flavoured -dumpmachine (x86_64-redhat-linux). It names the
	// artifact, so it must not change when the same build moves distro.
	Triple string
	Libc   string
	Key    string
}

// Describe is the one-line human form, for doctor and for provenance.
func (id Identity) Describe() string {
	return fmt.Sprintf("gcc %s targeting %s against %s, driver+headers %s",
		id.Version, id.Machine, id.Libc, id.Key[:12])
}

// The header fingerprint is a sorted dump of every macro the preprocessor
// defines for <features.h>: it covers the compiler's version and target macros
// and the libc's version together, costs a single preprocess, and carries no
// __DATE__ or __TIME__, so it is stable across runs. Nothing is executed, so
// this works the same whether or not the machine can run what it just built.
func Identify(ctx context.Context, cc string) (Identity, error) {
	id := Identity{CC: cc}
	var err error
	if id.Version, err = dump(ctx, cc, "-dumpversion"); err != nil {
		return id, err
	}
	if id.Machine, err = dump(ctx, cc, "-dumpmachine"); err != nil {
		return id, err
	}
	arch, err := SupportedArch()
	if err != nil {
		return id, err
	}
	macros, err := macroDump(ctx, cc)
	if err != nil {
		return id, err
	}
	id.Libc = libcOf(macros)
	id.Triple = arch + "-linux-" + libcTag(id.Libc)

	driver, err := sha256File(cc)
	if err != nil {
		return id, fmt.Errorf("hostcc: hashing %s: %w", cc, err)
	}
	sum := sha256.Sum256([]byte(strings.Join(macros, "\n")))
	h := sha256.Sum256([]byte(strings.Join(
		[]string{id.Version, id.Machine, driver, hex.EncodeToString(sum[:])}, "\x00")))
	id.Key = hex.EncodeToString(h[:])
	return id, nil
}

func dump(ctx context.Context, cc, arg string) (string, error) {
	cmd := exec.CommandContext(ctx, cc, arg)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("hostcc: %s %s failed: %w", cc, arg, err)
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return "", fmt.Errorf("hostcc: %s %s printed nothing", cc, arg)
	}
	return text, nil
}

func macroDump(ctx context.Context, cc string) ([]string, error) {
	cmd := exec.CommandContext(ctx, cc, "-E", "-dM", "-x", "c", "-")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.Stdin = strings.NewReader("#include <features.h>\n")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("hostcc: %s cannot preprocess <features.h>, so the host libc headers are unusable: %w", cc, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("hostcc: %s -dM produced no macros", cc)
	}
	// gcc emits these in an unspecified order.
	sort.Strings(lines)
	return lines, nil
}

// musl defines no version macro at all, so it can be named but not versioned.
// The header fingerprint still tells two of them apart, and that is what the
// key relies on.
func libcOf(macros []string) string {
	var major, minor string
	musl := false
	for _, m := range macros {
		switch {
		case strings.HasPrefix(m, "#define __GLIBC__ "):
			major = strings.TrimPrefix(m, "#define __GLIBC__ ")
		case strings.HasPrefix(m, "#define __GLIBC_MINOR__ "):
			minor = strings.TrimPrefix(m, "#define __GLIBC_MINOR__ ")
		case strings.Contains(m, "__musl__"), strings.Contains(m, "__MUSL__"):
			musl = true
		}
	}
	switch {
	case major != "" && minor != "":
		return "glibc " + major + "." + minor
	case musl:
		return "musl"
	default:
		return "unknown libc"
	}
}

func libcTag(libc string) string {
	if strings.HasPrefix(libc, "musl") {
		return "musl"
	}
	return "gnu"
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
