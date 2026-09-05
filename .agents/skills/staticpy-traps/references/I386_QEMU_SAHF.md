# qemu 11.0 TCG: stale `cc_op` after SAHF breaks i386 CPython compares

## Summary

Under Alpine `qemu-i386` 11.0.1, the static `i386-linux-musl` interpreter
fails core-verify methods by amounts no 387 misses: `divmod(7.0, 2.0)` is
`(4.0, 1.0)`, `math.hypot` returns 0, `pow(-2.0, -2.0)` is complex,
half-float 1.0 packs as `0x3c01`. The binary is fine. Host qemu 8.2.2
gives the IEEE answers. The same 11.0.1 binary with `-one-insn-per-tb`
does too.

## Minimal reproducer

    PY=dist/artifacts/pycross_default_x86_64-linux-musl_i386-linux-musl/bin/python3.13
    SYS=dist/toolchains/i386-linux-musl-cross/i386-linux-musl
    CODE='print(divmod(7.0,2.0)); import math; print(math.hypot(3,4))'
    qemu-i386 -L "$SYS" "$PY" -c "$CODE"
    qemu-i386 -one-insn-per-tb -L "$SYS" "$PY" -c "$CODE"

A 20-line C hypot/fmod built with the same gcc+musl is correct on both
qemus. The failure needs TCG translation-block chaining inside a large
LTO'd function.

## Root cause

qemu 11.0 commit da7649c6 (target/i386/tcg: do not compute all flags for
SAHF) left cc_op stale after SAHF. gcc's i386 -mfpmath=387 compare is
fucompp; fnstsw %ax; sahf; jcc. Stale cc_op takes the wrong branch.
GitLab #3537; fix c25f695 (Update cc_op for SAHF), queued for 11.0.4 /
11.1. Host 8.2.2 predates the bad commit.

How each symptom falls out of a wrong > / != in inlined C:

- _float_div_mod: fmod and the division are correct; `div - floordiv > 0.5`
  (0.0 > 0.5) snaps the quotient 3 to 4.
- float_pow: `iw != floor(iw)` on an integer exponent takes the complex path.
- vector_norm (math.hypot / math.dist): `x > max` never updates, so max
  stays 0 and the function returns 0.
- PyFloat_Pack2: the same `> 0.5` rounding bump yields 0x3c01 / 1.5009765625.

Python-level 0.0 > 0.5 and isolated fnstsw; sahf; seta are correct on
11.0.1. CPU model does not change anything. This is not an ST(0)/XMM0
mismatch: math.fmod(7,2) returns 1.0.

sinh(1)+sinh(-1) = 2.22e-16 is unchanged across 8.2.2, 11.0.1, and
-one-insn-per-tb. That is the real i386 80-bit ABI issue.

## What we ended up doing

The Dockerfile installs qemu-user **11.1.1-r0** as Alpine edge static-pie
binaries (sha256-pinned in `scripts/docker/fetch-qemu-user.sh`). Ubuntu
24.04's own qemu-user is 8.2.2 and still has the wrong `cc_op` and a
broken `/proc/self/stat` `num_threads`. The twelve
`[expect."i386-linux-musl:qemu"]` ignores are deleted. *MathTests.testSinh*
stays on `[expect.i386-linux-musl]` — that is the real 80-bit ABI issue.

## What we'd want upstream

Ubuntu shipping qemu-user >= 11.1, so the Alpine apk fetch can go.
`-one-insn-per-tb` is a proof, not a default.
