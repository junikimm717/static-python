# s390x sigsetjmp(buf, 0) jumps into the middle of an instruction

## Summary

On s390x, readline's completion path calls `sigsetjmp(buf, 0)` and the
interpreter dies with `qemu: uncaught target signal 4 (Illegal instruction)`.
CPython 3.15's `test_sqlite3.test_cli.Completion` (new in 3.15) is the first
core test to reach it: seven methods fail, and each pty child leaves a
`qemu_python3.15_*.core`. musl's hand-written `src/signal/s390x/sigsetjmp.s`
branches to `___setjmp` with `jz`, a 16-bit PC-relative branch
(`R_390_PC16DBL`, ±64 KiB), although `___setjmp` lives in another object
(`setjmp.o`). Once the static link puts `setjmp.o` more than 64 KiB away, GNU ld
2.44 wraps the displacement **without** a "relocation truncated to fit" error,
and the branch lands mid-instruction. It is not qemu (10.2.2 and 11.1.1 both
fault), not big-endian (ppc64 and mips64 complete), and not LTO in the usual
sense: the bug is latent in every s390x musl static binary and only fires
when the layout grows.

## Minimal reproducer

```
# a.s                         # b.s
.global _start                .global far
_start: ltgr %r3,%r3          .space 0x18000
        jz   far              far: br %r14
        j    _start
s390x-linux-musl-as a.s -o a.o && s390x-linux-musl-as b.s -o b.o
s390x-linux-musl-ld -o t a.o b.o       # rc=0, no warning
objdump -d t  →  je ff8084   (far is at 1018084)
```

## Layer 1: musl

```
__sigsetjmp:  ltgr %r3,%r3 ; jz ___setjmp      # R_390_PC16DBL ___setjmp+0x2
              ...            brasl %r14,___setjmp  # R_390_PC32DBL, fine
```

In python3.15 (3.15.2 default, s390x): `__sigsetjmp` at `0x1de8e40`,
`___setjmp` at `0x1e005d4` (the `brasl` resolves correctly), distance
`0x17790`. The halfword offset `0xBBC8` is −0x4438, so `je` goes to
`0x1de05d4`, which is the second halfword (`0x00a6`) of an unrelated `je`. The
caller does `lghi %r3,0` right before `brasl __sigsetjmp`, which is
`sigsetjmp(…, 0)`. Found with `qemu-s390x -d in_asm,int,nochain` on the pty
child: the last two translated blocks are `ltgr;je` and the lone `00a6`.

## Layer 2: binutils

`R_390_PC16DBL` uses a lax overflow check, so a displacement that wraps into
16 bits is not reported. With a correct check, layer 1 would have been a link
error, not a SIGILL.

## What we ended up doing

The fix belongs in the toolchain's musl (`jz` → `jgz`, i.e. `brcl`), tracked
as gccfactory#16. Until a fixed s390x toolchain is published,
`assets/files/libcfix/s390x-linux-musl.s` carries the patched `__sigsetjmp`.
`pyBuild` assembles any `libcfix/<triple>.s` and puts it in `LIBS`, so the
explicit object defines the symbol and `libc.a(sigsetjmp.o)` is never pulled;
only that triple's key moves. The file must keep its `.note.GNU-stack` line,
or ld makes the whole interpreter's stack executable. Delete the file when the
toolchain is fixed. Not an `[expect.s390x]`.

## What we'd want upstream

- musl: use `jgz ___setjmp` (32-bit `brcl`) in `src/signal/s390x/sigsetjmp.s`;
  a short branch to a symbol in another translation unit has no range
  guarantee.
- binutils: report overflow for `R_390_PC16DBL` / `PC12DBL` / `PC24DBL`
  against the signed range.
