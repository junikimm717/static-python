# Portability Proof: musl-cross Toolchain on a Foreign (glibc) Rootfs

## Summary

The x86_64-linux-musl toolchain we build via `USE_CROSSMAKE=1` -- the one
produced by `musl-cross-make` plus our `cross-make/post-install.sh`
relocatability fixup -- extracts cleanly into a stock `debian:stable-slim`
container that has **no compiler** of any kind, just `binutils`/`file`/`make`,
and from there compiles and runs three nontrivial test programs end-to-end,
including the LTO-with-linker-plugin path that is the entire reason the
wrapper system exists. Every artefact it produces is `static-pie linked`,
has no `PT_INTERP`, has no `DT_NEEDED`, and carries no glibc identifying
strings -- the host glibc never gets touched. The proof runs in roughly
seven seconds after a `make download`-free, cache-warm Docker build, and is
re-runnable end-to-end via `cross-make/test-portability/proof.sh`.

## Reproducer

```sh
# Host: any glibc box with docker + a recent kernel. No special privileges.
./cross-make/test-portability/proof.sh
# tee's full output to build-logs/portability-alien.log
```

Internals:

- `cross-make/test-portability/Dockerfile.alien` -- `debian:stable-slim`
  with `make`, `file`, `binutils`, `ca-certificates`, and a Dockerfile
  tripwire that fails the build if any of `cc`, `gcc`, `g++` survive.
- `cross-make/test-portability/x86_64-linux-musl-native.tgz` -- the toolchain
  tarball under test. Produced by tar'ing the wrapper-applied install tree
  out of `deps-x86_64-linux-musl/x86_64-linux-musl-native/`.
- `cross-make/test-portability/tests/{hello.c,hello.cc,lib.c,lib.h,main.c}`
  -- the three nontrivial programs (C, C++, two-TU LTO-via-archive).
- `cross-make/test-portability/run.sh` -- the in-container driver:
  identifies the rootfs, confirms no compiler is on `PATH`, extracts the
  tarball under `/opt`, compiles + runs each test, and emits the deep
  linkage diagnostics quoted below.

## Environment

```text
host arch:    x86_64
host kernel:  Linux 6.17.0-1013-azure (Ubuntu)
alien rootfs: Debian GNU/Linux 13 (trixie), inside docker
alien libc:   glibc; /bin/sh .interp = /lib64/ld-linux-x86-64.so.2
              (a symlink to /lib/x86_64-linux-gnu/ld-linux-x86-64.so.2)
musl loader on host or alien:  not installed in either rootfs.
toolchain:    deps-x86_64-linux-musl/x86_64-linux-musl-native/
              built with USE_CROSSMAKE=1 (gcc 15.1.0, musl 1.2.5,
              binutils 2.44), post-install.sh applied.
```

The alien rootfs has no `/lib/ld-musl-x86_64.so.1` and no
`/lib/libc.musl-x86_64.so.1`. The real `gcc`/`cc1`/`ld.bfd` ELFs inside
the tarball still carry `interpreter /lib/ld-musl-x86_64.so.1` -- that is
why running them naked on a non-musl rootfs would `ENOENT`. The wrapper
machinery (`cross-make/wrapper.c` + `cross-make/post-install.sh`) is the
whole reason this still works.

## What we expected vs what happened

### 1. Wrapper + bundled loader are healthy

Expected: `bin/x86_64-linux-musl-gcc` is the static-musl launcher; the
underlying real binary lives in `bin/.real/` and is a normal dynamic
musl ELF; `runtime/libc.so` is the bundled musl loader with a sane SONAME.

Observed (from `build-logs/portability-alien.log`):

