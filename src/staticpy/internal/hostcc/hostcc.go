// Package hostcc gates the reference build on a host toolchain that can
// actually produce it.
//
// The reference interpreter is the one thing staticpy builds with the host's
// own compiler and libc rather than a provisioned toolchain, so it is the one
// thing that can fail for reasons the rest of the build system never sees.
package hostcc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
