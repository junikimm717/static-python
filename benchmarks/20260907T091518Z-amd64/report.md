# pyperformance comparison

## Environment

- protocol: 2
- python_version: 3.14.7
- git_revision: dd9254a03cc19f599973fb8b61333ac2712b3ed2
- kit_version: 1
- triple: x86_64-linux-musl
- suite: pyperformance 1.14.0, pyperf 2.10.0
- kernel: Linux 6.12.94+deb13-amd64
- cpu: AMD Ryzen 5 3600 6-Core Processor
- memory: 62.7 GiB (61.7 GiB available)
- logical cores: 2
- caches: L1d 32K / L1i 32K / L2 512K / L3 16384K
- topology: 12 logical cpus, hybrid (6×4.21GHz, 6×capacity=0)
- affinity: pinned to cpu3
- fingerprint: 533a1daa3163da8450a0ff591f83b66a6b66b5d790e21096dfe7340ecd3998ee
- microcode: 0x8701034
- smt: active=0 control=off threads_per_core=1
- vulnerabilities: 0 vulnerable / 6 mitigated / 11 other (see env.json)
- clocksource: tsc
- baseline: reference
- rows: 79
- skipped: 8 (see skipped.json)

## Interpreters

| label | sha256 | linkage | lto | allocator | pgo | size |
|---|---|---|---|---|---|---:|
| default | `9fe62dc2101d` | static | whole-graph | mimalloc | yes | 19.0 MiB |
| nomimalloc | `3fa093077a0e` | static | whole-graph | musl | yes | 18.9 MiB |
| nolto | `0ac27442d00e` | static | none | mimalloc | yes | 18.0 MiB |
| nolto-nomimalloc | `aca8be25497e` | static | none | musl | yes | 17.9 MiB |
| seplto | `871c05be40d0` | static | per-dep | mimalloc | yes | 18.2 MiB |
| seplto-nomimalloc | `bdcc2d89986f` | static | per-dep | musl | yes | 18.1 MiB |
| reference | `39bfc42c796f` | dynamic | whole-graph | glibc | yes | 17.9 KiB |
| reference-nolto | `991770d17431` | dynamic | none | glibc | yes | 17.4 KiB |
| reference-mimalloc | `5da79ba2aa7f` | dynamic | whole-graph | mimalloc | yes | 230.6 KiB |
| reference-nolto-mimalloc | `d07ccc05e67c` | dynamic | none | mimalloc | yes | 230.1 KiB |

