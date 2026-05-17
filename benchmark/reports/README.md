# Benchmark Reports Summary

Last updated: **2026-05-17** -- covers 4 runs across 4 hosts (2 x86_64,
2 aarch64). Update this file whenever you add a new report; see
[`AGENTS.md`](../../AGENTS.md) for the rule.

## TL;DR

- **Static + `-flto` is consistently faster than same-version dynamic.**
  CPU geomean **1.17x-1.24x**, startup geomean **1.13x-1.22x**. Holds
  across both archs and all four hosts measured.
- **Eval-loop / dispatch-heavy benches are where the win lives.**
  `except_path`, `func_call`, `arith_loop`, `fib_iter`, `listcomp`,
  `dictops`, `attr_access` -- all 1.2x-1.45x in static's favor on every
  host.
- **One reproducible regression: `json_roundtrip` on x86_64**
  (0.75x-0.77x, static is ~25% slower). Does **not** appear on aarch64
  (1.27x-1.30x in static's favor there). Cause is unknown; library
  vintage hypothesis already falsified.
- **The dynamic baseline is *not* PGO+LTO** -- it's stock `-O3 -Wall`
  with no `-flto`. So the headline geomean number is "static + LTO vs
  dynamic + nothing". The real apples-to-apples comparison (vs
  `--enable-optimizations --with-lto`) hasn't been run yet and is the
  single most important missing experiment.

## What wins, every run, both archs

CPU rows where static is ahead on **every** report we've taken:

| bench         | range across 4 runs       | what it stresses              |
|---            |---:                       |---                            |
| `except_path` | 1.24x - 1.69x             | C-level exception restore     |
| `func_call`   | 1.10x - 1.42x             | shallow Python call dispatch  |
| `arith_loop`  | 1.16x - 1.45x             | bytecode int arithmetic       |
| `fib_iter`    | 1.08x - 1.28x             | pure loop + tuple pack/unpack |
| `listcomp`    | 1.14x - 1.26x             | list build hot path           |
| `dictops`     | 1.13x - 1.28x             | dict insert + lookup          |
| `attr_access` | 1.13x - 1.23x             | LOAD_ATTR specialisation      |
| `regex_match` | 1.14x - 1.21x             | `_sre` C extension            |

Pattern is consistent with LTO inlining: the bytecode dispatch loop
calls into C helpers (or its own static inlines) without crossing a
`libpython3.13.so` -> `_*.so` boundary, so the compiler gets to see
both sides.

## What is mixed or wrong

| bench            | x86_64                | aarch64               | notes                                         |
|---               |---:                   |---:                   |---                                            |
| `json_roundtrip` | **0.75x - 0.77x**     | 1.27x - 1.30x         | x86_64 regression is robust across 2 hosts    |
| `fib_recursive`  | 0.96x - 0.99x         | 0.69x - 1.04x         | M4 Pro outlier at 0.69x; flat everywhere else |
| `str_format`     | 0.97x - 0.99x         | **1.31x - 1.71x**     | aarch64 win is huge, x86_64 is parity         |

So three out of eleven rows have arch-dependent direction. **None** of
them have a single coherent story yet.

## What the static build has that the dynamic doesn't

Grounded in `sysconfig.get_config_var(...)` from each binary, not in the
build scripts.

| optimisation                                | static | dynamic | system (Alpine apk) |
|---                                          |:-:     |:-:      |:-:                  |
| `-flto` (compile + link)                    | yes    | no      | no                  |
| `-O3`                                       | yes    | yes     | `-Os` (overrides)   |
| `-no-pie` / non-PIC                         | yes    | no      | no                  |
| `-ffunction-sections -fdata-sections`       | yes    | no      | no                  |
| `-Wl,--gc-sections -Wl,-O1 -Wl,--as-needed` | yes    | no      | no                  |
| C extensions linked into the same ELF       | yes    | no      | no                  |
| In-tree library pins (matched versions)     | yes    | no      | no                  |
| PGO (`--enable-optimizations`)              | **no** | **no**  | **no**              |

All three test interpreters are **non-PGO**. Toolchain is effectively
identical (static gcc 15.1.0, dynamic apk gcc 15.2.0).

## What is missing

Highest-leverage open experiments, in priority order:

1. **PGO dynamic baseline.** Build a same-version dynamic Python with
   `--enable-shared --enable-optimizations --with-lto` and add it as a
   fourth column. Likely halves our visible static-vs-dynamic margin on
   eval-loop rows. Until this exists, the headline "1.2x" number is
   *not* "static + LTO vs dynamic + LTO + PGO" -- it's "static + LTO vs
   dynamic + nothing".
2. **`perf stat` on `json_roundtrip` on x86_64**, both binaries:
   `cycles, instructions, branch-misses, L1-icache-load-misses,
   iTLB-load-misses`. Settles whether the x86_64 regression is "static
   retires more instructions" (LTO bloated the decoder) or "same
   instructions, slower" (icache pressure on the 15 MB static binary).
   *Can't run on Apple silicon under OrbStack -- the guest has no PMU.
   Use the Azure x86_64 host or any bare-metal Linux box.*
3. **`perf stat` on `fib_recursive` on Apple M4 Pro** to characterise
   the 0.69x outlier. Same blocker (no PMU under OrbStack); rerun on a
   bare-metal aarch64 box if you can find one.
4. **Repeat-build variance.** 3-5 cycles of
   `make clean && make python3 && ./benchmark/dynamic-build.sh &&
   ./benchmark/run.sh`; report median-of-medians. Static + LTO +
   `--gc-sections` is known to be sensitive to relink layout on tight
   recursive rows. Would tell us how much of the per-row noise (~3-5%)
   is real and how much is build-to-build relink jitter.

## Reports index

| file                                                       | host                                        | notes                                                                                                 |
|---                                                         |---                                          |---                                                                                                    |
| [`2026-05-17T1818Z_x86_64.md`](./2026-05-17T1818Z_x86_64.md) | Azure Xeon Platinum 8370C (Cascade Lake)    | First gcc 15.1.0 + bumped libs run                                                                    |
| [`2026-05-17T1907Z_aarch64.md`](./2026-05-17T1907Z_aarch64.md) | Azure Neoverse-N1                           | First gcc 15.1.0 aarch64; readline 8.2 -> 8.3 bump                                                    |
| [`2026-05-17T1924Z_x86_64.md`](./2026-05-17T1924Z_x86_64.md) | Local Ryzen AI 9 HX 370 (Zen 5)             | Second x86_64 host; `json_roundtrip` x86_64 regression confirmed reproducible                         |
| [`2026-05-17T1955Z_aarch64.md`](./2026-05-17T1955Z_aarch64.md) | Local Apple M4 Pro via OrbStack             | `fib_recursive` 0.69x outlier; OrbStack hides PMU + cpuid + cache sizes                               |

## Checklist when adding a new report

1. Run `./benchmark/run.sh` per [`AGENTS.md`](../../AGENTS.md).
2. Fill in the `## Analysis` section in the new report file. Diff
   against the most recent same-arch report.
3. **Update this README:**
   - Bump the "Last updated" date and the run count at the top.
   - Update the "what wins" and "what is mixed or wrong" range columns
     if the new run pushes either bound.
   - Add a row to the "Reports index" table.
   - If a new bench moved from "wins everywhere" to "mixed", or a
     regression closed, update the TL;DR.
4. If you ran the missing PGO baseline or got `perf stat` data, strike
   the relevant item from "What is missing" and turn the result into a
   one-line TL;DR bullet.