```text
bin/x86_64-linux-musl-gcc:
  ELF 64-bit LSB executable, x86-64, version 1 (SYSV),
  statically linked, BuildID[sha1]=f1b8c9b3..., with debug_info, not stripped

bin/.real/x86_64-linux-musl-gcc:
  ELF 64-bit LSB executable, x86-64, version 1 (SYSV),
  dynamically linked, interpreter /lib/ld-musl-x86_64.so.1,
  BuildID[sha1]=6f3ce278..., with debug_info, not stripped

runtime/libc.so:
  ELF 64-bit LSB shared object, x86-64, version 1 (SYSV),
  dynamically linked, BuildID[sha1]=d52a6a6a..., stripped
  SONAME: libc.musl-x86_64.so.1
```

And the side-channel `liblto_plugin.so` symlink is in place, so
`ld.bfd`'s `lib/bfd-plugins/` autoload path resolves without an
explicit `--plugin`:

```text
lib/bfd-plugins/liblto_plugin.so
  -> ../../libexec/gcc/x86_64-linux-musl/15.1.0/liblto_plugin.so
```

`gcc --version` and `g++ --version` then run successfully on the alien
rootfs -- which by itself is the highest-value bit, because both ELFs
are dynamic musl binaries whose `.interp` does not exist on the host;
they only run at all because the static launcher rewrites the exec
into `runtime/libc.so --library-path runtime/ --argv0 ... .real/gcc ...`.

```text
x86_64-linux-musl-gcc (GCC) 15.1.0
x86_64-linux-musl-g++ (GCC) 15.1.0
```

### 2. Plain C

Source: `tests/hello.c` (linked-list build + reduce-with-callback).

```text
$ x86_64-linux-musl-gcc -O2 -static -o hello-c hello.c
$ ./hello-c
c-hello sum=15 product=120
```

`file`:
```text
ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV),
  static-pie linked, BuildID[sha1]=59ecbcd2..., not stripped
```

### 3. C++ with libstdc++

Source: `tests/hello.cc` (`<iostream>`, `<vector>`, `<string>`,
`std::sort`, `std::accumulate`). This forces `cc1plus` and `libstdc++`.

```text
$ x86_64-linux-musl-g++ -O2 -static -o hello-cxx hello.cc
$ ./hello-cxx
cxx-hello sorted=1,2,3,4,5 sum=15
```

`file`:
```text
ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV),
  static-pie linked, BuildID[sha1]=77139fa0..., with debug_info, not stripped
```

### 4. LTO with `-fuse-linker-plugin -fno-fat-lto-objects`

This is the whole reason we have the wrapper system: `ld.bfd` has to
`dlopen` `liblto_plugin.so`, which static musl cannot do. We therefore
let host gcc/binutils stay dynamic, and use the wrapper + bundled musl
loader to make those dynamic ELFs runnable on foreign rootfses.

Sources: `tests/lib.c` (defines `dot_product`), `tests/main.c` (calls it,
links against `lib.a`). Built strictly as slim LTO:

```text
$ x86_64-linux-musl-gcc -O2 -flto -fuse-linker-plugin -fno-fat-lto-objects \
    -c lib.c -o lib.o
$ x86_64-linux-musl-readelf -SW lib.o | grep gnu.lto_
  [ 4] .gnu.lto_.profile.81efe114120e8f08     PROGBITS  ...  E
  [ 5] .gnu.lto_.icf.81efe114120e8f08         PROGBITS  ...  E
  [ 6] .gnu.lto_.ipa_sra.81efe114120e8f08     PROGBITS  ...  E
  [ 7] .gnu.lto_.inline.81efe114120e8f08      PROGBITS  ...  E
  ...
  .text size = 000000
```

i.e. `lib.o` is genuinely a slim LTO object -- `.text` is empty, the
real implementation lives in `.gnu.lto_*` GIMPLE blobs that only `lto1`
can lower.

The link itself:

```text
$ x86_64-linux-musl-gcc-ar rcs lib.a lib.o
$ x86_64-linux-musl-gcc -O2 -flto -fuse-linker-plugin -fno-fat-lto-objects \
    -static -v -o lto-main main.o lib.a
```

The smoking-gun line from the `gcc -v` driver pipeline, proving that
`collect2` did pass the plugin path through to `ld.bfd`:

```text
/opt/x86_64-linux-musl-native/bin/../libexec/gcc/x86_64-linux-musl/15.1.0/collect2 \
  -plugin /opt/x86_64-linux-musl-native/bin/../libexec/gcc/x86_64-linux-musl/15.1.0/liblto_plugin.so \
  -plugin-opt=/opt/x86_64-linux-musl-native/bin/../libexec/gcc/x86_64-linux-musl/15.1.0/lto-wrapper \
  -plugin-opt=-fresolution=... -plugin-opt=-pass-through=-lgcc \
  -plugin-opt=-pass-through=-lgcc_eh -plugin-opt=-pass-through=-lc \
  -flto --sysroot=... -m elf_x86_64 -static -pie --no-dynamic-linker \
  ... main.o lib.a ...
```

And the lto-wrapper / lto1 worker invocations actually fire:

```text
/opt/x86_64-linux-musl-native/bin/../libexec/gcc/x86_64-linux-musl/15.1.0/lto-wrapper \
  -fresolution=... -flinker-output=pie main.o lib.a@0x94

/opt/x86_64-linux-musl-native/bin/../lib/gcc/../../libexec/gcc/x86_64-linux-musl/15.1.0/lto1 \
  ... -fwpa  ... lib.a slim sections ...
/opt/x86_64-linux-musl-native/bin/../lib/gcc/../../libexec/gcc/x86_64-linux-musl/15.1.0/lto1 \
  ... -fltrans ... -o /tmp/ccgAJPlF.s
```

The resulting executable:

```text
$ file lto-main
ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV),
  static-pie linked, BuildID[sha1]=0af3f088..., not stripped
$ ./lto-main
lto-main dot=35
```

#### Negative control

Without the plugin, the same slim LTO objects must fail to link, because
the linker sees their `.text` as empty:

```text
$ x86_64-linux-musl-gcc -O2 -static -fno-lto -o lto-main-noplugin \
    main.o lib.a
... rcrt1.c:(.text.__dls2+0x1a): undefined reference to `main'
no-plugin link rc=1 (expected non-zero)
```

`rcrt1.o` is musl's static-PIE entry; it cannot find `main` because
`main.o` is also a slim LTO object. That is the right falsification:
without `lto1` lowering the GIMPLE blobs, none of the symbol bodies
exist. Combined with the positive test above, this rules out "maybe gcc
just compiled a fat object behind our back".

### 5. The artefacts are static musl, no glibc leakage

```text
=== hello-c ===
file: ELF 64-bit LSB pie executable, ... static-pie linked
readelf -d: (no DT_NEEDED, no RUNPATH/RPATH)
readelf -l: (no PT_INTERP)
strings | grep glibc | grep -v glibcxx: (none)

=== hello-cxx ===
file: ELF 64-bit LSB pie executable, ... static-pie linked
readelf -d: (no DT_NEEDED, no RUNPATH/RPATH)
readelf -l: (no PT_INTERP)
strings | grep glibc | grep -v glibcxx: (none)

