---
name: staticpy-bump
description: Upgrade a pinned dependency — openssl, sqlite, ncurses, readline, libffi, xz, zlib, bzip2, util-linux/libuuid, or CPython itself — and survive the sharp corners: where the checksum must come from, why a patch that stops applying is the outcome you wanted, and the packages whose version is encoded in more than one place. Also the overnight matrix-upgrade rules (preemptive fuzz, class-wide hunt, full matrix failed=0, pack from a clean tree). A pyperformance/pyperf pin lives in config/bench.toml, not sources.toml — restamp eta_weights.json after a session has timed the new names (see staticpy, Bench ETA weights). Use whenever changing a version in config/sources.toml or bench.toml, or running a CPython/dep sweep.
---

# Bumping a pinned dependency

## Procedure

1. **Get an authoritative checksum.** There is no `staticpy sources update`,
   deliberately: **the sha256 you paste is the sha256 you trust from then
   on.** Take it from upstream's release announcement, signature or checksum
   file, then confirm your download matches. Hashing whatever the network
   handed you pins a substituted tarball just as firmly.
2. **Edit `config/sources.toml`**: `version`, `file`, `urls`, `sha256`, and
   `topdir` if the wrapper directory renamed. Version and sha256 move **in the
   same edit**. Every mirror must serve the *same* file; a differently packaged
   tarball is a failed build, not a fallback.
3. **Move and regenerate patches.** They live in
   `config/patches/<name>-<version>/`, so a bump orphans the old directory and
   `LoadPatches` fails loudly on a missing patch — that's the point; don't work
   around it. Regenerate against a *pristine* extraction, never
   `dist/srctrees/` (already patched, so the diff is empty or wrong):

   ```sh
   tar -xzf dist/src/<hash>-<file> -C /tmp/pristine
   cp -r /tmp/pristine/<pkg> /tmp/edit && (cd /tmp/edit && …apply the change…)
   diff -u --label a/<f> --label b/<f> /tmp/pristine/<pkg>/<f> /tmp/edit/<f>
   ```

4. **Fetch, verify, see the cost.** The srctree key changes, so the dep,
   sysroot and interpreter rebuild — for CPython, everything.

   ```sh
   ./staticpy sources fetch
   ./staticpy sources verify      # re-hashes; ignores the .done marker
   ./staticpy status              # what the bump will rebuild
   ```

5. **Rebuild through `./staticpy`.** `config/` is a symlink into the `go:embed`
   tree and `sources.toml` is excluded from the runtime overlay, so builds read
   only the embedded copy. The shim rebuilds the binary on a config change;
   bare `go build` does not and leaves a stale embed.

## Sharp corners, per package

- **sqlite encodes its version twice, one half a date.** `3510200` is 3.51.2
  packed; the URL has a *year* directory (`sqlite.org/2026/sqlite-src-3510200.zip`)
  not derivable from the version. Move both.
- **libuuid's source list is hardcoded and fails quietly.** `build = "sources"`
  compiles eleven files from `libuuid/src` plus `lib/randutils.c`, `lib/md5.c`,
  `lib/sha1.c`. A util-linux bump that *removes* a file breaks the compile
  (fine); one that *adds* a file leaves it uncompiled, found at link time or at
  runtime. Diff the old and new directory listings, and check whether
  `gen_uuid.c` gained a dependency in `lib/`.
- **openssl changes shape between majors.** The `Configure` patch needs
  regenerating; `no-*` option names in `packages.toml` aren't stable. Platform
  names in `Target.Maps["openssl"]` come from `Configurations/*.conf`
  (`./Configure LIST`); `i386` → `linux-x86` and `riscv32` → `linux32-riscv32`
  aren't guessable.
- **readline and ncurses are coupled** (`readline` needs `ncurses`; sqlite
  needs readline). Bump together, or get a link error one layer away.
- **bzip2 hasn't moved since 2019.** If it does, treat the whole recipe
  (patch, frozen Makefile) as unverified.
- **zlib rejects `--host`** and reads `$CHOST`. Re-check that special case if
  its hand-rolled configure changes.

## Bumping CPython

A patch release is usually mechanical; a minor (3.13 → 3.14) touches the most.

