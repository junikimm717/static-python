---
name: staticpy-traps
description: Symptom-to-cause catalogue for a static, cross-compiled, LTO'd CPython — builds that succeed while producing the wrong thing, configure guessing when it cannot run a test program, ctypes symbols that vanish, musl and libffi divergences, and the no-dlopen consequences that shape the whole design. Opens with the anti-overfit rule (do not park a class-wide bug under one triple or one package stanza). Carries the full bug write-ups for the musl fma sign-of-zero bug, the mips64 libffi closure bug, the qemu 11 SAHF bug, the libatomic fork hang, and the toolchain portability proof. Read before debugging anything that builds but misbehaves, before adding a source fixup or an [expect], and before trusting a green build. Every entry here cost real time to find.
---

# staticpy traps

On a cross build almost nothing fails loudly: the default failure is a
successful build of the wrong artifact. Entries are symptom-first.

## Do not overfit the last failure

Agent time is cheap; a verify+pack sweep is not. An `[expect.<this-triple>]` or
`[package.X.profile.this]` that makes *this* run green makes the next target,
package or qemu version pay the compile again. Catch the **class** first:

1. Reproduce on a second arm (native x86_64 static, another qemu, host vs
   container qemu). If it fails there too, the scope is the class, not the
   triple last in the log.
2. Name the layer: recipe invariant, emulator *version*, ABI, or one library's
   configure. Hunt until the complete fix fits in one sentence. An ignore is
   allowed only when that sentence is "unfixable, and here is the experiment
   that proves it."
3. Don't scope an ignore to one triple to avoid re-keying already-packed
   verifies. That lies to the next `status --target all`.
4. `[expect.qemu]` / `:qemu` is for emulator gaps you've seen pass *natively*,
   not "qemu 11.0.1 on this laptop". If the fix is a newer qemu, pin it and
   delete the ignores.
5. A second `[package.X.profile.seplto]` stanza for a prefix baked into an
   `.a` means the recipe has no policy; the next library fails the same
   sysroot check. Fix the class.

Past parking tickets now have complete fixes; don't put them back: Linux
getpath reads `/proc/self/exe`; per-dep LTO deps configure with `/usr` and the
recipe rewrites `.pc`/`.la`; `libat_atfork.c` replaces gcc's lock table; the
image pins qemu-user 11.1.1 and shims Ubuntu `*-binfmt-P` names to it.

## Long write-ups in `references/`

- **`references/MUSL_REPORT.md`** — musl `fma` loses negative zero on
  underflow; both safety nets (toolchain's missing `-mfma`, CPython's
  `linked_to_musl()` probe) fail specifically on a static non-PIE build.
- **`references/MIPS64_FFI_REPORT.md`** — libffi closures return the high half
  of the return slot for narrow integers on big-endian mips n32/n64.
  Root-caused to `src/mips/n32.S`, fixed by a target-scoped patch, unreported
  upstream.
- **`references/I386_QEMU_SAHF.md`** — qemu 11.0 SAHF/cc_op makes i386 CPython
  compares take the wrong branch.
- **`references/LIBATOMIC_FORK.md`** — two lock tables with no atfork: gcc
  libatomic (C probe) and mimalloc `mi_lock_t` (CPython test on default eabi).
  Fix is spinlocks + pid-steal — not an `[expect]`, not `-march=armv7`, not
  "skip mimalloc on eabi".
- **`references/PYREF_RPATH.md`** — two host compilers wrote one pyref prefix;
  Alpine `-lreadline` then opened a leftover glibc `libncursesw` because
  `-rpath` named that prefix. Key the publish path on hostcc. Shadow rpath is a
  separate isolation fix, not what produced `@GLIBC`.

**Adding a finding:** a paragraph-sized one is an entry in the matching
section below, phrased by what you'd have searched for. One needing a
reproducer, disassembly, before/after numbers or layered root cause becomes a
new `references/` file plus a short linking entry here. Never leave it only in
a commit message, scratch file or source comment.

## Silence is the enemy

