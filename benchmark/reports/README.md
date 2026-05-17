# Benchmark Reports -- Cliffs Notes

Short-attention-span guide to the timestamped reports in this folder. The
individual reports (`*.md`) have the full tables + analysis; this README
keeps the **recurring pattern across all runs to date** in one place so a
fresh reader doesn't have to read multiple reports to know what's known.

Last updated: **2026-05-17** -- covers 5 reports across 4 hosts
(2 x86_64, 2 aarch64); the latest is the post-`-flto-partition=none`
canonical baseline on M4 Pro. Update this file whenever you add a new
report; see [`AGENTS.md`](../../AGENTS.md) for the rule.

## TL;DR

- **PGO + whole-program `-flto-partition=none` is now on for static.**
 Dynamic baseline uses `--enable-shared --enable-optimizations
 --with-lto` (which expands to `-flto -ffat-lto-objects
 -flto-partition=none -fuse-linker-plugin`); static replicates the
 useful subset (`-flto -flto-partition=none`) via `configure-wrapper.sh`
 because our musl-cross gcc is `--disable-shared` and therefore lacks
 `liblto_plugin.so`, which `-fuse-linker-plugin` requires. Training
 task `-m test --pgo -x test_re` on both sides (musl rejects two
 locale tests). **The 4 reports before `2026-05-17T2146Z_aarch64.md`
 are pre-PGO and substantially overstate or understate the static
 win** -- they each carry a banner saying so.
- **Static + whole-program LTO + PGO beats dynamic + libpython-only-LTO
 + PGO by ~11% CPU geomean, ~12% startup geomean.** Up from the
 prior post-PGO 6% / 11%, after we matched (and extended) the LTO
 recipe on the static side. The improvement is concentrated in
 libpython-internal rows (`listcomp` jumped from parity to 1.27x,
 `except_path` from parity to 1.19x).
- **Static now wins on every CPU row except one.** Margins range from
 1.02x (`dictops`) to 1.27x (`listcomp`). The C-extension-boundary
 rows are still the widest single category (`str_format` 1.25x,
 `json_roundtrip` 1.22x, `attr_access` 1.17x), but the
 libpython-internal rows closed in this latest run and now match or
 exceed them.
- **The one row static still loses on, badly: `fib_recursive` on M4
 Pro at 0.78x.** Three rounds of optimisation knobs (PGO, then
 `-flto-partition=none`) have now had a shot at fixing it via
 `-fprofile-use` / more-aggressive inlining and have not moved the
 ratio. This is now the "live" hypothesis: structural call-site bloat
 on the recursive `_PyEval_EvalFrameDefault` path mispredicts on M4
 Pro's branch target buffer at varying recursion depths. `perf stat`
 on real aarch64 hardware (not OrbStack) is the next move.
- **The dynamic baseline links against Alpine apk's `libssl.so.3` /
 `libsqlite3.so.0` / `libcrypto.so.3` / etc., which are *not* LTO'd.**
 So three asymmetries are baked into every static-vs-dynamic row: (1)
 cross-`.so` cost at `_*.so` <-> `libpython` boundary, (2) cross-`.so`
 cost at `_*.so` <-> Alpine `lib*.so.N` boundary, (3) Alpine
 `lib*.so.N` isn't internally LTO'd. The current numbers can't
 separate these. The proposed `DYNAMIC_USE_INTREE=1` experiment
 (build the libs shared from our LTO'd sources, link dynamic Python
 against those) would isolate (1) from (2)+(3) and is the #1 missing
 experiment.
- **System python ratios are no longer meaningful comparators.** Alpine
 apk's `/usr/bin/python3` is the same `-Os` build it has always been;
 we just got much faster on both sides. `system/static` widened from
 1.23x to 1.51x, but that says more about Alpine's packaging policy
 than about us.

## What wins, every run, both archs

