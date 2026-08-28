---
name: staticpy-traps
description: Symptom-to-cause catalogue for a static, cross-compiled, LTO'd CPython — builds that succeed while producing the wrong thing, configure guessing when it cannot run a test program, ctypes symbols that vanish, musl and libffi divergences, and the no-dlopen consequences that shape the whole design. Carries the full bug write-ups for the musl fma sign-of-zero bug, the mips64 libffi closure bug, and the toolchain portability proof. Read before debugging anything that builds but misbehaves, before adding a source fixup, and before trusting a green build. Every entry here cost real time to find.
---

# staticpy traps

Ordered by how long each took to corner, not by area. The common thread: on a
cross build almost nothing fails loudly, so the default failure mode is a
successful build of the wrong artifact.

## The long write-ups

Three findings needed more than a catalogue entry. They live beside this file,
and the entries below link to them at the point where they bite:

- **`MUSL_REPORT.md`** — musl's `fma` losing negative zero on underflow, and
  the two safety nets (the toolchain's missing `-mfma`, CPython's
  `linked_to_musl()` probe) that both fail specifically on a static non-PIE
  build.
- **`MIPS64_FFI_REPORT.md`** — libffi closures returning the high half of the
  return slot for narrow integers on big-endian mips n32/n64. Root-caused to
  `src/mips/n32.S`, fixed by a target-scoped patch, unreported upstream.
- **`PORTABILITY_PROOF.md`** — the end-to-end proof that a toolchain tarball
  drops onto a foreign glibc rootfs with no compiler on it and still builds
  through the LTO plugin path. Written against the older wrapper-based
  toolchain, so read its mechanism sections as history; the property still
  holds and `test-portability/` still checks it.

A new finding of that size goes here too, as a sibling file plus the entry that
points at it — not inlined into the source it explains.

## Silence is the enemy

**A recipe step fails and the build carries on.** In a Makefile, a recipe line
beginning with `-` means *ignore errors*. A configure line that loses its
trailing backslash leaves its remaining arguments as a new recipe line, and if
that line starts with `--`, make swallows the failure. This is not theoretical:
`libffi` was configured without `--enable-static --disable-shared` for fifteen
months this way, with `/bin/sh: exec-prefix=...: not found` sitting in a 2.4 MB
log nobody read. Symptom: a library builds, links, and is subtly wrong.
Countermeasure: staticpy's Runner checks every exit code and writes one log per
command; never reintroduce a path where output is only inspectable in aggregate.

**A `sed` fixup silently does nothing.** `sed -i '/anchor/...'` on a source tree
is a no-op when the anchor moves, and exits 0. Upstream reformats one line on a
patch release and you ship an interpreter whose `ctypes` is quietly broken. This
is why `config.Edit` asserts a match count and fails the job on a miss, and why
anything that does not need to survive a version bump should be a real diff
instead — `patch` already fails loudly on context mismatch.

**A build reuses an artifact built by a different compiler.** Job keys must fold
in the toolchain's identity — gccfactory's Merkle key from `.gccfactory.json`, or
a probe fingerprint when there is none. `pyhost` was the gap: its only dependency
is the srctree, so nothing else carried the compiler into its key and a toolchain
re-publish would silently reuse the old build-python. Any new job whose deps do
not transitively include a `dep` needs the identity folded in explicitly.

## configure cannot run a test program

This is the root of most cross-build wrongness. `AC_RUN_IFELSE` has a
cross-compiling fallback, and the fallback is a guess.

**`ac_cv_aligned_required` defaults to `yes` on a cross build** (the `no` branch
is Linux-android only). That is the safe direction but it changes the hash
function to FNV, so every little-endian target silently pays for a property it
does not have. Probed now; do not let it regress to a guess.