**A recipe step fails and the build carries on.** A Makefile recipe line
starting with `-` ignores errors. A configure line that loses its trailing
backslash turns the remaining args into a new recipe line; starting with `--`,
make swallows the failure. `libffi` was configured without
`--enable-static --disable-shared` for fifteen months this way, with
`/bin/sh: exec-prefix=...: not found` in a 2.4 MB log. Symptom: a library
builds, links, and is subtly wrong. Countermeasure: the Runner checks every exit
code and logs per command; never reintroduce aggregate-only output.

**`expr: not found`, then `fcntl: No file descriptors available`.** configure
looks up `expr` on PATH; a missing applet plus the default 1024 nofile exhausts
fds on retries. Compose `nofile` and put `expr`/`awk`/`tr` on the process PATH
(e.g. the image's `busybox --install -s /bin`). Don't invent a second PATH
policy.

**`env: can't execute 'perl'` on openssl Configure.** Same class:
`#!/usr/bin/env perl` searches the job PATH. Doctor already requires perl;
`perl ./Configure` is a local extra, not a substitute.

**riscv64 core verify: ten files fail with `FileNotFoundError: …/bin/python3`.**
Smoke passes (staticpy prefixes `qemu-riscv64`), but anything that `exec`s
`sys.executable` goes through the host kernel's binfmt table (global,
first-writer). Host `qemu-user-binfmt` registers
`/usr/libexec/qemu-binfmt/<arch>-binfmt-P`; Dockerfile shims make that path
exist in the container for the qemu list in `scripts/docker/qemu-user.sh`. A
new arch, stale image, Fedora's `qemu-<arch>-static` names, or CI's host verify
all miss it. Refuse to verify unless the registered interpreter exists in this
mount namespace. Don't wrap CPython's re-exec. Not an `[expect]`.

**`test_subprocess.test_executable_without_cwd` fails with missing encodings.**
`Popen(["somethingyoudonthave"], executable=sys.executable)` leaves a fake
argv[0]. Linux `getpath.c` left `real_executable` None and fell back to the
compile-time PREFIX. The python patch fills it from `readlink("/proc/self/exe")`
(as Windows/macOS already do). If it returns after a CPython bump, the patch
failed to apply; don't add `[expect.static]`.

**i386 core verify: `test_divmod` / `test_math` / `test_struct` fail under qemu.**
qemu 11.0 TCG leaves `cc_op` stale after `SAHF` (`da7649c6`, GitLab #3537),
fixed in 11.0.4 / 11.1. The image builds qemu-user **11.1.1** from
download.qemu.org, not Ubuntu 24.04's 8.2.2. If these fail again the image is
on 11.0.x: upgrade qemu, don't restore the `:qemu` ignores.
`*MathTests.testSinh*` is a real x87 1-ulp ABI issue and stays on
`[expect.i386-linux-musl]`. Write-up: `references/I386_QEMU_SAHF.md`.

**`suite:test_os` unexpected fail under qemu: `test_fork_warns_when_non_python_thread_exists`.**
Binfmt class again. CPython reads `/proc/self/stat` field 20 (`num_threads`),
which qemu < 9.1 (host Ubuntu 8.2.2) leaves `0`. A missing shim falls through to
host qemu; a present one keeps re-exec on the image's 11.1.1. Doctor must require
the binfmt interpreter to exist *and* be ≥ 9.1. Don't restore `[expect.qemu]`.

**`suite:test_threading` SIGSEGV on s390x: `test_recursion_limit` (Issue 9670).**
The recursing subprocess thread should raise RecursionError; qemu-s390x dies
`uncaught target signal 11`. Not qemu, not libatomic: the script exits 0 on
aarch64/i386/mips64/ppc64le qemu and native x86_64 static; main-thread recursion
on s390x is fine; `threading.stack_size(8<<20)` and `sys.setrecursionlimit(100)`
both pass. musl's default pthread stack is **128 KiB on every triple**
(measured `pthread_attr_getstacksize` under qemu-s390x and qemu-aarch64).
CPython 3.14 skips `pthread_getattr_np` on musl and guesses `Py_C_STACK_SIZE`
(320000 on `__s390x__`, 4 MiB elsewhere); s390x frames are big enough that 1000
calls overflow 128 KiB before that or `py_recursion_remaining` fires. Fix:
`THREAD_STACK_SIZE 0x400000` in the s390x pyconfig fragment (what CPython
already does for FreeBSD/AIX). Don't add `[expect.s390x]`. Watch powerpc64
(`Py_C_STACK_SIZE` 2 MiB): if it SIGSEGVs, same define, not an ignore.

**sqlite configure: `s390x-binfmt-P: Could not open '/lib/ld-musl-s390x.so.1'`.**
sqlite's configure builds a bootstrap `jimsh0` with the *target* CC and runs
it; host binfmt intercepts the cross ELF. `B.cc=@BUILD_CC@` is make-only. A host
`jimsh` on PATH skips the bootstrap: `/bin/jimsh` in the Dockerfile, same class
as `expr`/`perl`. Without it the message is often `./jimsh0: not found`, then
`No working C compiler found`. Don't make qemu able to run configure tests.

**pyref sqlite configure: `Cannot find a tclsh to use for code generation`.**
sqlite 3.51 autosetup wants `tclsh`, not `jimsh`; `jimtcl` covered the static
`jimsh0` skip but pyref died on the host gcc path. `apt install tcl` and
`/bin/tclsh` in the Dockerfile. Don't pass `--disable-tcl` (drops the
amalgamation codegen).

