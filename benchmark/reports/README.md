# Benchmark Reports -- Cliffs Notes

One-page navigation aid for the timestamped reports in this folder. Each
report's own `## Analysis` carries the mechanism story and the per-run
hypothesis updates; this file just tracks **what holds across all runs**
so a fresh reader can skip to the right report.

Last updated: **2026-05-18** -- 7 benchmark reports + 1 perf follow-up,
across 4 hosts (3 x86_64, 4 aarch64). Three PGO+LTO data points span
three µarchs (M4 Pro, Neoverse-N1, Zen 5) and both ISAs. Update on
every new report; rule lives in [`AGENTS.md`](../../AGENTS.md).

## TL;DR

- **Recipe is now PGO + whole-program `-flto-partition=none` on both
  sides.** Reports older than `2026-05-17T2146Z_aarch64.md` are pre-PGO
  and carry a banner.
- **Static beats dynamic by 7-18% CPU geomean** across the three PGO+LTO
  hosts (N1 1.07x, M4 Pro 1.11x, Zen 5 1.18x). Startup geomean 1-12%
  (Zen 5 is the bottom of spread because of one regressed scenario).
- **Static wins every CPU row except `fib_recursive`.** That row is
  0.78x / 0.89x / 0.92x and is the only consistent loss. N1 `perf stat`
  attributes it to branch-mispredict + back-end stalls in the
  computed-goto dispatch inside `_PyEval_EvalFrameDefault`; **frontend
  is not the bottleneck**, killing the L1i hypothesis. Indirect-vs-
  conditional mispredict split needs a Zen 5 perf run (Azure fences
  the relevant counters on N1).
- **`listcomp` µarch spread (1.07-1.27x) is NOT L1i thrashing** --
  N1 perf stat shows frontend stalls equal between static and dynamic.
  Win mechanism is work-elimination (fewer retired insns); spread is
  per-µarch issue-width sensitivity, not icache.
