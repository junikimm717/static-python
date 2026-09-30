// Package hostcc finds and identifies the host C compiler.
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

var hostCCNames = []string{"gcc", "clang", "cc"}

func SupportedArch() (string, error) {
	if a, ok := supported[runtime.GOARCH]; ok {
		return a, nil
	}
	return "", fmt.Errorf("the reference build is only supported on x86_64 and aarch64; this machine is %s.\nThe static build is unaffected: `staticpy build` works on every configured target", runtime.GOARCH)
}

func Find() (string, error) {
	var tried []string
	if cc := strings.TrimSpace(os.Getenv("CC")); cc != "" {
		if p, err := exec.LookPath(cc); err == nil {
			return p, nil
		}
		tried = append(tried, cc+" (from $CC)")
	}
	// gcc before cc: CPython 3.13's configure matches *gcc* against $CC
	// for LTO, PGO, and -fno-semantic-interposition. /usr/bin/cc is gcc
	// and still misses. See staticpy-traps.
	for _, name := range hostCCNames {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
		tried = append(tried, name)
	}
	return "", fmt.Errorf("no host C compiler found (tried %s).\nThe reference build needs one: it is compiled against this machine's own libc, not a provisioned toolchain.\nInstall gcc or clang, or set CC", strings.Join(tried, ", "))
}

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