**`suite:test_bytes` unexpected pass on a reference arm.** `expect.static` skips
`test_bytes` because `_testlimitedcapi` can't be a builtin or a dlopen.
LookupExpect used to merge that scope for every interpreter; a host-built
reference *can* dlopen, so it passed and failed the run. Merge `expect.static`
only for non-host-built profiles.

**`elf:static` / missing Py_GetVersion on a published reference.** Verify
assumed every interpreter was fully static. A host-built reference has
PT_INTERP and keeps the C API in libpython.so; skip (or invert) those checks
for host-built profiles.

**`verify: bin/python3 is not a file` on a reference arm.** pyref publishes
`rootfs/bin/python3`, pynative `bin/python3`. Pack and bench unwrapped
`rootfs/`; verify didn't. Unwrap it the same way.

**`lto1: Cannot open Modules/_cursesmodule.o` during a CPython LTO link.**
The other builder's `GCStale` deleted this job's live `dist/work`. `kill(pid, 0)`
is PID-namespace local; the container and kitbuild share `dist/`, and after
`StaleAge` (10 min, shorter than `-flto-partition=none` WPA) each treats the
other's scratch as dead. gcc 16 reports the next `open` as `Cannot open %s`
with no errno (`_ssl.o`, `blob.o`, `odictobject.o` — whichever was next). Don't
serialize `make`. GC must skip a scratch dir whose heartbeat is `Live()` (which
already accepts a recent `UpdatedAt` across machines). A watchdog restarting on
`Cannot open` as a flake feeds the bug.

**seplto python link: `multiple definition of BZ2_*` / `lzma_*`.**
`materializeArchives` LTO-rels each `.a` into one relocatable, then `ar rcs`
*added* it as a member; the original IR objects stayed, so the link saw
`lib_libbz2.a.o` and `bzlib.o`. Slim LTO hid it (WPA merged duplicates).
Replace the archive; don't append.

**`lib64/libcrypto.a records a dependency prefix` on seplto (or any new
library with a baked LOCALEDIR / ICU_DATA).**
`lto_mode = per-dep` materializes `--prefix` strings in the `.a`. The recipe
configures every non-host per-dep dep with `/usr`, hoists `DESTDIR+/usr`, and
rewrites `.pc`/`.la`/`*-config` back to the artifact path. OpenSSL's cert dir is
`--openssldir=/etc/ssl` on every static build, not a seplto stanza. Don't add
`[package.X.profile.seplto]`; don't byte-replace OPENSSLDIR (a length change
corrupts `.rodata`). `--disable-database` on base ncurses is the
terminfo-specific extra, not the generic lever.

