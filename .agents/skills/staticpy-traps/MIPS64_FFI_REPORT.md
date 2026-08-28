# Bug Report: libffi Closures Return Garbage for Narrow Integers on mips64 BE

**Status: root-caused in libffi and fixed locally.** The offending instructions
are the narrow loads in `src/mips/n32.S`; our fix is
`config/patches/libffi-3.5.2/0001-narrow-closure-returns-on-big-endian-mips.diff`,
carried as a `target_patches` entry for `mips64-linux-musl` only. Still worth
filing upstream -- 3.5.2 has it, and every big-endian n32/n64 consumer hits it.

## Summary

On `mips64-linux-musl` (big-endian, n64 ABI), a libffi closure whose return
type is narrower than 64 bits hands back the **high** half of the 64-bit return
slot instead of the low half. Closures returning a full 64-bit type are
correct, and the non-closure `ffi_call` path is correct, so the fault is
confined to narrow returns from closures.

This makes every `ctypes` callback that returns `c_int` wrong:

```text
test.test_ctypes.test_simplesubclasses.Test.test_int_callback
    self.assertEqual(42, cb(42))
AssertionError: 42 != 1
```

## The pattern identifies the mechanism

```text
f(42)   -> 0      0x000000000000002A, high half = 0x00000000 =  0
f(1000) -> 0      0x00000000000003E8, high half = 0x00000000 =  0
f(-7)   -> -1     0xFFFFFFFFFFFFFFF9, high half = 0xFFFFFFFF = -1
```

Every result is the top 32 bits of the correctly sign-extended 64-bit value.

## Reproducer

No Python involved. Against the staticpy mips64 sysroot:

```c
#include <stdio.h>
#include <ffi.h>

static void cb(ffi_cif *cif, void *ret, void **args, void *ud) {
    (void)cif; (void)ud;
    *(ffi_arg *)ret = *(int *)args[0];
}

int main(void) {
    ffi_cif cif;
    ffi_type *at[1] = { &ffi_type_sint };
    void *code;
    ffi_closure *cl = ffi_closure_alloc(sizeof(ffi_closure), &code);
    ffi_prep_cif(&cif, FFI_DEFAULT_ABI, 1, &ffi_type_sint, at);
    ffi_prep_closure_loc(cl, &cif, cb, NULL, code);
    int (*f)(int) = (int (*)(int))code;
    printf("f(42)=%d f(1000)=%d f(-7)=%d\n", f(42), f(1000), f(-7));
    return 0;
}
```

```sh
SR=dist/artifacts/sysroot_default_mips64-linux-musl
dist/toolchains/mips64-linux-musl-cross/bin/mips64-linux-musl-gcc \
    -O2 -static -I$SR/include t.c -o t -L$SR/lib -lffi
qemu-mips64 -L dist/toolchains/mips64-linux-musl-cross/mips64-linux-musl ./t
# f(42)=0 f(1000)=0 f(-7)=-1
```

## What it is not

- **Not LTO.** The reproducer above is `-O2` with no `-flto` anywhere and fails
  identically. The interpreter is built with `-flto=auto
  -flto-partition=none`, so LTO was the first suspect and is ruled out.
- **Not big-endian in general.** s390x, powerpc64 and powerpc64le all return
  `cb(42) -> 42`. Only mips64 is affected.
- **Not the ABI selection.** The toolchain reports `_MIPS_SIM=_ABI64`, which is
  correct for this triple.
- **Not the calling path.** `ffi_call` with a 32-bit return is correct; only
  closures are wrong.

## Root cause

`src/mips/n32.S`. A closure leaves its return value in the `FFI_SIZEOF_ARG`-byte
slot at `V0_OFF2`, and the epilogue loads it back with a load sized to the return
type -- `lw`, `lh`, `lhu`, `lb`, `lbu` -- every one of them from offset 0 of that
slot. Offset 0 is the value on a little-endian target and the high half on a
big-endian one, so the fault needs n32/n64 **and** big-endian: mips64el, the
common case, is unaffected, which is why this has stood.

Reading the slot's low-order end is what the rest of the ecosystem assumes. It
is where a caller following libffi's documented `*(ffi_arg *)ret = value` leaves
the significant bytes; it is where CPython's ctypes writes a narrow callback
result (`Modules/_ctypes/callbacks.c`, under `WORDS_BIGENDIAN`); and it is what
`ffi_call`'s own narrow returns, a few hundred lines up in the same file, already
store. Not qemu: qemu executes the wrong load faithfully.

## Fix and verification

The patch guards the adjustment on `__MIPSEB__` / `_MIPSEB`, so little-endian
mips is untouched by construction. Against the same sysroot, with both the
`ffi_arg` and the ctypes write conventions, over all seven integer widths:

```text
BEFORE  sint8/uint8/sint16/uint16/sint32/uint32 -> WRONG (all values, both conventions)
        sint64                                  -> ok
AFTER   all seven                               -> ok
```

`mips64-linux-musl` stays `status = "experimental"` in `targets.toml` -- this
was the one known defect, but nothing about it was ever validated on real mips64
hardware.