**Word and double endianness are separate questions.** `WORDS_BIGENDIAN` is the
integer byte order; `DOUBLE_IS_*_IEEE754` is the byte order of a stored double,
and they need not agree — the mixed-endian case exists because some ARM FPUs
disagreed with their CPU. autoconf's sentinel double is the one whose bytes spell
`noonsees` big-endian and `seesnoon` little; using the same constant is what
keeps our answer and configure's from diverging.

**The per-target pyconfig fragment wins over the probe.** That is deliberate —
it exists to state deliberate deviations — but it means a stale fragment
silently overrides a correct measurement. A blanket `#undef HAVE_GCC_ASM_FOR_X87`
inherited from a template did exactly that to i386, which has x87. The probe now
warns when a fragment contradicts it; a quiet run is the signal the fragment can
be deleted. If the hardware can tell you, it belongs in the probe, not the
fragment.

**The old cross path faked `config.status` and sed'd the native build's
Makefile.** Its `_sysconfigdata` was the native one with the triple substituted,
which left `HOST_GNU_TYPE: 'x86_64-pc-linux-musl'` intact (the `-pc-` infix
dodges the substitution) and would report `SIZEOF_VOID_P: 8` on every 32-bit
target. CPython 3.11+ has `--with-build-python`, `HOSTRUNNER` and `CONFIG_SITE`;
use them. Symptom of the old way: `sysconfig` values that are right only when the
host and target happen to agree.

## Per-package archaeology

**openssl's `Configure` reads `-static` in LDFLAGS as an instruction to turn
static off.** It runs `disable('static', 'pic', 'threads')`, which is the exact
opposite of what a static build means by that flag. Patched out.

**libffi closures return the wrong half of the slot on big-endian mips.** A
closure returning anything narrower than 64 bits hands back the *high* half of
the return slot, so every `ctypes` callback returning `c_int` is wrong —
`cb(42)` gives `0`, `cb(-7)` gives `-1`. The epilogue in `src/mips/n32.S` loads
from offset 0 of the slot, which is the value little-endian and the high half
big-endian, so it needs n32/n64 **and** big-endian: mips64el is unaffected,
which is why it has stood. `ffi_call` and 64-bit returns are both fine, which is
what narrows it. Fixed by a `target_patches` entry for `mips64-linux-musl`
alone; full write-up and reproducer in `MIPS64_FFI_REPORT.md`.

**zlib rejects `--host`.** Its configure is hand-rolled, errors on unknown
options, and reads `$CHOST` instead. Anything that passes `--host` uniformly has
to special-case it.

**`tic` cannot run on a cross build.** ncurses' install compiles the terminfo
database with a target binary. `TIC_PATH=true` skips it; the database is not what
we ship, `libncursesw.a` is.

**bzip2's `all:` runs its self-test** on binaries that cannot execute on the
build machine. Build the named archives instead of the default target.

**libuuid does not need util-linux's meson build.** It is fourteen C files —
eleven from `libuuid/src` (excluding `test_uuid.c`, which has a `main`) plus
`lib/randutils.c`, `lib/md5.c` and `lib/sha1.c`, which `gen_uuid.c` calls into.
Compiling them directly removes meson, ninja, flex and bison from the host
requirements.

**Install with `DESTDIR`, and set `--prefix` to the final artifact path.**
openssl and libtool bake their prefix into what they install; a pid-tagged
staging path is gone by the time anything reads the `.pc` or `.la` file. A binary
with a baked-in prefix cannot be rewritten afterwards and should be refused
rather than patched.

## No dlopen, and what follows

A static musl interpreter has no `dlopen`. Everything below is downstream of
that one fact.

**Every C extension is a builtin or absent.** No wheel with a compiled module
will ever import. numpy is out of reach and no amount of build engineering
changes that: meson, generated sources, f2py, a BLAS, and an assumption it can
produce loadable `.so` files.

**A dotted builtin is unreachable through the normal import path.**
`PyImport_Inittab` accepts `lxml.etree`, but `BuiltinImporter.find_spec` returns
`None` whenever a path is given, so submodule lookup never reaches the table. A
generated `site-packages` shim doing `sys.modules[...] = _mangled` is the
reliable fix.