**`lib/libncursesw.a records a dependency prefix inside a binary file` on seplto.**
Same class: per-dep LTO makes the terminfo path contiguous. Base ncurses is
`--disable-database` (TIC_PATH=true, nothing baked). The prefix policy above
stops the next package.

**`libformw.so needs libncursesw.so.6 but has no RUNPATH`.** The `$ORIGIN`
rewrite only shrinks an existing DT_RPATH/DT_RUNPATH; it can't add one.
ncurses' form/menu/panel NEEDED libncursesw but were linked with `-rpath-link`
only. Host-built LDFLAGS now also bake `-Wl,-rpath,<prefix>/lib`, giving the
rewrite something longer than `$ORIGIN` to overwrite.

**`libffi built without producing lib64/libffi.so` on Alpine.** libffi's
`toolexeclibdir` follows `$CC -print-multi-os-directory`. Flipping `provides` to
`lib/libffi.so` lets an Alpine reference succeed and silently measure musl, and
breaks on a `../lib64` gcc. Own the install dir (`--disable-multi-os-directory`)
so `provides = lib/libffi.so` holds on every host. Host-built libc is whatever
`hostcc` fingerprints: don't refuse musl, don't treat `lib64` as a canary, don't
let `kitFactors` hardcode `glibc`. "Build reference on a glibc host for a glibc
baseline" is docs, not a recipe refusal.

**pyref libffi: `Something went wrong bootstrapping makefile fragments`.**
libffi's bootstrap needs GNU make on PATH (the image has it). Don't put a musl
toolchain first on a host-built job — gcc resolves `ld` from PATH, not `$LD`.

**A `sed` fixup silently does nothing.** `sed -i '/anchor/...'` no-ops and exits
0 when the anchor moves; one reformatted line upstream ships a quietly broken
`ctypes`. Hence `config.Edit` asserts a match count, and anything not needing
to survive a version bump should be a real diff (`patch` fails on context
mismatch).

**A build reuses an artifact built by a different compiler.** Keys must fold in
the toolchain identity: gccfactory's Merkle key from `.gccfactory.json`, or a
probe fingerprint. `pyhost` was the gap — its only dep is the srctree, so a
toolchain re-publish silently reused the old build-python. Any job whose deps
don't transitively include a `dep` needs the identity folded in explicitly.

**A hybrid machine reports itself uniform, and every guard downstream stops
working.** Core type was read from `cpu_capacity`, falling back to
`cpufreq/cpuinfo_max_freq`. On a Zen 5 + Zen 5c laptop `cpu_capacity` is a flat
1024 on all 24 threads while `cpuinfo_max_freq` separates 5157895 from 3289474,
so: `Topology.Hybrid` false, `Fastest()` returned every CPU, the bench menu
showed one block, and the "not in the fastest core class" warning could never
fire (`--cpu 7` silently got a 3.29 GHz core; it landed on a fast one only
because CPPC `highest_perf` happens to correlate, 208 vs 125). `cpu_capacity` is
right on ARM big.LITTLE, so `classSource` picks whichever source discriminates
on this machine. Symptom: `bench` reports `uniform` on a machine you know isn't,
or no core is ever marked slow.

**`Could not find a version that satisfies the requirement setuptools>=61` on
`./run`.** Kit `./run` is `--no-index --find-links vendor --no-deps`. `--no-deps`
skips pyperformance's runtime deps (psutil), not PEP 517. Both sdists require
`setuptools>=61`; the isolated build sees only those two tarballs; 3.13
ensurepip no longer seeds setuptools. Fails in one second at
`install pyperformance`. Vendor a setuptools wheel alongside and hash vendor
pins into the kit key. `--no-build-isolation` alone doesn't fix it — the venv
still lacks setuptools.

## Host-built profiles: the shared-prefix build

