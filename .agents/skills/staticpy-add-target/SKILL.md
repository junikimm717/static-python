---
name: staticpy-add-target
description: Add a target triple to staticpy's Go build system, or promote one from experimental to proven — the targets.toml row, the openssl platform name, the pyconfig fragment (and why it should usually be empty now that the ABI probe runs), the qemu binary, the ELF identity, and the verification that has to pass first. Use when asked to support, fix, or prove a triple like riscv32-linux-musl, mips64el, powerpc64-linux-musl, or arm-linux-musleabi.
---

# Adding / proving a target

11 triples live in `config/targets.toml`; only `x86_64-linux-musl` and
`aarch64-linux-musl` are `proven`. A new target is a TOML row plus one embedded
asset file. Go code changes only when the *architecture* is new (ELF identity),
not when the triple is.

## 1. The row in `config/targets.toml`

Every field, and what breaks without it (`internal/config/validate.go`,
`internal/config/types.go`):

- **`triple`** — must equal the table key. Validation rejects a mismatch, which
  is the only thing stopping a copy-pasted row from silently shadowing another.
- **`arch`** — the key into the ELF identity table (§6). `IdentityFor` tries
  `arch` first, then the leading component of the triple, so it can differ from
  the triple only if you mean it to.
- **`abi`** — required, but nothing in the Go tree reads it. It exists to
  distinguish rows that share an arch (`musleabi` vs `musleabihf`), and the host
  auto-detection error message says so.
- **`bits`** — 32 or 64. Checked twice at verify time: against the ELF class of
  the built interpreter, and against what the interpreter reports about itself
  in the smoke probes.
- **`libatomic`** — appends `-latomic` to CPython's `LDFLAGS`. Set it for any
  target whose 64-bit `_Py_atomic_*` operations land in libatomic rather than
  compiler builtins; CPython's own link line does not ask for it. The existing
  rows cover `i[3-6]86`, `arm` (non-64), `mips` (non-64), `microblaze`, `sh`,
  `m68k`, `or1k`, `riscv32|riscv64`.
- **`uint128`** — today this feeds only the CPython job key; the macro CPython
  actually sees (`HAVE_GCC_UINT128_T`) comes from the probe. Still set it
  truthfully: a wrong value makes the cache key lie about the build.
- **`qemu`** — the qemu-user binary name. `QemuBinaryName` prepends `qemu-` if
  absent and falls back to the triple's leading component when the field is
  empty, so spell it out where the arch name and the qemu name diverge
  (`powerpc64le` → `qemu-ppc64le`).
- **`status`** — `proven` or `experimental`; see §9.
- **`maps.openssl`** — see §2. Required: `package.openssl` declares
  `platform_map = "openssl"`, and config validation fails at load if any target
  lacks the entry. That failure is cheap and early — it fires on `staticpy print`.

`internal/config/defaults/` is a `cp -r` of repo-root `config/`, embedded so a
released binary works with no checkout. `config/` is the editable copy. The
`./staticpy` shim re-syncs `defaults/` before every rebuild and treats a change
under `config/` as a reason to rebuild, so going through the shim needs nothing
extra. Building the binary directly with `go build` does — run
`cd src/staticpy && go generate ./internal/config` first, or the new row exists
only for a checkout-relative run. Commit both copies.

## 2. The openssl platform name

OpenSSL's platform names follow no single rule. The authoritative list is
OpenSSL's own `Configurations/*.conf`; `./Configure LIST` in an unpacked source
tree prints it.