CPU rows where static is ahead on **every** report we've taken
(post-PGO+LTO only -- pre-PGO numbers in parens are wider and noisier):

| bench         | post-PGO+LTO range | (pre-PGO range) | what it stresses              |
|---            |---:                |---:             |---                            |
| `listcomp`    | **1.27x**          | (1.14x - 1.26x) | list build hot path           |
| `str_format`  | **1.25x**          | (0.97x - 1.71x) | `_string.formatter_parser`    |
| `json_roundtrip` | **1.22x**       | (0.75x - 1.30x) | `_json` C decoder             |
| `except_path` | **1.19x**          | (1.24x - 1.69x) | C-level exception restore     |
| `attr_access` | **1.17x**          | (1.13x - 1.23x) | LOAD_ATTR specialisation      |
| `func_call`   | **1.16x**          | (1.10x - 1.42x) | shallow Python call dispatch  |
| `arith_loop`  | **1.14x**          | (1.16x - 1.45x) | bytecode int arithmetic       |
| `fib_iter`    | **1.09x**          | (1.08x - 1.28x) | pure loop + tuple pack/unpack |
| `regex_match` | **1.03x**          | (1.14x - 1.21x) | `_sre` C extension            |
| `dictops`     | **1.02x**          | (1.13x - 1.28x) | dict insert + lookup          |

The post-PGO+LTO column has only one data point per row right now (the
2146Z aarch64 run); ranges will fill in as we re-run on x86_64. The
shape of the column changed materially after we added
`-flto-partition=none` to the static build: the libpython-internal
rows (`listcomp`, `except_path`, `func_call`, `arith_loop`) jumped
into the same 13-27% band that the C-extension-boundary rows occupy.
**The takeaway from this is that the static win is now driven by two
different effects of similar magnitude**: (a) whole-program LTO
across `libpython + libssl + libsqlite + ...` extending the inlining
visibility further than any dynamic build can do, and (b) the
intra-libpython inlining quality jump from `-flto-partition=none`,
which is also what upstream's `--with-lto` gives the dynamic
baseline's libpython but which we now also have on static. The
remaining tight rows (`regex_match`, `dictops`) are ones where the
hot path is already in a single highly-tuned C function the compiler
can't improve further.

## What is mixed or wrong

| bench            | x86_64 (pre-PGO)  | aarch64 post-PGO+LTO | notes                                                                |
|---               |---:               |---:                  |---                                                                   |
| `json_roundtrip` | **0.75x - 0.77x** | 1.22x                | x86_64 regression pre-PGO+LTO; needs re-bench with new static recipe |
| `fib_recursive`  | 0.96x - 0.99x     | **0.78x**            | M4 Pro outlier persists after PGO and `-flto-partition=none`; perf stat job |
| `str_format`     | 0.97x - 0.99x     | 1.25x                | x86_64 pre-PGO was parity; needs re-bench with new static recipe     |

The x86_64 rows in this table are still pre-PGO and pre-`-flto-partition=none`
numbers. **Both x86_64 runs in `reports/` need a re-bench with the
current static recipe** before we know how much of the
`json_roundtrip` x86_64 regression survived.

## What the static build has that the dynamic doesn't

Grounded in `sysconfig.get_config_var(...)` from each binary, not in
the build scripts.