Found building `pyref` (`--profile reference`, the dynamic interpreter from the
machine's gcc). None can happen to the static build, which is why they hid.

**A dependency prefix baked into a shared library.** libtool writes an RPATH,
OpenSSL OPENSSLDIR, ncurses its terminfo dir — correct only if `--prefix` is
where files finally live, so a host-built profile builds every dep into one
shared rootfs. The static build never notices: nothing resolves paths at run
time, and a slim-LTO archive doesn't hold the string contiguously, so the
sysroot scan can't see it (`strings libncursesw.a` returning nothing is that,
not absence).

After install, pyref rewrites every ELF RUNPATH to `$ORIGIN`-relative (in-place
shrink of the baked string), which makes the rootfs copyable. Don't
byte-replace OPENSSLDIR (length change corrupts `.rodata`). ncurses on
`reference` is `--disable-database`, so no terminfo path. `$ORIGIN` resolves via
`/proc/self/exe`, so a venv symlink still finds libpython.

**`ld` will not use `-L` to resolve a shared library's own `NEEDED` entries.**
It uses the library's `RUNPATH`, then `-rpath`, then `-rpath-link`. CPython's
readline check is `-lreadline` only, so `libncursesw` goes through that walk,
while direct `-lncursesw` (initscr, `_curses`) uses `-L` and succeeds. Symptom:
`checking for readline in -lreadline... no` with the header found and
`initscr` linked. The crash seen (`__memset_chk@GLIBC_2.3.4`) was a **glibc
leftover** at `pyref_reference_x86_64-linux-musl` after the host became Alpine:
two host compilers sharing `dist/` wrote one prefix (a same-toolchain leftover
would have been musl and linked). Key the publish path on hostcc (`_<12 hex>`)
and refuse a prefix whose manifest toolchain differs. Host-built `-rpath` still
names the shadow/view, not the published prefix: a previous artifact is the
wrong generation even with matching libc (same SONAME, new symbol), and the
shadow is longer than `$ORIGIN` so the rewrite fits. Don't skip readline; don't
delete the published tree as the fix. Write-up: `references/PYREF_RPATH.md`.

**gcc resolves `ld` off the command PATH, not `$LD`.** A provisioned musl
toolchain first on PATH makes the host compiler link glibc objects with a musl
linker: `undefined reference to 'floor@GLIBC_2.2.5'`, `libm.so.6 ... not found`.
Host-built profiles name no target when composing PATH.

**An import check that passes against the host's libraries.** CPython imports
every extension it builds. Without `LD_LIBRARY_PATH` on the staged rootfs the
loader finds the distro's `/lib64/libssl.so.3`, `libncursesw.so.6`,
`libz.so.1` and reports success. Only sqlite failed, because our SONAME is
unversioned (`libsqlite3.so`) and the host ships `libsqlite3.so.0`.

**The static build's ctypes edits leak through the shared srctree.** `pythonapi`
is rebound to the generated `staticapi` table and `dlopen` stubbed to a lambda
(no libdl when static). A dynamic build inherits both and dies on
`import ctypes` with `No module named 'staticapi'`. `pyref` restores stock
ctypes in its own copy and asserts the match count, so an upstream move fails
loudly.

