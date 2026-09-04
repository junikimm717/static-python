---
name: staticpy-traps
description: Symptom-to-cause catalogue for a static, cross-compiled, LTO'd CPython — builds that succeed while producing the wrong thing, configure guessing when it cannot run a test program, ctypes symbols that vanish, musl and libffi divergences, and the no-dlopen consequences that shape the whole design. Opens with the anti-overfit rule (do not park a class-wide bug under one triple or one package stanza). Carries the full bug write-ups for the musl fma sign-of-zero bug, the mips64 libffi closure bug, the qemu 11 SAHF bug, the libatomic fork hang, and the toolchain portability proof. Read before debugging anything that builds but misbehaves, before adding a source fixup or an [expect], and before trusting a green build. Every entry here cost real time to find.
---

# staticpy traps

Ordered by how long each took to corner, not by area. The common thread: on a
cross build almost nothing fails loudly, so the default failure mode is a
successful build of the wrong artifact.

## Do not overfit the last failure

Agent time is cheap. A verify+pack sweep is not. An `[expect.<this-triple>]`
or `[package.X.profile.this]` that makes *this* run green is how the next
target, the next package, or the next qemu version pays the compile again.

Catch the **class** of the bug before you write the parking ticket:

1. Reproduce on a second arm (native x86_64 static, another qemu, host qemu
   vs the container's qemu). If it fails there too, the scope is the class,
   not the triple that happened to be last in the log.
2. Name the layer: recipe invariant, emulator *version*, ABI, or one
   library's configure. Hunt until you can state the complete fix in one
   sentence. An ignore is allowed only when that sentence is "unfixable,
   and here is the experiment that proves it."
3. Do not scope an ignore to one triple to avoid re-keying already-packed
   verifies. That is a lie to the next `status --target all`.
4. `[expect.qemu]` / `:qemu` is for emulator gaps you have seen pass
   *natively*. It is not a bin for "qemu 11.0.1 on this laptop." If the
   fix is a newer qemu, pin the qemu and delete the ignores.
5. A second `[package.X.profile.seplto]` stanza for a prefix baked into an
   `.a` means the recipe has no policy. The next library fails the same
   sysroot check. Fix the class.

Those overnight parking tickets now have complete fixes. Do not put them
back: Linux getpath reads `/proc/self/exe`; per-dep LTO deps configure
with `/usr` and the recipe rewrites `.pc`/`.la`; `libat_atfork.c`
replaces gcc's lock table; Alpine qemu is pinned to 11.1.1 and Ubuntu
`*-binfmt-P` names are shimmed to Alpine's qemu.

## The long write-ups, in `references/`

Four findings needed more than a catalogue entry. The entries below link to
them at the point where they bite:

- **`references/MUSL_REPORT.md`** — musl's `fma` losing negative zero on
  underflow, and the two safety nets (the toolchain's missing `-mfma`,
  CPython's `linked_to_musl()` probe) that both fail specifically on a static
  non-PIE build.
- **`references/MIPS64_FFI_REPORT.md`** — libffi closures returning the high
  half of the return slot for narrow integers on big-endian mips n32/n64.
  Root-caused to `src/mips/n32.S`, fixed by a target-scoped patch, unreported
  upstream.
- **`references/I386_QEMU_SAHF.md`** — qemu 11.0 SAHF/cc_op making i386 CPython
  compares take the wrong branch.
- **`references/LIBATOMIC_FORK.md`** — two lock tables with no atfork:
  gcc libatomic (C probe) and mimalloc `mi_lock_t` (CPython test on
  default eabi). Spinlocks + pid-steal, not an `[expect]`, not
  `-march=armv7`, not "skip mimalloc on eabi".

**When you corner a failure worth documenting, write it up here.** A paragraph
is enough for most of them: add an entry to the matching section below,
phrased symptom-first so the next person finds it by what they would have
searched for. When it needs a reproducer, a disassembly, measured before/after
numbers, or layers of root cause — anything that would swamp a catalogue entry
— it becomes a new file in `references/`, and you add the short entry below
that links to it. Do not leave the long version in a commit message, a scratch
file, or the source it explains; none of those get read before the next person
hits the same wall.

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

**`expr: not found`, then `fcntl: No file descriptors available`.** Hermetic
PATH is toolchain + `dirname(busybox)`, which is not a closed toolbox.
Alpine keeps `expr`/`awk`/`tr`/`basename` under `/usr/bin` only; CI's
`/usr/local/bin/busybox` has none of them. The Dockerfile
`busybox --install -s /bin` plus compose `nofile` is image provisioning,
not the class. Materialize `dist/.bin/hermetic/` (`busybox --install -s`
plus LookPath symlinks) and put **that** dir on PATH — not `/usr/bin`,
not `dirname(busybox)`. Doctor must resolve `expr` on the composed
hermetic PATH. Do not "fix" this with `--no-hermetic`.

**`env: can't execute 'perl'` on openssl Configure.** Same class as
`expr`. `#!/usr/bin/env perl` looks up `perl` on hermetic PATH; doctor
LookPaths the parent PATH and reports green while Configure dies. A
`/bin/perl` symlink is the Alpine layout, not the class. Symlink
LookPath(perl) into the hermetic bin. `perl ./Configure` is a local
extra, not a substitute.

**riscv64 core verify: ten files fail with `FileNotFoundError: …/bin/python3`.**
Smoke probes pass (staticpy prefixes `qemu-riscv64`). Anything that
`exec`s `sys.executable` is the host kernel's binfmt table, which is
global and first-writer. Host `qemu-user-binfmt` registers
`/usr/libexec/qemu-binfmt/<arch>-binfmt-P`. Dockerfile shims make that
path exist *inside spython* for today's apk list; a new arch, a stale
image, Fedora's `qemu-<arch>-static` names, or CI's host verify all
miss. Refuse to verify unless the registered interpreter exists in
this mount namespace. Do not wrap CPython's re-exec. Not an `[expect]`.

**`test_subprocess.test_executable_without_cwd` fails with missing encodings.**
`Popen(["somethingyoudonthave"], executable=sys.executable)` leaves argv[0]
as a fake name. Linux `getpath.c` used to leave `real_executable` as None
and fall back to the compile-time PREFIX. The python patch fills that
slot from `readlink("/proc/self/exe")`, the same slot Windows/macOS
already fill. If this returns after a CPython bump, the patch failed to
apply; do not add `[expect.static]`.

**i386 core verify: `test_divmod` / `test_math` / `test_struct` fail under qemu.**
qemu 11.0 TCG leaves `cc_op` stale after `SAHF` (`da7649c6`, GitLab #3537).
Fixed in 11.0.4 / 11.1. The spython image pins qemu-user **11.1.1** from
Alpine edge. If those methods fail again, the image is still on 11.0.x —
upgrade qemu, do not restore the `:qemu` ignores. `*MathTests.testSinh*`
is the real x87 1-ulp ABI issue and stays on `[expect.i386-linux-musl]`.
Write-up: `references/I386_QEMU_SAHF.md`.

**`suite:test_os` unexpected fail under qemu: `test_fork_warns_when_non_python_thread_exists`.**
Same binfmt class. CPython reads `/proc/self/stat` field 20
(`num_threads`). qemu < 9.1 (host Ubuntu 8.2.2) leaves it `0`. A
missing shim falls through to the host qemu; a present shim keeps
re-exec on Alpine 11.1.1. Doctor must require the binfmt interpreter
to exist *and* be ≥ 9.1. Do not restore `[expect.qemu]`.

**sqlite configure: `s390x-binfmt-P: Could not open '/lib/ld-musl-s390x.so.1'`.**
sqlite's configure builds a bootstrap `jimsh0` with the *target* CC and
then runs it. Host binfmt intercepts the cross ELF. `B.cc=@BUILD_CC@`
is make-only; configure never sees it. A host `jimsh` on the *hermetic*
PATH skips the bootstrap — `/bin/jimsh` in the Dockerfile is the Alpine
layout, same class as `expr`/`perl`. Put LookPath(jimsh) in the hermetic
bin. Without it the message is often `./jimsh0: not found` then `No
working C compiler found`. Do not make qemu able to run configure tests.

**pyref sqlite configure: `Cannot find a tclsh to use for code generation`.**
sqlite 3.51 autosetup looks for `tclsh`, not `jimsh`. The image shipped
`jimtcl` (enough for the static `jimsh0` skip) and pyref still died on
the host gcc path. `apk add tcl` and `/bin/tclsh` in the Dockerfile.
Do not pass `--disable-tcl` — that drops the amalgamation codegen.

**`suite:test_bytes` unexpected pass on a reference arm.** `expect.static`
skips `test_bytes` because `_testlimitedcapi` cannot be a builtin or a
dlopen. LookupExpect used to merge that scope for every interpreter.
A host-built reference *can* dlopen, so the file passes and an unexpected
pass fails the run. Merge `expect.static` only when the profile is not
host-built.

**`elf:static` / missing Py_GetVersion on a published reference.** Verify
assumed every interpreter was fully static. A host-built reference has
PT_INTERP and keeps the C API in libpython.so; those checks must be
skipped (or inverted) when the profile is host-built.

**`verify: bin/python3 is not a file` on a reference arm.** pyref publishes
`rootfs/bin/python3`; pynative publishes `bin/python3`. Pack and bench already
unwrap `rootfs/`. Verify did not, so a published reference interpreter failed
core as if it were missing. Unwrap `rootfs/` the same way.

**`lto1: Cannot open Modules/_cursesmodule.o` during a CPython LTO link.**
The other builder's `GCStale` deleted this job's live `dist/work` tree.
`kill(pid, 0)` is PID-namespace local; spython and kitbuild share `dist/`
and after `StaleAge` (10 min, shorter than `-flto-partition=none` WPA)
each treats the other's scratch as dead. gcc 16 then reprints the next
`open` as `Cannot open %s` with no errno (`_ssl.o`, `blob.o`,
`odictobject.o` are whichever file was next). Do not serialize `make`.
GC must skip a scratch dir whose heartbeat is `Live()` — that check
already accepts a recent `UpdatedAt` across machines. The watchdog
restarting `Cannot open` as a flake feeds the bug.

**seplto python link: `multiple definition of BZ2_*` / `lzma_*`.**
`materializeArchives` LTO-rels each `.a` into one relocatable, then used
`ar rcs` to *add* that member. The original IR objects stayed, so the
python link saw `lib_libbz2.a.o` and `bzlib.o`. Slim LTO hid this because
WPA merged the duplicates. Replace the archive; do not append.

**`lib64/libcrypto.a records a dependency prefix` on seplto (or any new
library with a baked LOCALEDIR / ICU_DATA).**
`lto_mode = per-dep` materializes `--prefix` strings in the `.a`. The
recipe configures every non-host per-dep dep with `/usr`, hoists
`DESTDIR+/usr`, and rewrites `.pc`/`.la`/`*-config` back to the artifact
path. OpenSSL's cert dir is `--openssldir=/etc/ssl` on every static
build, not a seplto stanza. Do not add `[package.X.profile.seplto]` and
do not byte-replace OPENSSLDIR (different length corrupts `.rodata`).
`--disable-database` on base ncurses is the terminfo-specific extra, not
the generic lever.

**`lib/libncursesw.a records a dependency prefix inside a binary file` on seplto.**
Same class as openssl: per-dep LTO makes the terminfo path contiguous.
Base ncurses is `--disable-database` (TIC_PATH=true, nothing to bake).
The prefix policy above is what stops the next package.

**`libformw.so needs libncursesw.so.6 but has no RUNPATH`.** The $ORIGIN
rewrite only shrinks an existing DT_RPATH/DT_RUNPATH; it cannot add one.
ncurses' sibling libs (form, menu, panel) NEEDED libncursesw and were
linked with `-rpath-link` only. Host-built LDFLAGS now also bake
`-Wl,-rpath,<prefix>/lib` so the rewrite has something longer than
`$ORIGIN` to overwrite.

**`libffi built without producing lib64/libffi.so` on Alpine.** libffi's
`toolexeclibdir` follows `$CC -print-multi-os-directory`. Flipping
`provides` to `lib/libffi.so` after the last fail lets an Alpine
reference succeed and silently measure musl; on a `../lib64` gcc the
new pin fails. Own the install dir (`--disable-multi-os-directory`) so
`provides = lib/libffi.so` is true on every host. Host-built libc is
whatever `hostcc` fingerprints — do not refuse musl, do not treat
`lib64` as a canary, and do not let `kitFactors` hardcode `glibc`.
"Build reference on a glibc host if you want the glibc baseline" is
docs, not a recipe refuse.

**pyref libffi: `Something went wrong bootstrapping makefile fragments`.**
Same hermetic-PATH class as `expr`. Host-built PATH names no toolchain,
so it is `dirname(busybox)` only. GNU make lives at `/usr/bin/make`.
`/bin/make` in the Dockerfile is the Alpine layout; Ubuntu "works"
because usr-merge makes `dirname(/usr/bin/busybox)` equal `/usr/bin`
and accidentally puts gcc/ld on PATH. Put LookPath(GNU make) and
LookPath(GNU patch) in the hermetic bin (overwrite busybox's `patch`
applet). Do not put `/usr/bin` on the hermetic PATH.

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

**A hybrid machine reports itself uniform, and every guard downstream stops
working.** Core *type* was read from `cpu_capacity` first, falling back to
`cpufreq/cpuinfo_max_freq`. On a Zen 5 + Zen 5c laptop `cpu_capacity` is a flat
1024 for all 24 threads while `cpuinfo_max_freq` cleanly separates 5157895 from
3289474 — so the machine classified as one class, and the consequences were all
silent: `Topology.Hybrid` false, `Fastest()` returning every CPU, the bench menu
showing one undifferentiated block, and `bench`'s "not in the fastest core
class" warning unable to ever fire, so `--cpu 7` handed out a 3.29GHz core
without a word. It landed on a fast core anyway only because CPPC `highest_perf`
happens to correlate (208 vs 125). Neither source is right everywhere —
`cpu_capacity` is the correct one on ARM big.LITTLE — so `classSource` now picks
whichever actually discriminates on the machine in front of it. Symptom: a
`bench` run that reports `uniform` on a machine you know is not, or a menu where
no core is ever marked slow.

**`Could not find a version that satisfies the requirement setuptools>=61` on
`./run`.** Kit `./run` is `--no-index --find-links vendor --no-deps`.
`--no-deps` skips pyperformance's runtime deps (psutil), not PEP 517.
Both sdists declare `requires = ["setuptools>=61"]`; isolated build sees
only those two tarballs; 3.13 ensurepip no longer seeds setuptools. The
fail is a one-second `install pyperformance`. Vendor a setuptools wheel
in the same directory and hash the vendor pins into the kit key. Do not
"fix" it with `--no-build-isolation` alone — the venv still has no
setuptools.

## Host-built profiles: the shared-prefix build

Everything here was found building `pyref` (`--profile reference`), the dynamic
interpreter compiled with the machine's own gcc. None of it can happen to the
static build, which is exactly why it went unnoticed for so long.

**A dependency prefix baked into a shared library.** libtool writes an RPATH,
OpenSSL writes OPENSSLDIR, ncurses writes its terminfo directory. Those strings
are only correct when `--prefix` is where the files finally live, so a host-built
profile builds every dependency into one shared rootfs rather than a prefix each.
The static build never notices: nothing resolves a path at run time, and a
slim-LTO archive does not even contain the string as contiguous bytes, so the
sysroot composer's scan cannot see it. `strings libncursesw.a` returning nothing
is that, not absence.

After install, pyref rewrites every ELF RUNPATH to `$ORIGIN`-relative (in-place
shrink of the baked prefix string). That is what makes the rootfs copyable.
Do not byte-replace OPENSSLDIR: changing its length corrupts `.rodata`. ncurses
on `reference` is `--disable-database` so there is no terminfo path to rewrite.
`$ORIGIN` is resolved from `/proc/self/exe`, so a venv symlink still finds
libpython in the original tree.

**`ld` will not use `-L` to resolve a shared library's own `NEEDED` entries.**
It uses `-rpath-link`, then the library's `RUNPATH`. In a rootfs build that
RUNPATH is the published path, which does not exist while the build runs, so
`configure` link tests against any library that needs a sibling fail and the
module is dropped as "necessary bits not found" — a green build missing half its
extension modules. Symptom: `checking for X in -lfoo... no` while
`checking for foo.h... yes`.

**gcc resolves `ld` off the command PATH, not `$LD`.** Leaving a provisioned
musl toolchain first on a host build makes the host compiler link glibc objects
with a musl linker. Symptom: `undefined reference to 'floor@GLIBC_2.2.5'` and
`libm.so.6 ... not found`. A host-built profile therefore names no target when
composing PATH.

**An import check that passes against the host's libraries.** CPython imports
every extension it builds. Without `LD_LIBRARY_PATH` pointing at the staged
rootfs, the loader finds `/lib64/libssl.so.3`, `libncursesw.so.6`, `libz.so.1`
— the distro's — and reports success for modules linked against ours. Only
sqlite failed, because our SONAME is unversioned (`libsqlite3.so`) and the
host ships `libsqlite3.so.0`. Every other module was being checked against
somebody else's library.

**The static build's ctypes edits leak through the shared srctree.** `pythonapi`
is rebound onto the generated `staticapi` table and `dlopen` is stubbed to a
lambda, both because a fully static interpreter has no libdl. A dynamic build
inherits both from the same srctree and dies on `import ctypes` with
`No module named 'staticapi'`. `pyref` restores stock ctypes in its own copy and
asserts the match count, so an upstream bump that moves either line fails loudly
rather than shipping the static interpreter's ctypes in a dynamic one.

**Countermeasure for all of the above:** `pyref` imports every module that
exists only because a dependency was built, *including the Python-level
wrappers* — `ctypes` fails while `_ctypes` imports cleanly, and checking only
the extension is how that reached a published artifact. The check runs against
the staged tree whose RUNPATH is already `$ORIGIN`-relative, so it must **not**
set `LD_LIBRARY_PATH`: that would hide a failed rewrite by loading the host's
libraries. `make` still needs `LD_LIBRARY_PATH`, because the in-tree binary's
rpath still names the unpublished prefix.

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
alone; full write-up and reproducer in `references/MIPS64_FFI_REPORT.md`.

**`nomimalloc` still linking mimalloc.** Sysroot used to compose every package
regardless of `skip`, so `lib/mimalloc.o` still reached pynative's `LIBS=` and
the static allocator axis was a no-op. Skip is filtered in Sysroot and Deps;
`depBuilder.job` does not filter, so a `Needs` edge that names a skipped
package still fails instead of vanishing.

**`reference-mimalloc` still using glibc malloc.** Un-skipping mimalloc on a
host-built profile does nothing unless the `.o` is on the python link line.
musl's malloc is weak, so a strong `mimalloc.o` in `LIBS=` wins; glibc's malloc
is a strong symbol in libc.so and the override is ELF interposition of a malloc
defined in the executable — the same `LIBS=` path pynative already uses, not a
shared libmimalloc. Symptom: `ldd` shows no mimalloc (there is none) and
allocations match glibc; check that python-configure's argv contains
`LIBS=.../mimalloc.o`.

**Localise before `ld -r`, never after.** mimalloc ships as one relocatable
object with everything but the allocator's entry points made local. Doing that
with `objcopy --keep-global-symbols` *after* the relocatable link builds a
mips64 interpreter that SIGBUSes before `main` ever runs: `R_MIPS_GOT16` and
`CALL16` mean one thing for a global symbol and another for a local one, so
rebinding afterwards leaves the linker reading those relocations under the wrong
rule and the allocator dereferences garbage. Every other arch tolerates either
order, which is what makes it easy to ship. objcopy each compiled object first,
then merge. The cost is that `keep_globals` must cover any symbol the package's
own sources reference across object files -- a loud failure at the final link.

**Merge relocatable objects with the compiler driver, not `ld`.** A bare
`ld -r` selects its own default emulation, which is not the toolchain's ABI
everywhere: mips64 defaults to n32 and refuses the n64 input outright with
"ABI is incompatible with that of the selected emulation". `gcc -r -nostdlib`
passes the right `-m`, and its output is byte-identical to `ld -r` wherever
`ld -r` worked at all.

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
never fires. Full write-up in `references/MUSL_REPORT.md`.

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

**A hung forked child is libatomic then mimalloc, not qemu.** `test_threading`
fails as *env changed* with children "still running after 300.5 seconds".
The test is `ThreadJoinOnShutdown.test_reinit_tls_after_fork`. Two lock
tables, same missing atfork:

1. gcc `libatomic` — 64 `pthread_mutex_t`, no `pthread_atfork`. The recipe
   links `libat_atfork.o` *before* `-latomic`. Spinlocks + child-only zero
   (pid-steal so a late atomic in the child does not wait for our handler).
   Fixes the C class: stock `-latomic` hangs 16-fork+64-bit add on eabi,
   i386, and rv32.
2. mimalloc `mi_lock_t` — also `pthread_mutex_t` (`MI_USE_PTHREADS` stays
   on for TLS keys). `mi_process_init()` is a no-op after fork. nomimalloc
   eabi official test is 3/3 SUCCESS; default eabi with the libatomic
   object still hung 3/3 until `patches/mimalloc-*/0001-fork-safe-locks.diff`
   replaced those mutexes with the same pid-steal spinlocks. After that,
   default eabi is 3/3 in ~0.24s.

Reap leftover qemu children inside the container before calling the next
run a flake. See `references/LIBATOMIC_FORK.md`. If the C hang returns,
the `.o` is after `-latomic` or was compiled with `-flto`. Do not restore
`[expect]`. Do not raise arm-eabi to armv7 — that hides layer 1 the way
armhf does and does not help rv32. Do not skip mimalloc on one triple.

**A toolchain that works on your box proves nothing about a foreign rootfs.**
The requirement is that a tarball drops onto *any* Linux rootfs — glibc,
near-empty, whatever — and compiles through `-flto -fuse-linker-plugin
-fno-fat-lto-objects`. Both halves come from gccfactory: every binary is
static-musl, and the LTO plugin is compiled into `libbfd` so `ld` resolves
`-plugin liblto_plugin.so` to its built-in copy rather than opening a file that
does not exist. Proving that belongs in gccfactory, where the tarballs are
built; a failure surfaces here as a link that cannot find `liblto_plugin.so`,
or a driver that will not start at all.
