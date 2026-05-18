# Benchmark Reports -- Cliffs Notes

Short-attention-span guide to the timestamped reports in this folder. The
individual reports (`*.md`) have the full tables + analysis; this README
keeps the **recurring pattern across all runs to date** in one place so a
fresh reader doesn't have to read multiple reports to know what's known.

Last updated: **2026-05-18** -- covers 7 reports across 4 hosts
(3 x86_64, 4 aarch64); we now have **three** PGO+LTO data points
across three µarchs (M4 Pro Firestorm, Neoverse-N1, Zen 5), spanning
both ISAs. Update this file whenever you add a new report; see
[`AGENTS.md`](../../AGENTS.md) for the rule.

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
 + PGO by 7-18% CPU geomean across three µarchs.** Per-host: 1.07x on
 Azure Neoverse-N1 aarch64 (2359Z), 1.11x on Apple M4 Pro aarch64
 (2146Z), 1.18x on AMD Zen 5 x86_64 (0000Z). The Zen 5 number is the
 widest and is driven by big wins on `arith_loop` (1.52x), `func_call`
 (1.23x), and `fib_iter` (1.22x) -- bytecode-int rows where Zen 5's
 wider integer issue width compounds with the static build's `-no-pie
 -ffunction-sections --gc-sections`.
- **Startup geomean is 1-12% across the three PGO+LTO hosts and the
 spread is large.** 2359Z N1 1.09x, 2146Z M4 1.12x, 0000Z Zen 5
 **1.01x**. The Zen 5 number is bottom-of-spread because of one
 scenario (`stdlib` flipped from 1.13x to 0.89x -- dynamic spawns
 faster). The "static wins every startup scenario" claim from the
 pre-PGO era no longer holds on x86_64; both aarch64 hosts still
 hold all three scenarios. Mechanism is open; one-data-point.
- **Static now wins on every CPU row except one.** Margins range from
 1.02x (`dictops` on M4 Pro) to 1.52x (`arith_loop` on Zen 5). The
 C-extension-boundary rows are the widest cross-host category
 (`str_format` 1.11-1.25x, `json_roundtrip` 1.17-1.22x, `attr_access`
 1.07-1.22x). Libpython-internal rows are narrower and more
 µarch-sensitive (`listcomp` 1.07-1.27x, `except_path` 1.09-1.20x,
 `func_call` 1.06-1.23x).
- **The one row static loses on, on all three hosts: `fib_recursive`.**
 0.78x on M4 Pro, 0.89x on Zen 5, 0.92x on N1. Three rounds of
 optimisation knobs (PGO, then `-flto-partition=none`) have had a shot
 at fixing this via `-fprofile-use` / more-aggressive inlining and
 have not moved the ratio on any of the three. **Cross-isa,
 cross-vendor, cross-µarch** evidence for the "structural call-site
 bloat on the recursive `_PyEval_EvalFrameDefault` path under
 `-flto-partition=none`" hypothesis. `perf stat` runs cleanly on both
 N1 and Zen 5 (real PMUs); waiting on neither.
- **The `listcomp` win is sharply µarch-dependent and the L1i story
 is at best partial.** M4 Pro (192K L1i) 1.27x, Zen 5 (32K L1i)
 1.18x, N1 (64K L1i) 1.07x. The 2359Z L1i-thrashing hypothesis
 predicts a monotonic relationship between L1i size and `listcomp`
 ratio; the Zen 5 number sits between M4 Pro and N1 despite having
 the smallest L1i, which is consistent with Zen 5's op cache
 effectively acting as a second-level icache for hot loops. Needs a
 `perf -e L1-icache-load-misses` triangulation on all three hosts.
- **The pre-PGO `json_roundtrip` x86_64 regression is resolved.** The
 1924Z 0.75x became 1.19x once PGO landed on the static side. The
 1924Z analysis's "static + `-O3 -flto` on the JSON decoder is
 25-30% slower than the dynamic build's per-TU compilation" was
 falsified by simply turning on `--enable-optimizations`.
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

| bench            | post-PGO+LTO range  | M4 / N1 / Zen5  | (pre-PGO range) | what it stresses              |
|---               |---:                 |---:             |---:             |---                            |
| `arith_loop`     | **1.10x - 1.52x**   | 1.14 / 1.10 / 1.52 | (1.16x - 1.45x) | bytecode int arithmetic       |
| `str_format`     | **1.11x - 1.25x**   | 1.25 / 1.11 / 1.25 | (0.97x - 1.71x) | `_string.formatter_parser`    |
| `func_call`      | **1.06x - 1.23x**   | 1.16 / 1.06 / 1.23 | (1.10x - 1.42x) | shallow Python call dispatch  |
| `fib_iter`       | **1.08x - 1.22x**   | 1.09 / 1.08 / 1.22 | (1.08x - 1.28x) | pure loop + tuple pack/unpack |
| `attr_access`    | **1.07x - 1.22x**   | 1.17 / 1.07 / 1.22 | (1.13x - 1.23x) | LOAD_ATTR specialisation      |
| `except_path`    | **1.09x - 1.20x**   | 1.19 / 1.09 / 1.20 | (1.24x - 1.69x) | C-level exception restore     |
| `json_roundtrip` | **1.17x - 1.22x**   | 1.22 / 1.17 / 1.19 | (0.75x - 1.30x) | `_json` C decoder             |
| `listcomp`       | **1.07x - 1.27x**   | 1.27 / 1.07 / 1.18 | (1.14x - 1.26x) | list build hot path           |
| `dictops`        | **1.02x - 1.14x**   | 1.02 / 1.08 / 1.14 | (1.13x - 1.28x) | dict insert + lookup          |
| `regex_match`    | **1.03x - 1.08x**   | 1.03 / 1.07 / 1.08 | (1.14x - 1.21x) | `_sre` C extension            |

Three data points now: M4 Pro aarch64 (2146Z) via OrbStack, Azure
Neoverse-N1 aarch64 (2359Z) on bare-metal, and AMD Zen 5 x86_64
(0000Z) on bare-metal. All three rebench the same canonical PGO +
`-flto-partition=none` static recipe against an `--enable-optimizations
--with-lto` dynamic. The shape changed materially after we added
`-flto-partition=none` to the static build: the libpython-internal
rows (`listcomp`, `except_path`, `func_call`, `arith_loop`) jumped
into the same 7-52% band that the C-extension-boundary rows occupy.
**The takeaway is that the static win is now driven by two different
effects of similar magnitude**: (a) whole-program LTO across
`libpython + libssl + libsqlite + ...` extending the inlining
visibility further than any dynamic build can do, and (b) the
intra-libpython inlining quality jump from `-flto-partition=none`,
which is also what upstream's `--with-lto` gives the dynamic baseline's
libpython but which we now also have on static.

**Per-row µarch sensitivity is real.** `arith_loop`, `fib_iter`, and
`func_call` all swing 12-42 ratio-points across the three hosts and
they all peak on Zen 5 -- consistent with Zen 5's wider integer
issue width amplifying the static `-no-pie -ffunction-sections
--gc-sections` advantage on tight bytecode-int loops. `listcomp`
non-monotonically tracks L1i size (M4 192K > Zen 5 32K > N1 64K with
ratios 1.27 > 1.18 > 1.07), which is the "what is mixed" item below.
The tight rows (`regex_match`, `dictops`) are ones where the hot
path is already in a single highly-tuned C function the compiler
can't improve further; they sit in 1.02-1.14x across all three hosts.

## What is mixed or wrong

| bench / scenario  | M4 Pro | N1    | Zen 5    | notes                                                                                |
|---                |---:    |---:   |---:      |---                                                                                   |
| `fib_recursive`   | 0.78x  | 0.92x | 0.89x    | **Cross-µarch, cross-ISA static loss** after PGO + `-flto-partition=none`; perf stat |
| `listcomp`        | 1.27x  | 1.07x | 1.18x    | µarch-dependent; 2359Z L1i-thrashing hypothesis only partial (Zen 5 sits in middle on smaller L1i) |
| `stdlib` startup  | 1.13x  | 1.13x | **0.89x**| Zen-5-only: dynamic spawns faster on stdlib import. One data point, x86_64-specific |

Down from three pre-PGO+LTO rows. Resolved since the last edit:
`json_roundtrip` x86_64 regression (1924Z 0.75x -> 0000Z 1.19x),
`str_format` x86_64 parity (1924Z 0.97x -> 0000Z 1.25x). Both now
sit in the "what wins" table above.

`fib_recursive` is the **only consistent CPU loss across all three
PGO+LTO hosts** and the canonical perf-stat target. Both N1 and Zen 5
have real PMUs.

`listcomp` and `stdlib` startup are µarch-conditional: `listcomp`
wins on all three but with a 20-ratio-point spread, `stdlib` wins on
both aarch64 hosts but loses on the one x86_64 host. Both need a
follow-up `perf` invocation before they leave this table.

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
2. ~~**Re-bench both x86_64 hosts with the current static recipe.**~~
   **Half-done.** The Ryzen AI HX 370 host got its PGO+LTO re-bench in
   `2026-05-18T0000Z_x86_64.md` -- `json_roundtrip` regression resolved
   (0.75x -> 1.19x), `str_format` parity flipped to win (0.99x ->
   1.25x), new findings folded into the tables above. **Azure Xeon
   Platinum 8370C (1818Z host) still owes a PGO+LTO re-bench** to
   confirm the cross-host story on Cascade Lake -- second x86_64
   µarch matters because Zen 5 amplifies the bytecode-int rows by an
   amount we don't yet know is Zen-specific.
3. **`perf stat -e cycles,instructions,branch-misses,iTLB-load-misses,
   L1-icache-load-misses` on `fib_recursive`, on both N1 and Zen 5,
   both binaries.** No longer waiting on PMU access -- we now have two
   real-PMU hosts (N1 2359Z, Zen 5 0000Z) showing the same regression
   at different magnitudes (0.92x and 0.89x). Cross-host agreement on
   the offending counter narrows the mechanism better than any single
   host can. Promoted ahead of (1) by 2359Z and confirmed by 0000Z.
4. **`perf stat -e L1-icache-load-misses,L1-icache-loads` on
   `listcomp` on N1, M4 Pro, and Zen 5.** Tests the 2359Z L1i-thrashing
   hypothesis against the 0000Z counter-evidence (Zen 5 holds 1.18x on
   a 32K L1i, smaller than N1's 64K, which the L1i hypothesis doesn't
   predict). One `perf` invocation per host settles whether L1i miss
   rate or Zen 5's op cache is doing the work.
5. **`stdlib` startup regression triage on Zen 5.** New 0000Z finding:
   dynamic spawns faster on the `stdlib` startup scenario (0.89x)
   despite both aarch64 hosts holding 1.13x. Either Ryzen-host
   scheduling artefact, or the dynamic-side PGO captured something
   about the import path on musl-x86_64 that it didn't capture on
   musl-aarch64. `strace -e trace=openat,read,mmap,close -c` on the
   `python -c 'import sys, ...stdlib...'` body for both binaries is a
   one-hour answer.
6. **`STATIC_NO_LTO=1` static build.** Mirror experiment to (1):
   strip `-flto -flto-partition=none` from `configure-wrapper.sh` and
   measure. If the geomean drops to ~1.00-1.02x, static linkage
   *alone* (no LTO advantage) is parity with the dynamic baseline,
   and the project is really "whole-program LTO Python that happens
   to be static". Expected drop of ~10% based on the size of the
   2112Z -> 2146Z move.
7. **musl libc LTO via musl-cross-make patches.** Now even more
   incremental on top of (1); the remaining unrecovered cross-LTO
   win is inlining through `malloc` / `memchr` / `strlen` / etc.
   into our static binary. ~20 lines of musl-cross-make patches;
   expected gain ~3% but a more novel demo (no other static-python
   project does this). Risk: have to maintain a musl-cross-make
   fork.
8. **Cross-arch PGO via qemu-user.** `build-all.sh` currently
   produces non-PGO binaries for everything except the host arch
   (Makefile auto-gates `USE_PGO=0` on cross). Add qemu-user-static
   to the container and accept the ~3x training-step slowdown, or
   stand up a CI matrix with one runner per arch.    The pre-PGO reports (1818Z Cascade Lake, 1907Z Neoverse-N1, 1955Z
   M4 Pro) are the natural first re-bench targets.

## Reports index

| file                                                                                  | host                                        | era               | notes                                                                                                 |
|---                                                                                    |---                                          |---                |---                                                                                                    |
| [`2026-05-17T1818Z_x86_64.md`](./2026-05-17T1818Z_x86_64.md)                          | Azure Xeon Platinum 8370C (Cascade Lake)    | pre-PGO           | First gcc 15.1.0 + bumped libs run                                                                    |
| [`2026-05-17T1907Z_aarch64.md`](./2026-05-17T1907Z_aarch64.md)                        | Azure Neoverse-N1                           | pre-PGO           | First gcc 15.1.0 aarch64; readline 8.2 -> 8.3 bump                                                    |
| [`2026-05-17T1924Z_x86_64.md`](./2026-05-17T1924Z_x86_64.md)                          | Local Ryzen AI 9 HX 370 (Zen 5)             | pre-PGO           | Second x86_64 host; `json_roundtrip` x86_64 regression confirmed reproducible                         |
| [`2026-05-17T1955Z_aarch64.md`](./2026-05-17T1955Z_aarch64.md)                        | Local Apple M4 Pro via OrbStack             | pre-PGO           | `fib_recursive` 0.69x outlier; OrbStack hides PMU + cpuid + cache sizes                               |
| [`2026-05-17T2146Z_aarch64.md`](./2026-05-17T2146Z_aarch64.md)                        | Local Apple M4 Pro via OrbStack             | **PGO+LTO**       | Canonical post-PGO + `-flto-partition=none` baseline; geomean 1.11x; `fib_recursive` 0.78x persists. Supersedes the deleted 2112Z report (which had PGO but no `-flto-partition=none` on static). |
| [`2026-05-17T2359Z_aarch64.md`](./2026-05-17T2359Z_aarch64.md)                        | Azure Neoverse-N1                           | **PGO+LTO**       | First PGO+LTO on real-PMU hardware. Cross-validates the M4 Pro PGO+LTO recipe. Geomean 1.07x (narrower than M4 Pro 1.11x). `fib_recursive` 0.92x replicates the M4 Pro loss on a totally different aarch64 µarch. `listcomp` did NOT replicate (1.07x vs 1.27x on M4 Pro); 2359Z proposes L1i-thrashing hypothesis. |
| [`2026-05-18T0000Z_x86_64.md`](./2026-05-18T0000Z_x86_64.md)                          | Local Ryzen AI 9 HX 370 (Zen 5)             | **PGO+LTO**       | First x86_64 PGO+LTO report. Same host as 1924Z; PGO unlocked via `BUGREPORT.md` musl-fma workaround. Resolves the 1924Z `json_roundtrip` regression (0.75x -> 1.19x). Confirms `fib_recursive` as a three-host loss. `listcomp` 1.18x on a 32K L1i partially falsifies 2359Z's L1i-thrashing hypothesis. New: `stdlib` startup 0.89x (dynamic spawns faster). |

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
