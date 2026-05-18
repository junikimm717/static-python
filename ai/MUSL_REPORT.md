# Bug Report: musl `fma` Loses Negative Zero on Underflow

## Summary

On an x86_64 musl toolchain built via `musl-cross-make`, `fma(1e-300,
-1e-300, 0.0)` returns positive zero for an underflow-to-zero case where
CPython 3.13's IEEE-754 tests expect negative zero.

This aborts CPython's PGO training run in:

```text
test_math.FMATests.test_fma_zero_result
```

This document tracks three layers of root cause; the surface bug is in musl,
but two downstream safety nets (the toolchain's lack of `-mfma` and CPython's
runtime musl-detection) both fail in ways specific to this project's static,
non-PIE build configuration.

## Minimal Reproducer

```c
#include <math.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

static void show(const char *label, double v) {
    uint64_t bits;
    memcpy(&bits, &v, sizeof bits);
    printf("%-28s signbit=%d bits=%016llx\n",
           label, signbit(v) ? 1 : 0, (unsigned long long)bits);
}

int main(void) {
    volatile double tiny_v = 1e-300;
    double tiny = tiny_v;

    show("fma(tiny,-tiny,0.0)", fma(tiny, -tiny, 0.0));
    return 0;
}
```

## Build and Run

```sh
x86_64-linux-musl-gcc -O0 fma.c -lm -o fma-o0
./fma-o0

x86_64-linux-musl-gcc -O3 fma.c -lm -o fma-o3
./fma-o3

x86_64-linux-musl-gcc -O3 -fno-builtin-fma fma.c -lm -o fma-nobuiltin
./fma-nobuiltin
```

## Observed

All tested musl builds print positive zero:

```text
fma(tiny,-tiny,0.0)       signbit=0 bits=0000000000000000
```

The same result was observed with:

```text
-O0
-O3
-O3 -fno-builtin-fma
-O3 -ffp-contract=off
-O3 -flto -flto-partition=none
-O3 -flto -flto-partition=none -fno-builtin-fma
```

It is **fixed** by `-O3 -mfma` (or any `-march=x86-64-v3`-class flag), which
makes gcc emit `vfmadd*sd` inline at the call site and avoid musl's libm
entirely. That is what makes layer 2 below relevant.

## Expected

The result should preserve the negative sign on underflow:

```text
fma(tiny,-tiny,0.0)       signbit=1 bits=8000000000000000
```

A host glibc build of the same C reproducer on the same machine returns the
expected negative zero:

```text
fma(tiny,-tiny,0.0)       signbit=1 bits=8000000000000000
```

## Environment

```text
arch: x86_64
musl: 1.2.5, via musl-cross-make
gcc: 15.1.0
kernel: Linux 6.18.5-200.fc43.x86_64
```

The musl toolchain used for reproduction was:

```text
/workspace/deps-x86_64-linux-musl/x86_64-linux-musl-native/bin/x86_64-linux-musl-gcc
```

The `fma` symbol is present in the static musl `libc.a`, so this does not
appear to be a missing-symbol fallback:

```text
libc.a:fma.lo:0000000000000000 T fma
```

## CPython Failure

CPython 3.13.13 reproduces the same issue during the PGO profile run:

```text
FAIL: test_fma_zero_result (test.test_math.FMATests.test_fma_zero_result)
AssertionError: False is not true : Expected a negative zero, got 0.0
```

The failing assertion is:

```python
tiny = 1e-300
self.assertIsNegativeZero(math.fma(tiny, -tiny, 0.0))
```

Direct CPython reproducer:

```python
import math
import struct

v = math.fma(1e-300, -1e-300, 0.0)
print(math.copysign(1.0, v), struct.pack(">d", v).hex())
```

Observed under the musl-built CPython profile interpreter:

```text
1.0 0000000000000000
```

Expected:

```text
-1.0 8000000000000000
```

## Three-Layer Root Cause

This isn't a single bug. Three independent failures stack to produce the
visible PGO abort.

### Layer 1 -- the actual fma bug in musl 1.2.5

`src/math/fma.c` (the generic software fallback) has a fast path for `z==0`:

```c
if (nz.e >= ZEROINFNAN) {
    if (nz.e > ZEROINFNAN) /* z==0 */
        return x*y + z;
    return z;
}
```

That `return x*y + z;` is wrong for the underflow case. For
`fma(1e-300, -1e-300, 0.0)`:

1. `x*y` is mathematically `-1e-600`, which is below the smallest representable
   subnormal, so the regular fp multiply rounds it to `-0`.
2. `-0 + (+0)` then evaluates to `+0` by the IEEE-754 round-to-nearest-even
   rule for adding opposite-signed zeros.

So musl rounds twice and loses the sign. True IEEE FMA must round once, on
the exact infinite-precision `(x*y) + z` -- which here is a tiny negative
number that rounds to `-0`.