| benchmark | default | nomimalloc | nolto | nolto-nomimalloc | seplto | seplto-nomimalloc | reference | reference-nolto | reference-mimalloc | reference-nolto-mimalloc |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 2to3 | 1.08x | 1.01x | 1.01x | 0.97x | 1.07x | 1.02x | 1.00x | 0.88x | 0.99x | 0.85x |
| ascii85_large | 1.13x | 0.98x | 1.07x | 0.96x | 1.12x | 1.00x | 1.00x | 0.93x | 0.99x | 0.87x |
| ascii85_small | 1.14x | 1.07x | 1.06x | 1.02x | 1.13x | 1.07x | 1.00x | 0.91x | 1.00x | 0.85x |
| async_generators | 1.34x | 1.34x | 1.20x | 1.24x | 1.32x | 1.35x | 1.00x | 0.84x | 1.00x | 0.79x |
| async_tree_none | 1.11x | 1.10x | 1.04x | 1.05x | 1.08x | 1.09x | 1.00x | 0.88x | 0.99x | 0.87x |
| asyncio_tcp | 0.78x | 0.17x | 0.77x | 0.17x | 0.77x | 0.17x | 1.00x | 0.98x | 0.96x | 0.95x |
| asyncio_websockets | 1.02x | 0.82x | 1.02x | 0.82x | 1.03x | 0.83x | 1.00x | 1.00x | 1.03x | 1.03x |
| base16_large | 1.05x | 0.60x | 0.91x | 0.55x | 1.05x | 0.60x | 1.00x | 0.95x | 0.95x | 0.87x |
| base16_small | 1.09x | 1.00x | 1.00x | 0.93x | 1.10x | 0.99x | 1.00x | 0.93x | 0.92x | 0.92x |
| base32_large | 1.27x | 1.20x | 1.08x | 1.08x | 1.23x | 1.18x | 1.00x | 0.86x | 0.99x | 0.81x |
| base32_small | 1.24x | 1.23x | 1.05x | 1.09x | 1.20x | 1.19x | 1.00x | 0.85x | 0.99x | 0.80x |
| base64_large | 0.62x | 0.53x | 0.67x | 0.60x | 0.62x | 0.50x | 1.00x | 1.09x | 1.00x | 1.08x |
| base64_small | 0.83x | 0.83x | 0.87x | 0.88x | 0.85x | 0.85x | 1.00x | 0.90x | 0.98x | 0.93x |
| base85_large | 1.17x | 1.04x | 1.12x | 0.97x | 1.23x | 1.04x | 1.00x | 0.87x | 0.98x | 0.82x |
| base85_small | 1.18x | 1.12x | 1.10x | 1.03x | 1.23x | 1.11x | 1.00x | 0.86x | 0.99x | 0.81x |
| bench_mp_pool | 1.07x | 0.95x | 1.03x | 1.05x | 1.09x | 1.03x | 1.00x | 0.95x | 0.97x | 0.90x |
| bench_thread_pool | 1.00x | 1.00x | 0.97x | 0.96x | 0.99x | 0.98x | 1.00x | 0.93x | 0.93x | 0.85x |
| bpe_tokeniser | 1.22x | 1.14x | 1.07x | 1.01x | 1.23x | 1.15x | 1.00x | 0.82x | 0.97x | 0.78x |
| chameleon | 1.11x | 0.98x | 0.97x | 0.89x | 1.11x | 0.99x | 1.00x | 0.86x | 1.00x | 0.84x |
| chaos | 1.21x | 1.15x | 1.04x | 1.10x | 1.20x | 1.20x | 1.00x | 0.81x | 0.98x | 0.84x |
| comprehensions | 1.10x | 1.08x | 0.99x | 1.01x | 1.09x | 1.09x | 1.00x | 0.89x | 0.98x | 0.87x |
| coroutines | 1.20x | 1.15x | 1.07x | 0.99x | 1.32x | 1.34x | 1.00x | 0.89x | 1.00x | 0.91x |
| coverage | 2.25x | 1.07x | 1.04x | 0.99x | 1.15x | 1.07x | 1.00x | 0.82x | 0.95x | 0.79x |
| create_gc_cycles | 1.02x | 1.03x | 0.94x | 0.89x | 1.03x | 1.02x | 1.00x | 0.86x | 1.00x | 0.88x |
| crypto_pyaes | 1.24x | 1.20x | 1.14x | 1.10x | 1.23x | 1.18x | 1.00x | 0.96x | 1.00x | 0.95x |
| decimal_factorial | 1.17x | 1.16x | 1.14x | 1.08x | 1.19x | 1.18x | 1.00x | 0.93x | 1.00x | 0.92x |
| decimal_pi | 1.40x | 1.43x | 1.24x | 1.23x | 1.36x | 1.46x | 1.00x | 0.94x | 1.01x | 0.92x |
| deepcopy | 1.13x | 1.12x | 1.00x | 1.03x | 1.12x | 1.12x | 1.00x | 0.82x | 0.96x | 0.79x |
| deepcopy_memo | 1.12x | 1.06x | 1.01x | 1.02x | 1.09x | 1.06x | 1.00x | 0.93x | 0.96x | 0.89x |
| deepcopy_reduce | 1.11x | 1.10x | 0.97x | 1.03x | 1.10x | 1.11x | 1.00x | 0.79x | 0.93x | 0.75x |
| deltablue | 1.03x | 1.03x | 0.96x | 1.00x | 0.96x | 1.02x | 1.00x | 0.88x | 0.99x | 0.80x |
| docutils | 0.99x | 0.95x | 0.93x | 0.91x | 0.99x | 0.95x | 1.00x | 0.88x | 0.98x | 0.86x |
| fannkuch | 1.18x | 1.22x | 1.07x | 1.09x | 1.24x | 1.23x | 1.00x | 0.81x | 0.99x | 0.83x |
| float | 1.22x | 1.20x | 1.09x | 1.11x | 1.22x | 1.23x | 1.00x | 0.85x | 1.01x | 0.86x |
| gc_traversal | 0.98x | 0.98x | 0.99x | 0.92x | 1.00x | 0.98x | 1.00x | 0.87x | 1.01x | 0.91x |
| generators | 1.16x | 1.05x | 1.10x | 1.06x | 1.10x | 1.09x | 1.00x | 0.94x | 0.94x | 0.93x |
| genshi_text | 1.09x | 1.05x | 1.01x | 1.01x | 1.09x | 1.04x | 1.00x | 0.84x | 0.95x | 0.81x |
| go | 1.07x | 1.06x | 1.03x | 1.03x | 1.05x | 1.06x | 1.00x | 0.95x | 0.99x | 0.94x |
| hexiom | 1.01x | 1.03x | 0.98x | 1.03x | 0.99x | 1.04x | 1.00x | 0.91x | 0.96x | 0.85x |
| html5lib | 1.03x | 1.01x | 0.98x | 0.97x | 1.02x | 1.00x | 1.00x | 0.90x | 0.96x | 0.88x |
| json_dumps | 1.23x | 1.21x | 1.14x | 1.09x | 1.26x | 1.19x | 1.00x | 0.95x | 1.04x | 0.93x |
| json_loads | 1.21x | 1.12x | 1.06x | 0.66x | 1.16x | 1.19x | 1.00x | 0.92x | 0.96x | 0.91x |
| logging_format | 1.07x | 1.08x | 1.02x | 1.04x | 1.08x | 1.11x | 1.00x | 0.84x | 0.99x | 0.86x |
| mako | 1.40x | 1.06x | 1.26x | 0.99x | 1.42x | 1.06x | 1.00x | 0.97x | 1.27x | 1.04x |
| many_optionals | 1.11x | 1.09x | 1.04x | 1.04x | 1.11x | 1.08x | 1.00x | 0.88x | 1.00x | 0.87x |
| mdp | 1.17x | 1.17x | 1.04x | 1.06x | 1.13x | 1.16x | 1.00x | 0.82x | 0.99x | 0.82x |
| meteor_contest | 1.10x | 1.12x | 1.07x | 1.09x | 1.10x | 1.11x | 1.00x | 0.97x | 1.02x | 0.95x |
| nbody | 1.38x | 1.38x | 1.17x | 1.22x | 1.41x | 1.43x | 1.00x | 0.89x | 0.99x | 0.89x |
| nqueens | 1.22x | 1.22x | 1.04x | 1.09x | 1.18x | 1.16x | 1.00x | 0.81x | 0.96x | 0.76x |
| pathlib | 1.08x | 1.07x | 1.02x | 1.02x | 1.06x | 1.06x | 1.00x | 0.87x | 0.99x | 0.85x |
| pickle | 1.20x | 1.15x | 1.05x | 1.01x | 1.20x | 1.14x | 1.00x | 0.87x | 0.98x | 0.93x |
| pidigits | 1.09x | 0.61x | 1.08x | 0.61x | 1.09x | 0.61x | 1.00x | 1.03x | 1.01x | 1.01x |
| pprint_pformat | 1.11x | 1.10x | 0.97x | 1.00x | 1.12x | 1.10x | 1.00x | 0.83x | 0.96x | 0.80x |
| pprint_safe_repr | 1.11x | 1.09x | 0.96x | 1.00x | 1.12x | 1.10x | 1.00x | 0.83x | 0.96x | 0.80x |
| pyflate | 1.06x | 0.98x | 0.99x | 0.93x | 1.06x | 0.97x | 1.00x | 0.90x | 0.97x | 0.89x |
| python_startup | 1.09x | 0.94x | 1.05x | 0.91x | 1.09x | 0.94x | 1.00x | 0.90x | 0.95x | 0.87x |
| quadtree_nbody | 1.32x | 1.29x | 1.17x | 1.16x | 1.31x | 1.34x | 1.00x | 0.87x | 1.00x | 0.85x |
| raytrace | 1.15x | 1.15x | 1.04x | 1.05x | 1.15x | 1.17x | 1.00x | 0.82x | 1.00x | 0.83x |
| regex_compile | 1.15x | 1.11x | 1.04x | 1.06x | 1.13x | 1.12x | 1.00x | 0.92x | 1.02x | 0.90x |
| regex_dna | 0.98x | 0.84x | 0.96x | 0.84x | 0.96x | 0.85x | 1.00x | 0.98x | 1.02x | 0.98x |
| regex_effbot | 0.91x | 0.87x | 0.92x | 0.87x | 0.87x | 0.90x | 1.00x | 0.90x | 0.96x | 0.92x |
| regex_v8 | 1.01x | 0.93x | 0.98x | 0.91x | 1.01x | 0.90x | 1.00x | 0.91x | 0.98x | 0.95x |
| richards | 1.08x | 1.01x | 1.01x | 1.02x | 1.03x | 1.06x | 1.00x | 0.91x | 1.00x | 0.90x |
| richards_super | 1.04x | 1.02x | 0.97x | 1.00x | 1.02x | 1.03x | 1.00x | 0.88x | 1.00x | 0.87x |
| scimark_fft | 1.45x | 1.46x | 1.18x | 1.25x | 1.46x | 1.51x | 1.00x | 0.90x | 1.02x | 0.86x |
| shortest_path | 1.00x | 0.94x | 0.99x | 0.94x | 1.01x | 0.94x | 1.00x | 0.98x | 1.01x | 1.00x |
| spectral_norm | 1.28x | 1.29x | 1.13x | 1.16x | 1.39x | 1.22x | 1.00x | 0.86x | 1.01x | 0.83x |
| sphinx | 1.02x | 0.98x | 0.95x | 0.94x | 1.02x | 0.98x | 1.00x | 0.86x | 0.97x | 0.85x |
| sqlglot_v2_parse | 1.08x | 1.07x | 1.00x | 1.03x | 1.08x | 1.07x | 1.00x | 0.86x | 0.97x | 0.86x |
| sqlite_synth | 1.13x | 1.13x | 1.00x | 1.08x | 1.10x | 1.09x | 1.00x | 0.84x | 0.97x | 0.80x |
| telco | 1.36x | 1.24x | 1.10x | 1.09x | 1.33x | 1.23x | 1.00x | 0.87x | 0.98x | 0.79x |
| tomli_loads | 1.05x | 1.08x | 0.99x | 1.01x | 1.05x | 1.04x | 1.00x | 0.88x | 0.93x | 0.84x |
| tornado_http | 1.11x | 0.78x | 1.06x | 0.76x | 1.09x | 0.78x | 1.00x | 0.91x | 1.03x | 0.94x |
| typing_runtime_protocols | 1.18x | 1.16x | 1.03x | 1.07x | 1.17x | 1.20x | 1.00x | 0.79x | 0.95x | 0.74x |
| unpack_sequence_list | 0.89x | 0.90x | 0.99x | 0.99x | 0.93x | 0.99x | 1.00x | 0.97x | 1.00x | 0.99x |
| urlsafe_base64_small | 0.88x | 0.89x | 0.89x | 0.89x | 0.87x | 0.88x | 1.00x | 0.88x | 0.98x | 0.90x |
| xdsl_constant_fold | 1.11x | 1.12x | 1.01x | 1.03x | 1.11x | 1.11x | 1.00x | 0.85x | 0.98x | 0.84x |
| xml_etree_parse | 1.18x | 1.04x | 1.09x | 1.00x | 1.18x | 1.09x | 1.00x | 1.02x | 1.10x | 1.02x |
| yaml | 1.05x | 1.07x | 1.00x | 1.00x | 1.06x | 1.05x | 1.00x | 0.85x | 0.96x | 0.85x |

Geomean vs baseline (>1 is faster):

- default: 1.116x
- nomimalloc: 1.024x
- nolto: 1.023x
- nolto-nomimalloc: 0.964x
- seplto: 1.104x
- seplto-nomimalloc: 1.030x
- reference-nolto: 0.892x
- reference-mimalloc: 0.988x
- reference-nolto-mimalloc: 0.874x