| optimisation                                | static                              | dynamic                                   | system (Alpine apk) |
|---                                          |:-:                                  |:-:                                        |:-:                  |
| `-flto`                                     | yes                                 | yes                                       | no                  |
| `-flto` *scope*                             | **whole program** (incl. `libssl.a`, `libsqlite3.a`, `libz.a`, `libffi.a`, `liblzma.a`, `libbz2.a`, `libncursesw.a`, `libuuid.a`) | **libpython + bundled extensions only** (system `lib*.so.N` are Alpine apk, no LTO sections) | no                  |
| `-flto-partition=none`                      | **yes**                             | yes (via `--with-lto`)                    | no                  |
| `-fuse-linker-plugin`                       | no (toolchain limit -- our gcc is `--disable-shared` so no `liblto_plugin.so`) | yes (via `--with-lto`) | no |
| `-ffat-lto-objects`                         | no (slim LTO is fine for our use)   | yes (via `--with-lto`)                    | no                  |
| `-O3`                                       | yes                                 | yes                                       | `-Os` (overrides)   |
| PGO (`--enable-optimizations`)              | yes                                 | yes                                       | no                  |
| `-no-pie` / non-PIC                         | yes                                 | no                                        | no                  |
| `-ffunction-sections -fdata-sections`       | yes                                 | no                                        | no                  |
| `-Wl,--gc-sections -Wl,-O1 -Wl,--as-needed` | yes                                 | no                                        | no                  |
| C extensions linked into the same ELF       | yes                                 | no                                        | no                  |
| In-tree library pins (matched versions)     | yes                                 | accidentally yes (Alpine ships our versions for libffi 3.5.2, lzma 5.8.3, zlib 1.3.2, sqlite 3.51.2, readline 8.3, openssl 3.5.x) | n/a |

Post-PGO+LTO state of the world: **both interpreters have PGO on
libpython; both have `-flto -flto-partition=none` on libpython; but
the static side extends that LTO across every dependent `.a` archive
(openssl, sqlite, zlib, libffi, ncurses, lzma, bz2, libuuid,
readline), and the dynamic side links against Alpine's un-LTO'd
shared libraries.** The structural static-only advantages are now:
(1) whole-program LTO scope including system libs, (2) `-no-pie` +
`--gc-sections` for code size and PIC-free hot paths, (3) C
extensions linked into the same ELF instead of dlopened.

## What is missing

Highest-leverage open experiments, in priority order:

1. **`DYNAMIC_USE_INTREE=1` dynamic build.** Currently
   `dynamic-build.sh` does `apk add openssl-dev sqlite-dev zlib-dev
   ...` and links the dynamic interpreter against Alpine's un-LTO'd
   system shared libraries. The static side LTOs across all the same
   libraries from our in-tree sources. We don't know how much of the
   current 1.11x geomean is "cross-`.so` cost is real" (asymmetry 1
   in the TL;DR) vs "Alpine's system libs are un-LTO'd" (asymmetries
   2+3 in the TL;DR). The experiment: build our `openssl` /
   `sqlite` / `zlib` / `libffi` / `xz` / `bz2` / `ncurses` /
   `libuuid` shared with our `-O3 -flto -flto-partition=none`
   `CFLAGS`, install to `build-dyn-$(TARGET)/`, set `LDFLAGS_NODIST`
   to rpath that prefix, point `dynamic-build.sh` at it. If
   `str_format` / `attr_access` / `json_roundtrip` drop from
   1.17-1.25x toward 1.00-1.05x against the new "intree-dyn"
   binary, the structural static win is asymmetry 1 only and is
   small. If they stay 1.10x+, asymmetry 1 alone is real.
2. **Re-bench both x86_64 hosts with the current static recipe.** The
   published x86_64 reports predate both PGO *and*
   `-flto-partition=none`. Specifically: does the `json_roundtrip`
   x86_64 regression (~0.75x) survive on the current static build?
   Does `str_format`'s x86_64 parity (~0.99x) flip to 1.25x like on
   aarch64? Different answers per arch would be a meaningful
   arch-specific finding.
3. **`perf stat` on `fib_recursive` on real aarch64 hardware** (not
   OrbStack -- guest has no PMU). Three rounds of optimisation knobs
   (PGO, `-flto-partition=none`) have had the chance to fix the 0.78x
   gap and have not moved it. The "structural call-site bloat under
   LTO" hypothesis is now the live one. Events to capture:
   `instructions, cycles, branch-misses, iTLB-load-misses,
   L1-icache-load-misses` on a tight `fib(20)`-in-a-loop body for
   both binaries.