The pattern that holds for most rows: `linux-<arch>` for x86_64/aarch64,
`linux64-<arch>` for mips64/riscv64/s390x/sparcv9, `linux-ppc64` /
`linux-ppc64le`, `linux-armv4` for any 32-bit ARM (the toolchain's `-march` wins
over the config's baseline).

Two are **not** guessable from the arch name, and both are recorded with their
reason in `targets.toml`:

- **i386 → `linux-x86`.** Not `linux-x32`: that is the x86_64 ILP32 ABI and
  forces `-mx32`, which a genuinely 32-bit gcc cannot do. Left unset, OpenSSL
  guesses and guesses wrong.
- **riscv32 → `linux32-riscv32`.** OpenSSL ships no `linux64-riscv32`.

## 3. The pyconfig fragment

Create `src/staticpy/internal/assets/files/pyconfig/<triple>-patches.h`.

The **file** is always required — assets are embedded with no overlay layer, and
`recipe.Probe` hard-errors when it is missing. The **content** usually is not:
create it empty and add nothing until §5 forces you to. `aarch64-linux-musl` and
`mips64-linux-musl` are zero bytes, which is the target state for a new row.

## 4. What the probe measures, so you do not hand-write it

`internal/assets/files/patcher.c` is compiled with the target toolchain, run
under qemu, and its stdout becomes both a `config.site` (so CPython's own
configure computes pyconfig.h correctly for a target it cannot execute) and the
appended pyconfig fragment. It reports:

- every `SIZEOF_*` and `ALIGNOF_*` in `probedMacros`
- `HAVE_GCC_UINT128_T`, `HAVE_GCC_ASM_FOR_X64`, `HAVE_GCC_ASM_FOR_X87`
- `WORDS_BIGENDIAN` — integer byte order
- `DOUBLE_IS_BIG_ENDIAN_IEEE754` / `DOUBLE_IS_LITTLE_ENDIAN_IEEE754` — the byte
  order of a stored double, which need not match the integer one
- `HAVE_ALIGNED_REQUIRED` — the misaligned load, run in a forked child because
  the SIGBUS *is* the answer on a strict-alignment target

**A fragment line repeating any of these is redundant, and the fragment wins.**
`configSite` merges probe output first, then the asset, so a stale fragment
silently overrides a correct measurement. `reportOverrides` warns on every
contradiction and logs a debug line on every exact repeat — **a quiet run is the
signal the fragment can be deleted.** Check the log before you copy an existing
fragment as a template; several are still carrying dead weight:

- `arm-linux-musleabi`, `arm-linux-musleabihf`, `i386`, `riscv32` still hold
  full hand-written size/alignment tables plus `#undef HAVE_GCC_UINT128_T`.
- `powerpc64`, `powerpc64le`, `s390x` still hold endianness and
  `HAVE_ALIGNED_REQUIRED 1`.

All of that is measured now. `HAVE_ALIGNED_REQUIRED` is the one that costs
something: forcing it to 1 on a target that tolerates unaligned loads silently
gives up the faster hash, which is exactly the failure the probe exists to end.

One hard constraint: a fragment may not `#undef` a `SIZEOF_*`/`ALIGNOF_*` macro.
`configSite` refuses — configure has no way to measure it on a cross build, so
give it a value or drop the line.

## 5. What genuinely still needs a fragment

Deliberate deviations from what the hardware reports. The test: **if a program
running on the target could answer the question, it belongs in `patcher.c`, not
the fragment.**

The live example is riscv32 and riscv64:

```c
// Same musl/gcc libatomic.a quirk as riscv64; force the table fallback.
#undef HAVE___BUILTIN_CLZ
#undef HAVE_BUILTIN_ATOMIC
```

The hardware has both builtins. The fragment removes them because the musl
toolchain does not ship a usable `libatomic.a` to back them, so CPython must
take its table fallback. That is a decision about the toolchain, not a
measurement of the chip.

Two routes out of the fragment, and which one a macro takes is automatic:

- a macro with an autoconf cache variable (`autoconfCache` / `boolCache`) is
  written into `config.site` and steers configure;
- one without — `HAVE___BUILTIN_CLZ`, `HAVE_BUILTIN_ATOMIC` — is appended to the
  generated `pyconfig.h` after configure runs.

The one measurement the probe can legitimately hand back is mixed-endian
doubles: `patcher.c` prints an `#error` naming the byte order and tells you to
set `DOUBLE_IS_*` in the fragment. No current target hits this.

## 6. ELF identity (only for a genuinely new architecture)

If `arch` is not already in `identities` in `internal/ensure/elf.go`, add it —
machine, class, endianness. Verification fails with an explicit "add a row to
identities" message rather than guessing.

Two cautions:

- `archAliases` maps **`mips64el` → `mips64`, which is `ELFDATA2MSB`**. Adding a
  little-endian mips64 target through that alias would assert big-endian and
  fail at `elf:machine`. Fix the table before using it.
- `goarchIdentity` in `internal/ensure/qemu.go` is the separate map that decides
  whether this machine can run the target natively. It only matters if the new
  arch will ever be a build host.

## 7. qemu

staticpy never fetches qemu. Resolution is `--qemu <triple>=<path>` first, then
`exec.LookPath(QemuBinaryName(t))`. Provisioning is the shim's and the
container's job — for the dev container that means
`scripts/docker/fetch-qemu-user.sh` plus the `/usr/libexec/qemu-binfmt`
shims. A new triple that needs verify must be added there (package name,
sha256 for each image `TARGETARCH`) or `doctor` will show `(not found)`
and the verify job fails at launcher construction.

A target with no qemu is not a broken target. `doctor` separates *buildable*
from *runnable* deliberately.

## 8. Test expectations

Only if the target has known musl or qemu failures. `config/tests.toml`,
consumed by `ensure.LookupExpect`, which merges exactly two keys:

```toml
[[expect."<triple>".skip]]        # both runners
[[expect."<triple>:qemu".fail]]   # qemu only; ":native" for the other
```

Runner-keyed because qemu-user has its own failures around signals, threads and
subprocesses that say nothing about the build — a skip earned under qemu must
not silence the same test running natively. Every entry needs a `why`; the
string is carried into the report so a stale entry can be found by the reason
that justified it.

**An unexpected pass fails the run.** Add an entry only for something you have
actually observed failing on this target, and delete it the moment it passes.

Do not invent `[expect."<triple>"]` for a failure you have not reproduced on
a second arm. If native x86_64 static fails the same method, the scope is
`[expect.static]` or a recipe fix, not the new triple. See staticpy-traps
**Do not overfit the last failure** — agent time is cheap, the sweep is not.

Two things to know before editing this file:

- Expect keys are **not** validated. A typo'd triple is silently ignored, and
  the run then reports the failure as unexpected.
- `tests.toml` currently has `[expect.all.*]` and `[expect.static.*]` tables.
  `LookupExpect` looks up only `<triple>` and `<triple>:<runner>`, so as far as
  I can tell those two tables are never read by the Go path. Confirm before
  copying that shape for a new target — write per-triple keys.

## 9. Proven versus experimental

`status` gates four things, none of them the build itself:

- `--target proven` expands to that set; so does `staticpy print targets-proven`
- `doctor` and `staticpy help targets` label everything else
- host auto-detection: if exactly one **proven** row matches this machine's
  arch, it wins outright; otherwise all rows with that arch are considered and
  two of them is an error. **Promoting a second same-arch row to proven does not
  disambiguate — it is still one arch with two proven rows.** Marking
  `arm-linux-musleabi` proven where `arm-linux-musleabihf` already is would break
  host resolution on an arm builder.
- CI gates on proven only

Before promoting, the evidence that should exist:

1. `staticpy verify --level core --target <triple>` clean — this is the minimum.
   `core` is where a miscompiled target shows up: the numerics and containers,
   plus every extension module staticpy links in by hand, which is what a wrong
   `_sysconfigdata` or a half-linked library breaks. `smoke` proves the
   interpreter starts, which is not the same claim.
2. A probe run with **no** `fragment overrides what the probe measured` warnings.
3. Any `tests.toml` entries for the triple justified and current — no unexpected
   passes.

## 10. Verifying you got it right, cheapest first

```sh
staticpy print targets-all                  # row parses, validation passed
staticpy doctor                             # toolchain resolves; qemu resolves
staticpy build --dry-run --target <triple>  # plan builds, no missing asset
staticpy build --target <triple>            # hours
staticpy verify --level core --target <triple>
```

The first two cost seconds and catch most of §1 and §2: a missing
`maps.openssl`, a bad `status`, a `bits` that is neither 32 nor 64, and a qemu
name that resolves to nothing. `--dry-run` is what catches a missing pyconfig
asset, because `recipe.Probe` is constructed during planning.

See `staticpy-traps` for the failure catalogue. The three that bite hardest when
adding a target:

- **`-latomic`.** A target that needs it and does not declare it fails at the
  CPython link on `_Py_atomic_*`, long after the deps are built.
- **32-bit targets are where a corrupted `_sysconfigdata` shows first**, as a
  wrong `SIZEOF_VOID_P`. On a 64-bit target the host's answer and the target's
  agree often enough to hide the bug.
- **`qemu-riscv32` is absent from the Dockerfile** (§7), so riscv32 cannot be
  promoted from inside the dev container as it stands.