- **`python-abi` changes**: `bin/python3.14`, `lib/python3.14`. Anything
  matching those strings moves too.
- **`symbols.c` regenerates** from `Misc/stable_abi.toml`. The set of truly
  undeclared `abi_only` entries changes per release; if the generated file stops
  compiling, look at the header scan, not the entry list.
- **The ctypes anchored edits break first** — that's why they're anchored.
  `MustMatch` names the anchor and count; re-anchor, don't loosen.
- **`--with-build-python` checks only major.minor**, so a stale-patch `pyhost`
  would be accepted. The job key (pyhost keyed on the srctree) covers it unless
  you bypass the build system.
- **musl skips may be fixed upstream.** `test_re` and `test_fma_zero_result`
  are in `tests.toml`; a fix shows as an **unexpected pass** that fails verify.
  Delete the stale entry, don't silence it.
- **Pyconfig fragments** describe the target and are unaffected. One that
  suddenly matters means CPython started using a new macro.

## Not a package bump

- **The compiler** is pinned in gccfactory (gcc, binutils, musl, kernel
  headers). A bump is a gccfactory change plus re-publish; here it shows up as
  a new toolchain key that invalidates everything downstream (see
  `staticpy-traps` for why `pyhost` needed that wired in explicitly).
- **Adding a package**: `[source.X]` plus `[package.X]`. A new architecture:
  `staticpy-add-target`.
- **The bench suite**: pyperformance and pyperf are in `config/bench.toml`
  (fallback constants in `internal/bench/pins.go`). A pin bump is a suite
  change, not a protocol bump, and does not break the ETA — `eta_weights.json`
  covers the benches we actually time. After a session has timed the new
  names, restamp it: `staticpy` skill, **Bench ETA weights**.

## Verifying the bump

Cheapest first:

```sh
./staticpy sources verify          # checksum is what you think
./staticpy config show             # pin resolved, and from which file
./staticpy status                  # what will rebuild
./staticpy build --dry-run
./staticpy build --target <t> --verify core
```

Sanity imports catch a dep that built but linked wrong: `ssl`, `zlib`,
`sqlite3`, `ctypes`, `_lzma`, `_hashlib`, `readline`, `curses`, `uuid`,
`compression.zstd`. The `smoke` level runs exactly these plus a
`sysconfig`/`ctypes` cross-check for a corrupted `_sysconfigdata`.

## Running a matrix upgrade

A CPython minor or a sweep invalidating every interpreter covers the current
matrix: every `staticpy print targets-all` triple × every static profile, plus
every host-built `reference*` profile. Don't hard-code a cell count. Don't stop
until every cell is packed with a `failed=0` verify.

- **Fuzz before compiling, always** (even after "just a patch release"). A
  class-wide hole costs >1 h of verify per cell; cheap parallel agent passes
  first: stable-ABI / `staticapi` macros, `Setup` vs
  `Modules/Setup.stdlib.in`, fork/atomics, new `AC_RUN_IFELSE` tests the probe
  doesn't cover.
- **Hunt the class, don't green-wash.** For a failing cell, spin a separate
  investigation demanding a repro on a second arm, the layer, and the complete
  fix. Parking it under `[expect.<this-triple>]` or a one-package profile
  stanza is forbidden (staticpy-traps **Do not overfit the last failure**).
  Write the finding there.
- **Use the machine.** Load is `--workers` × `-j`; maximise without exploding
  RSS (LTO is the peak — measure before trusting the default). earlyoom is a
  backstop, not a plan. Fail-fast: re-run instead of reading a partial sweep.
- **Docker and tmux** exactly as in `AGENTS.md`: one `spython` container for
  every cell (no second container for the libcs); host tmux with
  `EXIT_CODE=${PIPESTATUS[0]}`; poll the log and `staticpy status`, don't
  attach.
- **Pack from a clean tree** (`staticpy-kit`): commit, then stamp, or
  `kit.json` and `staticpy-bench` advertise `HEAD-dirty`.

Done when: `staticpy status --target all --verify core --pack` is 0 stale /
0 missing on every static profile and each `reference*`; every verify report
has `failed=0`; every tarball sha256 matches its sidecar; any shipped kit has a
clean `git_revision`.