- **Dynamic baseline links Alpine apk `lib*.so.N` (un-LTO'd).** Three
  asymmetries baked into every row: (1) cross-`.so` cost, (2) the
  same cost vs Alpine libs, (3) Alpine libs not internally LTO'd.
  Untangled only by `DYNAMIC_USE_INTREE=1` (open experiment #1).
- **System python ratios are no longer meaningful.** Alpine's `-Os`
  3.12.x hasn't moved; we have.

## What wins, every PGO+LTO run, both archs

Per-row range and per-host detail. Pre-PGO numbers in parens for
context; they're noisier and biased.

| bench            | post-PGO+LTO range  | M4 / N1 / Zen5     | (pre-PGO range) | stresses                       |
|---               |---:                 |---:                |---:             |---                             |
| `arith_loop`     | **1.10x - 1.52x**   | 1.14 / 1.10 / 1.52 | (1.16 - 1.45)   | bytecode int arithmetic        |
| `str_format`     | **1.11x - 1.25x**   | 1.25 / 1.11 / 1.25 | (0.97 - 1.71)   | `_string.formatter_parser`     |
| `func_call`      | **1.06x - 1.23x**   | 1.16 / 1.06 / 1.23 | (1.10 - 1.42)   | shallow Python call dispatch   |
| `fib_iter`       | **1.08x - 1.22x**   | 1.09 / 1.08 / 1.22 | (1.08 - 1.28)   | pure loop + tuple pack/unpack  |
| `attr_access`    | **1.07x - 1.22x**   | 1.17 / 1.07 / 1.22 | (1.13 - 1.23)   | LOAD_ATTR specialisation       |
| `except_path`    | **1.09x - 1.20x**   | 1.19 / 1.09 / 1.20 | (1.24 - 1.69)   | C-level exception restore      |
| `json_roundtrip` | **1.17x - 1.22x**   | 1.22 / 1.17 / 1.19 | (0.75 - 1.30)   | `_json` C decoder              |
| `listcomp`       | **1.07x - 1.27x**   | 1.27 / 1.07 / 1.18 | (1.14 - 1.26)   | list build hot path            |
| `dictops`        | **1.02x - 1.14x**   | 1.02 / 1.08 / 1.14 | (1.13 - 1.28)   | dict insert + lookup           |
| `regex_match`    | **1.03x - 1.08x**   | 1.03 / 1.07 / 1.08 | (1.14 - 1.21)   | `_sre` C extension             |

## What is mixed or wrong

| bench / scenario | M4    | N1    | Zen 5    | summary (see linked report for mechanism)                                              |
|---               |---:   |---:   |---:      |---                                                                                     |
| `fib_recursive`  | 0.78x | 0.92x | 0.89x    | Cross-µarch, cross-ISA static loss. N1 perf stat: branch-mispredict + backend stalls in `_PyEval_EvalFrameDefault`; NOT L1i. See [0024Z perf](./2026-05-18T0024Z_aarch64_perf.md). Zen 5 perf run still owed. |
| `listcomp`       | 1.27x | 1.07x | 1.18x    | µarch-dependent. L1i hypothesis falsified by N1 perf stat. Spread is issue-width, not icache. See [0024Z perf](./2026-05-18T0024Z_aarch64_perf.md). |
| `stdlib` startup | 1.13x | 1.13x | **0.89x**| Zen 5 only -- dynamic spawns faster. Mechanism open; one data point. |

## What the static build has that the dynamic doesn't

From `sysconfig.get_config_var(...)` on each binary.

| optimisation                                | static                       | dynamic                            | system (Alpine) |
|---                                          |:-:                           |:-:                                 |:-:              |
| `-flto`                                     | yes                          | yes                                | no              |
| `-flto` *scope*                             | **whole program** (openssl, sqlite, zlib, libffi, lzma, bz2, ncursesw, libuuid all .a-LTO'd) | libpython + bundled extensions only | no |
| `-flto-partition=none`                      | yes                          | yes (via `--with-lto`)             | no              |
| `-fuse-linker-plugin` / `-ffat-lto-objects` | no (musl-cross gcc is `--disable-shared`) | yes (via `--with-lto`)  | no              |
| `-O3`                                       | yes                          | yes                                | `-Os`           |
| PGO (`--enable-optimizations`)              | yes                          | yes                                | no              |
| `-no-pie` / non-PIC                         | yes                          | no                                 | no              |
| `-ffunction-sections -fdata-sections`       | yes                          | no                                 | no              |
| `-Wl,--gc-sections -Wl,-O1 -Wl,--as-needed` | yes                          | no                                 | no              |
| C extensions in same ELF                    | yes                          | no                                 | no              |
| In-tree library pins                        | yes                          | accidental match on most libs      | n/a             |

Net: static-only structural advantages are (1) whole-program LTO scope
across system libs, (2) `-no-pie` + `--gc-sections`, (3) C extensions
linked into the same ELF.

## Open experiments (priority order)

1. **`DYNAMIC_USE_INTREE=1`** -- build openssl/sqlite/zlib/libffi/xz/bz2/
   ncurses/libuuid shared from our LTO'd sources, link dynamic against
   that, rebench. Isolates "cross-`.so` cost" from "Alpine libs aren't
   LTO'd".
2. **PGO+LTO re-bench Azure Cascade Lake (1818Z host).** Second x86_64
   µarch needed; Zen 5 amplifies bytecode-int rows by an unknown amount.
3. **Zen 5 `perf stat` on `fib_recursive` and `listcomp`.** AMD doesn't
   fence `br_indirect_spec`/`br_return_spec`/`l1i_*`; settles the BTB-
   vs-RAS-vs-conditional question the [N1 perf run](./2026-05-18T0024Z_aarch64_perf.md)
   couldn't.
4. **`stdlib` startup triage on Zen 5.** `strace -c` on the import body
   for both binaries; one-hour answer.
5. **`STATIC_NO_LTO=1`** baseline -- strip `-flto -flto-partition=none`,
   measure how much of the geomean is LTO alone.
6. **`-mcpu=native` (or per-target `-mcpu`) for native builds.** Currently
   neither binary sets any `-march`/`-mcpu`/`-mtune`; both are generic.
7. **PGO training task currently `-m test --pgo`** -- doesn't exercise
   tight recursive int arithmetic; consider adding `benchmark/microbench.py`
   to PROFILE_TASK to retrain dispatch hints for the actual benchmark mix.
8. **musl libc LTO via musl-cross-make patches** -- inlines `malloc` /
   `memchr` / `strlen` into the static binary. ~3% expected.
9. **Cross-arch PGO via qemu-user** -- `build-all.sh` currently produces
   non-PGO binaries for everything except the host arch.

## Reports index

| file                                                                                 | host                                     | era             | one-line                                                              |
|---                                                                                   |---                                       |---              |---                                                                    |
| [`2026-05-17T1818Z_x86_64.md`](./2026-05-17T1818Z_x86_64.md)                         | Azure Xeon Platinum 8370C (Cascade Lake) | pre-PGO         | First gcc 15.1.0 + bumped libs                                        |
| [`2026-05-17T1907Z_aarch64.md`](./2026-05-17T1907Z_aarch64.md)                       | Azure Neoverse-N1                        | pre-PGO         | First gcc 15.1.0 aarch64                                              |
| [`2026-05-17T1924Z_x86_64.md`](./2026-05-17T1924Z_x86_64.md)                         | Local Ryzen AI 9 HX 370 (Zen 5)          | pre-PGO         | `json_roundtrip` x86_64 regression replicated                         |
| [`2026-05-17T1955Z_aarch64.md`](./2026-05-17T1955Z_aarch64.md)                       | Apple M4 Pro via OrbStack                | pre-PGO         | `fib_recursive` 0.69x outlier; OrbStack hides PMU                     |
| [`2026-05-17T2146Z_aarch64.md`](./2026-05-17T2146Z_aarch64.md)                       | Apple M4 Pro via OrbStack                | **PGO+LTO**     | Canonical PGO+LTO baseline; geomean 1.11x; `fib_recursive` 0.78x      |
| [`2026-05-17T2359Z_aarch64.md`](./2026-05-17T2359Z_aarch64.md)                       | Azure Neoverse-N1                        | **PGO+LTO**     | First PGO+LTO on real PMU; cross-validates M4 Pro recipe; geomean 1.07x |
| [`2026-05-18T0000Z_x86_64.md`](./2026-05-18T0000Z_x86_64.md)                         | Local Ryzen AI 9 HX 370 (Zen 5)          | **PGO+LTO**     | First x86_64 PGO+LTO; resolves 1924Z `json_roundtrip`; new `stdlib` regression |
| [`2026-05-18T0024Z_aarch64_perf.md`](./2026-05-18T0024Z_aarch64_perf.md)             | Azure Neoverse-N1                        | perf follow-up  | `perf stat` on `fib_recursive` + `listcomp`; falsifies L1i hypothesis on both |

## Checklist when adding a new report

1. Run `./benchmark/run.sh` per [`AGENTS.md`](../../AGENTS.md). Mechanism
   prose, hypothesis updates, and per-run diffs go into the new report's
   own `## Analysis`, not here.
2. Update this file:
   - Bump "Last updated" + run count.
   - Update "what wins" / "what is mixed or wrong" ranges if either
     bound moved.
   - Add a row to the reports index. Era column: `PGO+LTO` for runs
     with both PGO and `-flto-partition=none` on static (current
     canonical recipe), `PGO` for PGO-only, `pre-PGO` otherwise,
     `perf follow-up` for targeted perf investigations.
   - Edit TL;DR bullets that became more or less true.
3. If you ran an "open experiments" item, strike it and turn the
   result into a TL;DR bullet.