**`--with-lto --enable-optimizations` in CONFIG_ARGS, no `-flto` in the binary.**
CPython 3.13's configure matches `*gcc*` against `$CC` / `CC_BASENAME` for LTO,
PGO and `-fno-semantic-interposition`. `hostcc.Find` preferred `cc`; on Ubuntu
that's `/usr/bin/cc` → gcc, the glob misses, and `profile-opt` runs two
uninstrumented builds. The 3.13 kit advertised both and shipped neither
(`reference` and `reference-nolto` differed by 8 bytes). 3.14 switched to
`ac_cv_cc_name` ([gh-96398](https://github.com/python/cpython/issues/96398)),
so the same recipe started working. `hostcc` now prefers `gcc`, and pyref
refuses a Makefile that requested LTO/PGO without getting the flags. Those 3.13
sessions are gone; not a baseline.

**Countermeasure for all of the above:** `pyref` imports every module that
exists only because a dependency was built, *including Python-level wrappers*
(`ctypes` failed while `_ctypes` imported — that reached a published artifact).
The check runs on the staged tree with `$ORIGIN` RUNPATHs and must **not** set
`LD_LIBRARY_PATH`, which would mask a failed rewrite. `make` still needs
`LD_LIBRARY_PATH` because the in-tree binary's rpath names the unpublished
prefix.

## configure cannot run a test program

The root of most cross-build wrongness: `AC_RUN_IFELSE`'s cross fallback is a
guess.

**`ac_cv_aligned_required` defaults to `yes` on a cross build** (`no` only on
Linux-android). Safe, but it switches the hash to FNV, so every little-endian
target silently pays. Probed now; don't let it regress to a guess.

**Word and double endianness are separate questions.** `WORDS_BIGENDIAN` is
integer order; `DOUBLE_IS_*_IEEE754` is stored-double order; they can differ
(some ARM FPUs). Use autoconf's sentinel double (bytes spell `noonsees`
big-endian, `seesnoon` little) so our answer and configure's can't diverge.

**The per-target pyconfig fragment wins over the probe.** Deliberate (it states
deviations), but a stale fragment silently overrides a correct measurement: a
templated `#undef HAVE_GCC_ASM_FOR_X87` did that to i386, which has x87. The
probe warns on contradictions; a quiet run means the fragment can go. If the
hardware can answer it, it belongs in the probe.

**The old cross path faked `config.status` and sed'd the native build's
Makefile.** Its `_sysconfigdata` was the native one with the triple
substituted, leaving `HOST_GNU_TYPE: 'x86_64-pc-linux-musl'` (the `-pc-` infix
dodges substitution) and `SIZEOF_VOID_P: 8` on every 32-bit target. Use
CPython 3.11+'s `--with-build-python`, `HOSTRUNNER`, `CONFIG_SITE`. Symptom:
`sysconfig` values right only when host and target agree.

## Per-package archaeology

**openssl's `Configure` reads `-static` in LDFLAGS as an instruction to turn
static off.** It runs `disable('static', 'pic', 'threads')`. Patched out.

**libffi closures return the wrong half of the slot on big-endian mips.** A
closure returning <64 bits hands back the *high* half, so every `c_int` ctypes
callback is wrong (`cb(42)` → `0`, `cb(-7)` → `-1`). The `src/mips/n32.S`
epilogue loads offset 0 of the slot: the value on little-endian, the high half
on big-endian. Needs n32/n64 **and** big-endian (mips64el is fine); `ffi_call`
and 64-bit returns are fine. Fixed by a `target_patches` entry for
`mips64-linux-musl` only; see `references/MIPS64_FFI_REPORT.md`.

**`nomimalloc` still linking mimalloc.** Sysroot composed every package
regardless of `skip`, so `lib/mimalloc.o` reached pynative's `LIBS=` and the
allocator axis was a no-op. Skip is now filtered in Sysroot and Deps;
`depBuilder.job` doesn't filter, so a `Needs` edge to a skipped package fails
instead of vanishing.

**`reference-mimalloc` still using glibc malloc.** Un-skipping mimalloc on a
host-built profile does nothing unless the `.o` is on the python link line.
musl's malloc is weak, so a strong `mimalloc.o` in `LIBS=` wins; glibc's is
strong in libc.so, and the override is ELF interposition of a malloc defined in
the executable — the same `LIBS=` path as pynative, not a shared libmimalloc.
Symptom: `ldd` shows no mimalloc (expected) and allocations match glibc; check
python-configure's argv has `LIBS=.../mimalloc.o`.

**Localise before `ld -r`, never after.** mimalloc ships as one relocatable
object with all but the allocator entry points local. Doing that with
`objcopy --keep-global-symbols` *after* the relocatable link gives a mips64
interpreter that SIGBUSes before `main`: `R_MIPS_GOT16` and `CALL16` mean
different things for global vs local symbols, so rebinding afterwards makes the
linker read them under the wrong rule. Other arches tolerate either order.
objcopy each object first, then merge. Cost: `keep_globals` must cover any
symbol referenced across the package's own objects (a loud final-link failure
otherwise).

