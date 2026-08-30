---
name: staticpy-bump
description: Upgrade a pinned dependency — openssl, sqlite, ncurses, readline, libffi, xz, zlib, bzip2, util-linux/libuuid, or CPython itself — and survive the sharp corners: where the checksum must come from, why a patch that stops applying is the outcome you wanted, and the packages whose version is encoded in more than one place. Use whenever changing a version in config/sources.toml.
---

# Bumping a pinned dependency

A version bump is the operation this whole build system exists to make safe, so
most of it is mechanical. What follows is the order, and the places where it is
not mechanical.

## The procedure

**1. Get the new version and an authoritative checksum.**

There is no `staticpy sources update` — nothing here downloads with verification
disabled and records whatever arrived. The gap is deliberate enough to state
plainly:
**the sha256 you paste is the sha256 you trust from then on.** Hashing whatever
your network handed you pins a corrupted or substituted tarball exactly as
firmly as it pins the real one. Take the digest from upstream's release
announcement, signature, or checksum file — then download and confirm your copy
matches it.

**2. Edit `config/sources.toml`.** `version`, `file`, `urls`, `sha256`, and
`topdir` if the archive's wrapper directory changed name. Mirrors must serve the
*same* file: a mirror carrying a differently-packaged tarball of the same
release is a failed build, not a fallback.

**3. Move and regenerate the patches.** Patch files live under
`patches/<name>-<version>/`, so bumping the version orphans the old directory.
`LoadPatches` errors when a listed patch is missing, so this fails loudly rather
than silently building unpatched — do not work around it. Regenerate against a
*pristine* extraction:

```sh
tar -xzf dist/src/<hash>-<file> -C /tmp/pristine
cp -r /tmp/pristine/<pkg> /tmp/edit && (cd /tmp/edit && …apply the change…)
diff -u --label a/<f> --label b/<f> /tmp/pristine/<pkg>/<f> /tmp/edit/<f>
```

Never diff against a tree in `dist/srctrees/`: patches and content-anchored
edits have already been applied there, so the diff you get will be empty or
wrong.

**4. The version and its sha256 move in the same edit.** A pin without its
checksum is not a half-done commit, it is a build that fetches an unverified
tarball.

**5. Fetch and verify.**

```sh
./staticpy sources fetch
./staticpy sources verify      # re-hashes; does not trust the .done marker
./staticpy status              # what the bump will rebuild
```

**6. Rebuild.** The shim re-syncs `internal/config/defaults/` from `config/` and
treats a change under either as a rebuild trigger. Building with bare
`go build` skips that and will use a stale embedded copy — `config/sources.toml`
is excluded from the runtime overlay by design, so the embedded copy is the only
one a build reads.

## What a bump costs

The srctree key changes, so everything downstream of it rebuilds: the dep, the
sysroot, the interpreter. For CPython that is the whole tree. Run
`./staticpy status` first and know what you are triggering.

## Sharp corners, per package

**sqlite encodes its version twice, and one half is a date.** `3510200` is
3.51.2 as a packed integer, and the download URL carries a *year* directory
(`sqlite.org/2026/sqlite-src-3510200.zip`). Both move together, and the year is
not derivable from the version.

**libuuid's source list is hardcoded, and this is the one that fails quietly.**
`build = "sources"` compiles a declared file list with no configure step — for
libuuid, eleven files from `libuuid/src` plus `lib/randutils.c`, `lib/md5.c` and
`lib/sha1.c`. A util-linux bump that *removes* a file breaks the compile, which
is fine. One that *adds* a file leaves it silently uncompiled, and you find out
at link time if you are lucky and at runtime if you are not. Diff the directory
listings between the old and new tarballs before trusting the list, and check
whether `gen_uuid.c` picked up a new dependency on something in `lib/`.

**openssl changes shape between majors.** The `Configure` patch is a
version-pinned diff and will need regenerating. The `no-*` option names in
`packages.toml` are not stable across major versions. The platform names in
`Target.Maps["openssl"]` come from `Configurations/*.conf` — `./Configure LIST`
is authoritative, and `i386` → `linux-x86` and `riscv32` → `linux32-riscv32`
are the two that are not guessable from the arch name.

**readline and ncurses are coupled.** `readline` declares `needs = ["ncurses"]`,
and sqlite needs readline. Bump them together or expect a mismatch that surfaces
as a link error a layer away from the change.

**bzip2 has not moved since 2019.** Its patch is stable and its Makefile is
frozen. If a new release appears, treat the whole recipe as unverified rather
than assuming the patch still applies.

**zlib rejects `--host`** and reads `$CHOST` instead. If a bump changes its
hand-rolled configure, that special case is the first thing to re-check.

## Bumping CPython

A patch release (3.13.13 → 3.13.14) is usually mechanical. A minor bump
(3.13 → 3.14) touches more than any other dependency here.

- **`python-abi` changes**, so the interpreter becomes `bin/python3.14` and
  `lib/python3.14`. Anything matching those paths by string moves with it.
- **`symbols.c` regenerates automatically** from the new `Misc/stable_abi.toml`,
  which is the point of generating it. But the count of genuinely undeclared
  `abi_only` entries changes with the release, and the header scan that finds
  them must still work — if the generated file stops compiling, that scan is
  where to look, not the entry list.
- **The ctypes anchored edits are the most likely thing to break**, which is
  exactly why they are anchored edits rather than diffs. `MustMatch` fails the
  job naming the anchor and the count; re-anchor rather than loosening it.
- **`--with-build-python` only checks major.minor.** A `pyhost` left over from
  the old minor would be rejected, but one from a different *patch* release
  would be silently accepted. The job key covers this — pyhost is keyed on the
  srctree — so it only bites if you go around the build system.
- **The musl skips may have been fixed upstream.** `test_re` and
  `test_fma_zero_result` are declared in `tests.toml`; if musl or CPython fixed
  either, verification reports an **unexpected pass** and fails, which is the
  mechanism working. Delete the stale entry rather than silencing it.
- **The per-target pyconfig fragments** are unaffected by a CPython bump — they
  describe the target, not the interpreter. A fragment that suddenly matters
  again after a bump means CPython started using a macro it did not before.

## What is not a package bump

**The compiler is not pinned here.** gcc, binutils, musl and the kernel headers
live in gccfactory, which publishes one tarball per (host, target) cell. A
compiler bump is a gccfactory change plus a re-publish. On this side it appears
as a new toolchain key, which invalidates every downstream artifact
automatically — see `staticpy-traps` for why `pyhost` needed that wired in
explicitly.

**Adding a package is not bumping one.** A new native library is a `[source.X]`
plus a `[package.X]`; a new architecture is `staticpy-add-target`.

## Verifying the bump

Cheapest first, so a mistake costs seconds rather than an hour:

```sh
./staticpy sources verify          # the checksum is what you think it is
./staticpy config show             # the pin resolved, and from which file
./staticpy status                  # what will rebuild
./staticpy build --dry-run
./staticpy build --target <t> --verify core
```

Then the sanity imports, which catch a dependency that built but linked wrong:
`ssl`, `zlib`, `sqlite3`, `ctypes`, `_lzma`, `_hashlib`, `readline`, `curses`,
`uuid`. The `smoke` verification level runs exactly these, plus a
`sysconfig`/`ctypes` cross-check that catches a corrupted `_sysconfigdata`.