This is a known upstream bug, reported on `musl@openwall` on 2025-03-11
([thread](https://www.openwall.com/lists/musl/2025/03/11/3)). It is **not
fixed in any tagged musl release as of writing**; musl 1.2.5 (Feb 2024) predates
the report and there has been no 1.2.6 release. Our local
`cross-make/patches/musl-1.2.5/` carries only the two CVE-2025-26519 patches,
not an fma fix.

The simplest known patch is a one-liner -- replace `return x*y + z;` with
`return x*y;` in that fast path -- but discussion on the CPython issue noted
that other corner cases of the same `fma.c` may have similar single-/double-
rounding hazards, so the upstream fix may end up larger.

### Layer 2 -- musl is built without `-mfma`

`src/math/x86_64/fma.c` *does* contain a hardware-FMA specialization that
would side-step the bug entirely:

```c
#if __FMA__
double fma(double x, double y, double z)
{
    __asm__ ("vfmadd132sd %1, %2, %0" : "+x" (x) : "x" (y), "x" (z));
    return x;
}
#elif __FMA4__
... /* AMD vfmaddsd variant */
#else
#include "../fma.c"
#endif
```

But `__FMA__` only gets defined when the source is compiled with `-mfma`
(equivalently `-march=haswell` or `-march=x86-64-v3`). `musl-cross-make`
doesn't pass either, and the baseline x86_64 ABI doesn't include FMA, so
the `#else` branch wins and the generic buggy `fma.c` is what actually
lands in `libc.a`. Confirmed by disassembling the built object: it's the
~300-line integer-math routine with a `normalize` helper, no inline
`vfmadd*` instruction in sight.

Adding `-mfma` to musl's build would fix this for x86-64-v3-class CPUs but
would move our toolchain's baseline ABI off plain `x86_64`. It also wouldn't
help any of the non-x86 architectures in `supported.txt`, where the generic
`fma.c` is always what runs.

### Layer 3 -- CPython's `linked_to_musl()` doesn't recognize static binaries

CPython 3.13 already has a skip for this exact test:

```python
@unittest.skipIf(
    sys.platform.startswith(("freebsd", "wasi", "netbsd", "emscripten"))
    or (sys.platform == "android" and platform.machine() == "x86_64")
    or support.linked_to_musl(),  # gh-131032
    f"this platform doesn't implement IEE 754-2008 properly")
def test_fma_zero_result(self):
```

The implementation of `linked_to_musl()` shells out to `ldd`:

```python
def linked_to_musl():
    if sys.platform != 'linux':
        return False
    import subprocess
    exe = getattr(sys, '_base_executable', sys.executable)
    cmd = ['ldd', exe]
    try:
        stdout = subprocess.check_output(cmd,
                                         text=True,
                                         stderr=subprocess.STDOUT)
    except (OSError, subprocess.CalledProcessError):
        return False
    return ('musl' in stdout)
```

That works for shared-libc musl (Alpine, distro Python on musl), but the
PGO interpreter we build with `-static -no-pie` makes `ldd` exit non-zero:

```text
$ ldd .../Python-3.13.13/python
/lib/ld-musl-x86_64.so.1: .../python: Not a valid dynamic program
$ echo $?
1
```

`subprocess.check_output` raises `CalledProcessError`, the `except` clause
swallows it, `linked_to_musl()` returns `False`, the skip never triggers,
and `test_fma_zero_result` runs to its inevitable failure under PGO.

(For the dynamic baseline -- `benchmark/dynamic-build.sh` -- this layer
*does* work; ldd on the shared interpreter returns 0 and prints the musl
loader, so the test is properly skipped without any of our intervention.)

## Current workaround (in tree)

To unblock benchmarking we apply the bluntest fix at layer 3: add
`-i test_fma_zero_result` to `PROFILE_TASK` so the failing test is excluded
from the PGO run, the same way `-x test_re` already excludes musl's locale-
related test_re failures.

The change is in two places:

- `Makefile` (`PROFILE_TASK ?=`) -- applies to the static `python3` target.
- `benchmark/dynamic-build.sh` -- mirrored so the dynamic baseline uses the
  same exclusion set (defense-in-depth; layer 3 already covers it there).

The runtime bug at layer 1 is **not fixed** by this change. `math.fma(x,y,z)`
in the resulting interpreter will still return the wrong sign for the
underflow-to-zero case. Any user code that relies on IEEE-754 fma sign
preservation will still get bitten.

## What we are explicitly not doing yet

In rough order of effort and blast radius:

1. **Patching musl** in `cross-make/patches/musl-1.2.5/`. A one-line patch
   (`return x*y + z;` -> `return x*y;`) would fix layer 1, but upstream
   discussion suggests other corner cases of `fma.c` may have similar issues
   and the eventual upstream fix may be larger. Carrying an out-of-tree
   patch ahead of upstream is doable but adds a maintenance debt for every
   musl version bump.

2. **Building musl with `-mfma`** (and probably matching `-march`). Trades
   the bug for a higher baseline CPU floor on x86_64; doesn't help other
   architectures, where the generic `fma.c` is always what runs.

3. **Patching `linked_to_musl()`** (or providing a sysconfig-based override)
   so the upstream CPython skip recognizes static-musl builds. Cleanly fixes
   layer 3 for any future musl-only test, but is purely cosmetic for the
   underlying fma defect.

If/when this becomes more than a PGO blocker -- e.g. a user reports wrong
math, or a benchmark-relevant numeric code path is affected -- option 1 is
the right escalation.

## References

- musl mailing list, "Re: Bug in fma(): should return negative zero":
  <https://www.openwall.com/lists/musl/2025/03/11/3>
- CPython issue gh-131032, "test_math.test_fma_zero_result() fails with the
  musl C library": <https://github.com/python/cpython/issues/131032>
- CPython PR gh-131071 (the upstream `linked_to_musl()` skip).