**Merge relocatable objects with the compiler driver, not `ld`.** Bare `ld -r`
picks its own default emulation — mips64 defaults to n32 and rejects n64 input
("ABI is incompatible with that of the selected emulation"). `gcc -r -nostdlib`
passes the right `-m` and is byte-identical to `ld -r` wherever that worked.

**zlib rejects `--host`.** Hand-rolled configure, errors on unknown options,
reads `$CHOST`. Uniform `--host` passing must special-case it.

**`tic` cannot run on a cross build.** ncurses' install compiles terminfo with a
target binary. `TIC_PATH=true` skips it; we ship `libncursesw.a`, not the
database.

**bzip2's `all:` runs its self-test** on binaries that can't run on the build
machine. Build the named archives instead.

**libuuid does not need util-linux's meson build.** It's fourteen C files:
eleven from `libuuid/src` (excluding `test_uuid.c`, which has a `main`) plus
`lib/randutils.c`, `lib/md5.c`, `lib/sha1.c` (called by `gen_uuid.c`).
Compiling them directly drops meson, ninja, flex and bison from host
requirements.

**Install with `DESTDIR`, and set `--prefix` to the final artifact path.**
openssl and libtool bake their prefix; a pid-tagged staging path is gone by the
time anything reads the `.pc`/`.la`. A binary with a baked prefix can't be
rewritten afterwards; refuse it rather than patch it.

## No dlopen, and what follows

A static musl interpreter has no `dlopen`; everything here follows from that.

**Every C extension is a builtin or absent.** No compiled wheel will import.
numpy is out of reach regardless of build engineering (meson, generated
sources, f2py, a BLAS, and loadable `.so` assumptions).

**A dotted builtin is unreachable through the normal import path.**
`PyImport_Inittab` accepts `lxml.etree`, but `BuiltinImporter.find_spec`
returns `None` whenever a path is given, so submodule lookup never reaches it.
Fix: a generated `site-packages` shim doing `sys.modules[...] = _mangled`.

**`ctypes.pythonapi` is a bespoke mechanism.** It resolves through a
compiled-in name→address table, not `dlsym`. So: a symbol missing from
`Misc/stable_abi.toml` is missing from the table; data symbols need a separate
path because `StaticCDLL.__getitem__` wraps every address in a `_CFuncPtr`; and
`py_object.in_dll(...)` calls real `dlsym` on a handle stubbed to `0`, so it
doesn't work at all — 143 symbols of API, every `PyExc_*` included.

**The symbol table is a liveness anchor.** Taking `&name` per entry stops
`-Wl,--gc-sections` reaping the unreferenced C API. Trimming it loses symbols.

**`_ctypes_test` is a shared library** that ctypes' tests `dlopen`. They can
never pass here. Declare them; don't debug them.

**`test_ctypes.test_dllist.test_lists_system` fails with `loaded=['/proc/self/exe']`.**
3.14's `ctypes.util.dllist` (`dl_iterate_phdr`) test wants `libc.so` or
`libffi.so`; a fully static binary has only the executable phdr. Dynamic
reference arms pass. This is `[expect.static]`, not `[expect.<triple>]`, not
qemu. Ignore glob is `*test_ctypes.test_dllist*` (the module);
`*test_dllist.test_lists_system*` doesn't match because regrtest fnmatch never
sees those names adjacent. `test_lists_updates` needs `_ctypes_test` and skips.

## Generating the symbol table

**`abi_only` does not mean undeclared.** Many are declared in public headers,
and a synthetic `extern void f(void);` for those is a conflicting-types error.
Scan the headers `Python.h` actually reaches and emit externs only for the
genuinely undeclared.

