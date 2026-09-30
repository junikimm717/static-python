---
name: staticpy-add-target
description: Add a target triple to staticpy's Go build system, or promote one from experimental to proven — the targets.toml row, the openssl platform name, the pyconfig fragment (and why it should usually be empty now that the ABI probe runs), the qemu binary, the ELF identity, and the verification that has to pass first. Use when asked to support, fix, or prove a triple like riscv32-linux-musl, mips64el, powerpc64-linux-musl, or arm-linux-musleabi.
---

# Adding / proving a target

11 triples live in `config/targets.toml`; only `x86_64-linux-musl` and
`aarch64-linux-musl` are `proven`. A new target is a TOML row plus one
embedded asset. Go changes only for a new *architecture* (ELF identity).

## 1. The row in `config/targets.toml`

Fields (`internal/config/validate.go`, `internal/config/types.go`):

| field | notes |
|---|---|
| `triple` | must equal the table key; validation rejects a mismatch (stops a copy-pasted row shadowing another) |
| `arch` | key into the ELF identity table (§6). `IdentityFor` tries `arch`, then the triple's leading component |
| `abi` | required but unread by Go; distinguishes rows sharing an arch (`musleabi` vs `musleabihf`), as the host auto-detect error says |
| `bits` | 32 or 64. Checked at verify against the ELF class and the interpreter's own smoke-probe report |
| `libatomic` | appends `-latomic` to CPython `LDFLAGS`; set wherever 64-bit `_Py_atomic_*` lands in libatomic (CPython won't ask). Existing: `i[3-6]86`, `arm` (non-64), `mips` (non-64), `microblaze`, `sh`, `m68k`, `or1k`, `riscv32\|riscv64` |
| `uint128` | only feeds the CPython job key (`HAVE_GCC_UINT128_T` comes from the probe). Set it truthfully or the key lies |
| `qemu` | qemu-user binary. `QemuBinaryName` prepends `qemu-` and defaults to the triple's leading component; spell it out when they diverge (`powerpc64le` → `qemu-ppc64le`) |
| `status` | `proven` or `experimental` (§9) |
| `maps.openssl` | required (§2): `package.openssl` has `platform_map = "openssl"`, so validation fails at load — cheap, fires on `staticpy print` |

`config/` is a symlink to `src/staticpy/internal/config/defaults/`, the
`go:embed` tree. Go through `./staticpy`, which rebuilds the binary on a
config change; a bare `go build` keeps the old embedded rows.

## 2. The openssl platform name

Authoritative list: OpenSSL's `Configurations/*.conf` (`./Configure LIST`).
Usual pattern: `linux-<arch>` for x86_64/aarch64; `linux64-<arch>` for
mips64/riscv64/s390x/sparcv9; `linux-ppc64` / `linux-ppc64le`; `linux-armv4`
for any 32-bit ARM (the toolchain's `-march` wins). Not guessable, both noted
in `targets.toml`:

- **i386 → `linux-x86`.** Not `linux-x32` (x86_64 ILP32, forces `-mx32`, which
  a 32-bit gcc can't do). Unset, OpenSSL guesses wrong.
- **riscv32 → `linux32-riscv32`.** There is no `linux64-riscv32`.

## 3. The pyconfig fragment

Create `src/staticpy/internal/assets/files/pyconfig/<triple>-patches.h`. The
**file** is required (no overlay for assets; `recipe.Probe` hard-errors). The
**content** usually isn't: start empty. `aarch64-linux-musl` and
`mips64-linux-musl` are zero bytes — the target state.

## 4. What the probe measures

`internal/assets/files/patcher.c` is built with the target toolchain, run
under qemu, and its stdout becomes a `config.site` (so CPython's configure
computes pyconfig.h for a target it can't run) and the appended fragment. It
reports:

- every `SIZEOF_*` and `ALIGNOF_*` in `probedMacros`
- `HAVE_GCC_UINT128_T`, `HAVE_GCC_ASM_FOR_X64`, `HAVE_GCC_ASM_FOR_X87`
- `WORDS_BIGENDIAN` (integer byte order)
- `DOUBLE_IS_BIG_ENDIAN_IEEE754` / `DOUBLE_IS_LITTLE_ENDIAN_IEEE754` (stored
  double byte order; may differ from integer)
- `HAVE_ALIGNED_REQUIRED` (misaligned load in a forked child; SIGBUS is the
  answer on strict-alignment targets)

**A fragment line repeating these is redundant, and the fragment wins.**
`configSite` merges probe output first, then the asset, so a stale fragment
overrides a correct measurement. `reportOverrides` warns on contradictions and
debug-logs exact repeats — **a quiet run means the fragment can be deleted.**
Don't copy an existing fragment as a template; several carry dead weight:

- `arm-linux-musleabi`, `arm-linux-musleabihf`, `i386`, `riscv32`: full
  hand-written size/alignment tables plus `#undef HAVE_GCC_UINT128_T`.
- `powerpc64`, `powerpc64le`, `s390x`: endianness and
  `HAVE_ALIGNED_REQUIRED 1`.

All measured now. `HAVE_ALIGNED_REQUIRED 1` on a target that tolerates
unaligned loads silently loses the faster hash.

Hard constraint: a fragment may not `#undef` a `SIZEOF_*`/`ALIGNOF_*`;
`configSite` refuses (configure can't measure it cross). Give a value or drop
the line.

## 5. What still needs a fragment

Deliberate deviations from the hardware. Test: **if a program on the target
could answer it, it belongs in `patcher.c`.** Live example, riscv32/riscv64:

```c
// Same musl/gcc libatomic.a quirk as riscv64; force the table fallback.
#undef HAVE___BUILTIN_CLZ
#undef HAVE_BUILTIN_ATOMIC
```

The chip has both builtins; the musl toolchain lacks a usable `libatomic.a`
behind them, so CPython must take its table fallback — a toolchain decision.

Routing is automatic: a macro with an autoconf cache variable
(`autoconfCache` / `boolCache`) goes into `config.site`; one without
(`HAVE___BUILTIN_CLZ`, `HAVE_BUILTIN_ATOMIC`) is appended to `pyconfig.h`
after configure.

Mixed-endian doubles are the one probe hand-back: `patcher.c` prints an
`#error` naming the byte order and asks for `DOUBLE_IS_*` in the fragment. No
current target hits it.

## 6. ELF identity (new architecture only)

If `arch` isn't in `identities` in `internal/ensure/elf.go`, add machine,
class, endianness; verify otherwise fails with "add a row to identities".

- `archAliases` maps **`mips64el` → `mips64`, `ELFDATA2MSB`**. A little-endian
  mips64 through that alias asserts big-endian and fails `elf:machine`. Fix the
  table first.
- `goarchIdentity` in `internal/ensure/qemu.go` decides whether this machine
  runs the target natively; only matters if the arch will be a build host.

## 7. qemu

staticpy never fetches qemu: `--qemu <triple>=<path>`, then
`exec.LookPath(QemuBinaryName(t))`. The dev container provisions it via
`scripts/docker/qemu-user.sh` (`--target-list`) plus the
`/usr/libexec/qemu-binfmt` shims. A new triple that needs verify must be added
there, or `doctor` shows `(not found)` and verify fails at launcher
construction. No qemu isn't a broken target: `doctor` separates *buildable*
from *runnable*.

## 8. Test expectations

Only for observed musl/qemu failures, in `config/tests.toml`.
`ensure.LookupExpect` merges, widest first: `all`, `static` (only when the
profile isn't host-built), the runner (`qemu`/`native`), `<triple>`, and
`<triple>:<runner>`.

```toml
[[expect."<triple>".skip]]        # both runners
[[expect."<triple>:qemu".fail]]   # qemu only; ":native" for the other
```

- Runner-keyed because qemu-user fails around signals, threads and
  subprocesses in ways that say nothing about the build.
- Every entry needs a `why`; it's carried into the report so stale entries can
  be found by their reason.
- **An unexpected pass fails the run.** Delete an entry the moment it passes.
- Keys are **not** validated: a typo'd triple is silently ignored and the
  failure reports as unexpected.
- Don't add `[expect."<triple>"]` without reproducing on a second arm. If
  native x86_64 static fails too, the scope is `[expect.static]` or a recipe
  fix. See staticpy-traps **Do not overfit the last failure**.

## 9. Proven versus experimental

`status` gates four things, none of them the build:

- `--target proven` and `staticpy print targets-proven`
- labels in `doctor` and `staticpy help targets`
- host auto-detection: exactly one **proven** row for this arch wins; otherwise
  all rows for the arch are considered and two is an error. **A second proven
  same-arch row doesn't disambiguate** — proving `arm-linux-musleabi` alongside
  `arm-linux-musleabihf` would break host resolution on an arm builder.
- CI gates on proven only

Evidence before promoting:

1. `staticpy verify --level core --target <triple>` clean — the minimum.
   `core` catches miscompiles (numerics, containers, every hand-linked
   extension, i.e. a bad `_sysconfigdata` or half-linked lib); `smoke` only
   proves it starts.
2. A probe run with **no** `fragment overrides what the probe measured`
   warnings.
3. The triple's `tests.toml` entries justified and current (no unexpected
   passes).

## 10. Verifying, cheapest first

```sh
staticpy print targets-all                  # row parses, validation passed
staticpy doctor                             # toolchain and qemu resolve
staticpy build --dry-run --target <triple>  # plan builds, no missing asset
staticpy build --target <triple>            # hours
staticpy verify --level core --target <triple>
```

The first two (seconds) catch a missing `maps.openssl`, bad `status`, `bits`
not 32/64, unresolvable qemu. `--dry-run` catches a missing pyconfig asset
(`recipe.Probe` is built during planning).

What bites hardest when adding a target (more in `staticpy-traps`):

- **`-latomic`** undeclared: fails at the CPython link on `_Py_atomic_*`, long
  after deps are built.
- **32-bit targets expose a corrupted `_sysconfigdata` first**, as a wrong
  `SIZEOF_VOID_P`; on 64-bit, host and target agree often enough to hide it.