**`ctypes.pythonapi` is a bespoke mechanism.** It resolves through a compiled-in
name→address table, not `dlsym`. Consequences: a symbol missing from
`Misc/stable_abi.toml` is missing from the table; data symbols need a different
path from functions, because `StaticCDLL.__getitem__` wraps every address in a
`_CFuncPtr`; and `py_object.in_dll(...)` goes through the real `dlsym` against a
handle stubbed to `0`, so it does not work at all. That last one is 143 symbols
of real API surface, every `PyExc_*` among them.

**The symbol table is a liveness anchor.** Taking `&name` for every entry is what
stops `-Wl,--gc-sections` from reaping the unreferenced half of the C API. It
cannot be trimmed as dead weight without losing symbols.

**`_ctypes_test` is a shared library** that ctypes' own tests `dlopen`. Those
tests cannot pass here, ever. Declare them, do not debug them.

## Generating the symbol table

**`abi_only` does not mean undeclared.** 23 of the 57 `abi_only` entries in
CPython 3.13 *are* declared in public headers, and emitting a synthetic
`extern void f(void);` for those is a conflicting-types compile error. Scan
`Include/*.h` and `Include/cpython/*.h` to find the genuinely undeclared set —
34, at 3.13.13 — and emit externs only for those.

**Emit `#ifdef` guards per entry, never hoisted into blocks.** The table has to
stay sorted whichever way a target resolves the guards, or `bsearch` silently
misses symbols. Hoisting is the optimisation that breaks it.

**The manifest hash cannot be computed at plan time.** Keys are computed before
the srctree dependency is materialised, so hashing `Misc/stable_abi.toml`
directly would differ between run 1 and run 2 and force a spurious rebuild. Let
it reach the key through the srctree dep instead.

## musl is not glibc

**`test_re`** fails on locale tests that need byte-level case folding musl does
not do. Alpine's apk skips it for the same reason.

**`fma` loses negative zero on underflow**, which `test_fma_zero_result` catches.
Upstream gates that test with a `linked_to_musl()` probe that shells out to
`ldd` — which exits non-zero on a fully static `-no-pie` binary, so the skip
never fires. Full write-up in `MUSL_REPORT.md`.

**Some targets need `-latomic`** for 64-bit atomics. Which ones is a property of
the target, not something to rediscover with a regex.

## Verification

**A static, stripped, non-PIE binary has no `.dynsym`.** `-Wl,--export-dynamic`
buys nothing there — no `PT_DYNAMIC`, zero dynamic symbols. Do not write a check
that expects one, and do not plan on reading your own symbol table at runtime.

**An unexpected pass has to fail the run.** A skip list that only grows is how a
suite quietly stops meaning anything; if musl fixes case folding you want to be
told, not to keep skipping `test_re` for another three years.

**Do not hide expectations behind `-x`.** A test excluded from the run can never
be discovered to be stale, which defeats the mechanism entirely. Run them and
judge the result.

**Expectations are per (target, runner).** qemu-user has its own failures around
signals, threads and subprocesses that say nothing about whether the build is
correct.

**Benchmarks under qemu measure qemu.** The overhead is not uniform across
workloads, so the numbers are comparable to nothing — not to native, and not to
each other.

**A toolchain that works on your box proves nothing about a foreign rootfs.**
The requirement is that a tarball drops onto *any* Linux rootfs — glibc,
near-empty, whatever — and compiles through `-flto -fuse-linker-plugin
-fno-fat-lto-objects`. Both halves come from gccfactory: every binary is
static-musl, and the LTO plugin is compiled into `libbfd` so `ld` resolves
`-plugin liblto_plugin.so` to its built-in copy rather than opening a file that
does not exist. `test-portability/proof.sh` checks it in a Debian image with no
compiler in it, negative control included; re-run it after every toolchain
re-publish. `PORTABILITY_PROOF.md` has the expected output and the
falsification controls.