4. **`STATIC_NO_LTO=1` static build.** Mirror experiment to (1):
   strip `-flto -flto-partition=none` from `configure-wrapper.sh` and
   measure. If the geomean drops to ~1.00-1.02x, static linkage
   *alone* (no LTO advantage) is parity with the dynamic baseline,
   and the project is really "whole-program LTO Python that happens
   to be static". Expected drop of ~10% based on the size of the
   2112Z -> 2146Z move.
5. **musl libc LTO via musl-cross-make patches.** Now even more
   incremental on top of (1); the remaining unrecovered cross-LTO
   win is inlining through `malloc` / `memchr` / `strlen` / etc.
   into our static binary. ~20 lines of musl-cross-make patches;
   expected gain ~3% but a more novel demo (no other static-python
   project does this). Risk: have to maintain a musl-cross-make
   fork.
6. **Cross-arch PGO via qemu-user.** `build-all.sh` currently
   produces non-PGO binaries for everything except the host arch
   (Makefile auto-gates `USE_PGO=0` on cross). Add qemu-user-static
   to the container and accept the ~3x training-step slowdown, or
   stand up a CI matrix with one runner per arch. The 2 published
   x86_64 reports are the natural first re-bench targets for this.

## Reports index

| file                                                                                  | host                                        | era               | notes                                                                                                 |
|---                                                                                    |---                                          |---                |---                                                                                                    |
| [`2026-05-17T1818Z_x86_64.md`](./2026-05-17T1818Z_x86_64.md)                          | Azure Xeon Platinum 8370C (Cascade Lake)    | pre-PGO           | First gcc 15.1.0 + bumped libs run                                                                    |
| [`2026-05-17T1907Z_aarch64.md`](./2026-05-17T1907Z_aarch64.md)                        | Azure Neoverse-N1                           | pre-PGO           | First gcc 15.1.0 aarch64; readline 8.2 -> 8.3 bump                                                    |
| [`2026-05-17T1924Z_x86_64.md`](./2026-05-17T1924Z_x86_64.md)                          | Local Ryzen AI 9 HX 370 (Zen 5)             | pre-PGO           | Second x86_64 host; `json_roundtrip` x86_64 regression confirmed reproducible                         |
| [`2026-05-17T1955Z_aarch64.md`](./2026-05-17T1955Z_aarch64.md)                        | Local Apple M4 Pro via OrbStack             | pre-PGO           | `fib_recursive` 0.69x outlier; OrbStack hides PMU + cpuid + cache sizes                               |
| [`2026-05-17T2146Z_aarch64.md`](./2026-05-17T2146Z_aarch64.md)                        | Local Apple M4 Pro via OrbStack             | **PGO+LTO**       | Canonical post-PGO + `-flto-partition=none` baseline; geomean 1.11x; `fib_recursive` 0.78x persists. Supersedes the deleted 2112Z report (which had PGO but no `-flto-partition=none` on static). |

## Checklist when adding a new report

1. Run `./benchmark/run.sh` per [`AGENTS.md`](../../AGENTS.md).
2. Fill in the `## Analysis` section in the new report file. Diff
   against the most recent same-arch report.
3. **Update this README:**
   - Bump the "Last updated" date and the run-count line at the top.
   - Update the "what wins" and "what is mixed or wrong" range
     columns if the new run pushes either bound.
   - Add a row to the "Reports index" table; set the "era" column to
     `PGO+LTO` for runs with both PGO and `-flto-partition=none`
     on static (the current canonical recipe), `PGO` for PGO-only
     runs, `pre-PGO` otherwise.
   - If a TL;DR bullet became more or less true, edit it.
4. If you ran the missing PGO baseline or got `perf stat` data, strike
   the relevant item from "What is missing" and turn the result into a
   one-line TL;DR bullet.