**A public header that `#define`s a stable-ABI function hides the `PyAPI_FUNC`.**
3.14's `Py_PACK_FULL_VERSION` is declared, then `#define`d to a function-like
macro, so `EXPORT_FUNC`'s `&name` becomes `&_Py_PACK_FULL_VERSION` and fails
(`undeclared here (not in a function)`). `dump_stable_abi.py` marks names that
are both `#define` and `PyAPI_FUNC`; the generator `#undef`s them. Don't
special-case one symbol; the next header wrapper hits the same hole.

**Emit `#ifdef` guards per entry, never hoisted into blocks.** The table must
stay sorted however a target resolves the guards, or `bsearch` silently misses
symbols.

**The manifest hash cannot be computed at plan time.** Keys are computed before
the srctree is materialised, so hashing `Misc/stable_abi.toml` directly differs
between runs 1 and 2 and forces a rebuild. Let it reach the key through the
srctree dep.

## musl is not glibc

**`test_re`** fails on locale tests needing byte-level case folding musl lacks.
Alpine's apk skips it too.

**`fma` loses negative zero on underflow** (caught by `test_fma_zero_result`).
Upstream's `linked_to_musl()` gate shells out to `ldd`, which exits non-zero on a
fully static `-no-pie` binary, so the skip never fires. See
`references/MUSL_REPORT.md`.

**Some targets need `-latomic`** for 64-bit atomics — a property of the
target (the `libatomic` field), not something to rediscover with a regex.

## Verification

**A static, stripped, non-PIE binary has no `.dynsym`.** `-Wl,--export-dynamic`
buys nothing (no `PT_DYNAMIC`). Don't write a check expecting one or plan to
read your own symbol table at runtime.

**An unexpected pass has to fail the run.** A skip list that only grows stops
meaning anything; if musl fixes case folding you want to know.

**Do not hide expectations behind `-x`.** An excluded test can never be found
stale. Run it and judge the result.

**Expectations are per (target, runner).** qemu-user fails around signals,
threads and subprocesses in ways that say nothing about the build.

**Benchmarks under qemu measure qemu.** The overhead is non-uniform, so the
numbers compare to nothing — not native, not each other.

**A hung forked child is libatomic then mimalloc, not qemu.** `test_threading`
reports *env changed* with children "still running after 300.5 seconds"
(`ThreadJoinOnShutdown.test_reinit_tls_after_fork`). Two lock tables, same
missing atfork:

1. gcc `libatomic`: 64 `pthread_mutex_t`, no `pthread_atfork`. The recipe links
   `libat_atfork.o` *before* `-latomic`: spinlocks + child-only zero (pid-steal,
   so a late atomic in the child doesn't wait for our handler). Fixes the C
   class: stock `-latomic` hangs 16-fork + 64-bit add on eabi, i386 and rv32.
2. mimalloc `mi_lock_t`: also `pthread_mutex_t` (`MI_USE_PTHREADS` stays on for
   TLS keys); `mi_process_init()` is a no-op after fork. nomimalloc eabi was 3/3
   SUCCESS; default eabi with the libatomic fix still hung 3/3 until
   `patches/mimalloc-*/0001-fork-safe-locks.diff` swapped those mutexes for the
   same pid-steal spinlocks. Now 3/3 in ~0.24 s.

Reap leftover qemu children in the container before calling the next run a
flake. See `references/LIBATOMIC_FORK.md`. If the C hang returns, the `.o` is
after `-latomic` or was built with `-flto`. Don't restore `[expect]`, don't
raise arm-eabi to armv7 (hides layer 1 like armhf does, doesn't help rv32),
don't skip mimalloc on one triple.

**A toolchain that works on your box proves nothing about a foreign rootfs.**
A tarball must drop onto *any* Linux rootfs (glibc, near-empty) and compile
through `-flto -fuse-linker-plugin -fno-fat-lto-objects`. gccfactory provides
both halves: every binary is static-musl, and the LTO plugin is compiled into
`libbfd` so `ld` resolves `-plugin liblto_plugin.so` to its built-in copy. The
proof belongs in gccfactory; here a failure shows as a link that can't find
`liblto_plugin.so`, or a driver that won't start.