=== lto-main ===
file: ELF 64-bit LSB pie executable, ... static-pie linked
readelf -d: (no DT_NEEDED, no RUNPATH/RPATH)
readelf -l: (no PT_INTERP)
strings | grep glibc | grep -v glibcxx: (none)
```

The `hello-cxx` binary does carry `glibcxx.*` / `GLIBCXX_TUNABLES`
strings, but those are the GNU libstdc++ internal namespace tags, not
glibc -- glibc would show up as `GLIBC_2.*` version symbols or a
`GNU C Library` banner string, and neither appears in any artefact.
The grep in `run.sh` explicitly filters `glibcxx`/`libstdc` out so the
result reflects glibc exposure only.

The `hello-cxx` binary also embeds a few build-time include paths from
`/workspace/deps-x86_64-linux-musl/.../libstdc++-v3/...` (because we
build without `-g0` / `-ffile-prefix-map`); they are debug strings, not
runtime deps, and only confirm provenance.

## Where this would break

1. **Host kernel too old for static-PIE musl.** `runtime/libc.so` is
   linked `static-pie`; running it through `execve` directly works on
   any kernel new enough to load PIE ELFs. We have not exercised the
   floor; pre-3.3 kernels (no PIE for static-pie) would refuse.
   Symptom would be `execve` returning `ENOEXEC` from the wrapper. Not
   a realistic risk in 2026.

2. **No `/proc` mounted.** The wrapper resolves its own location via
   `readlink("/proc/self/exe", ...)` -- if `/proc` is not mounted, the
   wrapper bails with `tc-wrapper: readlink /proc/self/exe: ...`.
   Containers and bind-mounted chroots that omit `/proc` would trip
   this. Mitigation: mount `procfs`.

3. **Foreign kernel personality** (e.g. running a glibc syscall
   wrapper against an old kernel ABI). `runtime/libc.so` is musl, so
   the libc->kernel ABI it expects is whatever musl 1.2.5 was built
   against (Linux 5.15 headers in our case). Pre-5.15-ish kernels may
   refuse certain syscalls; the symptom would be `ENOSYS` deep inside
   the toolchain rather than at exec time.

4. **Toolchain bind-mounted read-only with a missing `runtime/`.**
   The wrapper walks parent directories looking for `runtime/libc.so`.
   If the tarball was extracted partially, or `runtime/` was excluded
   from a bind-mount filter, the wrapper bails with
   `tc-wrapper: could not locate runtime/libc.so walking up from ...`.

5. **Other architectures.** This proof is x86_64-only. The wrapper
   logic is arch-agnostic (it just exec's the bundled loader), but we
   have not produced a portability tarball for, say, aarch64. The
   expectation is that `parallel-toolchains.pl` already produces
   equivalent wrapper-applied tarballs per arch under `tarballs/`,
   and the proof can be re-run by re-pointing `proof.sh` at those.

## What we ended up doing

- Authored `cross-make/test-portability/{Dockerfile.alien, run.sh,
  proof.sh, tests/}` and committed a freshly-tar'd
  `x86_64-linux-musl-native.tgz` next to them.
- Refactored `run.sh` after a first pass: the initial draft tried to
  catch the plugin load via `ld --verbose` greps, which were
  inconclusive because the verbose output got squelched by the
  driver. Replacing that with `gcc -v` and grepping for the
  `collect2 -plugin <abs path>` line is much harder to fool.
- Added a negative-control link without the plugin: it must fail with
  "undefined reference to `main`" (the slim LTO `main.o` has no real
  `.text`), which forces the LTO-positive result to actually mean
  something.
- Filtered the `glibc` strings grep to exclude libstdc++'s own
  `glibcxx.*` / `GLIBCXX_TUNABLES` identifiers, which are namespace
  tags from libstdc++ rather than evidence of glibc leakage.

## Next time

- If we ever bump `GCC_VER` or `BINUTILS` (especially binutils, since
  the plugin ABI lives there): re-run `proof.sh` and diff the
  `collect2 -plugin ...` line above. If the plugin path or arguments
  change shape, update the `grep` in `run.sh` so we don't silently
  start passing without exercising the right thing.
- The tarball `cross-make/test-portability/x86_64-linux-musl-native.tgz`
  is the artefact under test, regenerated by:
  ```sh
  docker compose exec -T spython sh -lc \
    'cd /workspace && tar -czf cross-make/test-portability/x86_64-linux-musl-native.tgz \
       -C deps-x86_64-linux-musl x86_64-linux-musl-native'
  ```
  Refresh it any time `cross-make/post-install.sh` or `wrapper.c`
  changes -- otherwise the proof is silently re-validating an old
  wrapper.
- Generalising to non-x86_64 archs is straightforward: parameterise
  `run.sh` over `<arch>-linux-musl` and adjust the alien container to
  use `--platform=linux/<arch>` (via qemu-user). The wrapper logic is
  identical per arch, so a clean run on aarch64 would add weight but
  the x86_64 result already covers the load-bearing claim.
