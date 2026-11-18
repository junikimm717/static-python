# pyperformance comparison

## Environment

- protocol: 2
- python_version: 3.15.2
- git_revision: 4b72fd9d00ce23131fd4e8bb93502250275feae6
- kit_version: 1
- triple: x86_64-linux-musl
- suite: pyperformance 1.14.0, pyperf 2.10.0
- kernel: Linux 6.12.94+deb13-amd64
- cpu: AMD Ryzen 5 3600 6-Core Processor
- memory: 62.7 GiB (61.6 GiB available)
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
- rows: 78
- skipped: 9 (see skipped.json)

## Interpreters

| label | sha256 | linkage | lto | allocator | pgo | size |
|---|---|---|---|---|---|---:|
| default | `417e5c9f318f` | static | whole-graph | mimalloc | yes | 19.4 MiB |
| nomimalloc | `4243809390ec` | static | whole-graph | musl | yes | 19.3 MiB |
| nolto | `ed7065b3fd58` | static | none | mimalloc | yes | 18.4 MiB |
| nolto-nomimalloc | `0a90314abff9` | static | none | musl | yes | 18.3 MiB |
| seplto | `7ccdac49d7aa` | static | per-dep | mimalloc | yes | 18.7 MiB |
| seplto-nomimalloc | `7ed95ecdb4eb` | static | per-dep | musl | yes | 18.5 MiB |
| reference | `f0aa05447738` | dynamic | whole-graph | glibc | yes | 18.0 KiB |
| reference-nolto | `790476e12710` | dynamic | none | glibc | yes | 17.5 KiB |
| reference-mimalloc | `a288f50d0a79` | dynamic | whole-graph | mimalloc | yes | 230.7 KiB |
| reference-nolto-mimalloc | `bd569bf0f121` | dynamic | none | mimalloc | yes | 230.2 KiB |

| benchmark | default | nomimalloc | nolto | nolto-nomimalloc | seplto | seplto-nomimalloc | reference | reference-nolto | reference-mimalloc | reference-nolto-mimalloc |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 2to3 | 1.10x | 1.02x | 1.02x | 0.96x | 1.09x | 1.02x | 1.00x | 0.87x | 1.01x | 0.87x |
| ascii85_large | 0.37x | 0.33x | 0.39x | 0.35x | 0.37x | 0.33x | 1.00x | 1.05x | 1.00x | 1.05x |
| ascii85_small | 0.71x | 0.70x | 0.70x | 0.70x | 0.70x | 0.70x | 1.00x | 1.01x | 1.02x | 1.03x |
| async_generators | 1.31x | 1.29x | 1.18x | 1.21x | 1.28x | 1.30x | 1.00x | 0.82x | 1.01x | 0.82x |
| async_tree_none | 1.12x | 1.11x | 1.03x | 1.04x | 1.09x | 1.08x | 1.00x | 0.87x | 1.01x | 0.87x |
| asyncio_tcp | 0.92x | 0.23x | 0.91x | 0.23x | 0.91x | 0.24x | 1.00x | 0.96x | 1.03x | 1.00x |
| asyncio_websockets | 1.09x | 0.97x | 1.09x | 0.97x | 1.10x | 0.98x | 1.00x | 1.00x | 1.10x | 1.10x |
| base16_large | 1.18x | 0.67x | 1.09x | 0.67x | 1.17x | 0.68x | 1.00x | 1.06x | 1.40x | 1.34x |
| base16_small | 1.01x | 0.92x | 0.92x | 0.89x | 1.03x | 0.88x | 1.00x | 0.93x | 1.05x | 0.95x |
| base32_large | 1.10x | 0.53x | 1.10x | 0.53x | 1.10x | 0.53x | 1.00x | 1.00x | 1.00x | 1.00x |
| base32_small | 1.09x | 1.06x | 1.02x | 1.01x | 1.10x | 1.07x | 1.00x | 0.96x | 1.03x | 0.94x |
| base64_large | 1.03x | 0.48x | 1.03x | 0.48x | 1.03x | 0.49x | 1.00x | 1.04x | 1.00x | 1.04x |
| base64_small | 1.06x | 1.04x | 1.00x | 0.99x | 1.08x | 1.05x | 1.00x | 0.94x | 1.01x | 0.93x |
| base85_large | 0.30x | 0.26x | 0.30x | 0.26x | 0.30x | 0.25x | 1.00x | 0.89x | 0.98x | 0.90x |
| base85_small | 0.63x | 0.62x | 0.61x | 0.61x | 0.63x | 0.60x | 1.00x | 0.87x | 1.02x | 0.89x |
| bench_mp_pool | 1.08x | 1.04x | 1.07x | 1.04x | 1.10x | 1.06x | 1.00x | 1.01x | 0.95x | 0.92x |
| bench_thread_pool | 1.01x | 1.03x | 0.99x | 1.02x | 1.00x | 1.04x | 1.00x | 0.93x | 0.96x | 0.90x |
| bpe_tokeniser | 1.17x | 1.13x | 1.08x | 1.03x | 1.20x | 1.14x | 1.00x | 0.78x | 1.00x | 0.79x |
| chameleon | 1.14x | 1.04x | 1.00x | 0.92x | 1.12x | 1.00x | 1.00x | 0.83x | 1.04x | 0.82x |
| chaos | 1.21x | 1.20x | 1.10x | 1.07x | 1.21x | 1.12x | 1.00x | 0.81x | 1.03x | 0.84x |
| comprehensions | 1.14x | 1.12x | 1.02x | 1.03x | 1.15x | 1.11x | 1.00x | 0.88x | 1.04x | 0.88x |
| coroutines | 1.12x | 1.20x | 1.17x | 1.02x | 1.24x | 1.08x | 1.00x | 0.89x | 0.97x | 0.85x |
| coverage | 2.26x | 1.09x | 1.05x | 0.95x | 1.11x | 1.06x | 1.00x | 0.81x | 1.02x | 0.80x |
| create_gc_cycles | 1.04x | 1.03x | 0.95x | 0.94x | 1.04x | 1.03x | 1.00x | 0.91x | 1.01x | 0.92x |
| crypto_pyaes | 1.24x | 1.13x | 1.12x | 1.07x | 1.20x | 1.12x | 1.00x | 0.90x | 0.98x | 0.91x |
| decimal_factorial | 1.19x | 1.17x | 1.12x | 1.11x | 1.14x | 1.13x | 1.00x | 0.91x | 1.00x | 0.91x |
| decimal_pi | 1.45x | 1.40x | 1.30x | 1.23x | 1.45x | 1.42x | 1.00x | 0.94x | 1.01x | 0.93x |
| deepcopy | 1.18x | 1.19x | 1.03x | 1.03x | 1.16x | 1.12x | 1.00x | 0.80x | 1.01x | 0.79x |
| deepcopy_memo | 1.10x | 1.14x | 1.11x | 1.09x | 1.17x | 1.11x | 1.00x | 0.94x | 1.01x | 0.93x |
| deepcopy_reduce | 1.18x | 1.20x | 1.02x | 1.02x | 1.15x | 1.12x | 1.00x | 0.78x | 1.02x | 0.78x |
| deltablue | 1.05x | 0.99x | 0.88x | 0.89x | 1.02x | 1.03x | 1.00x | 0.81x | 1.02x | 0.81x |
| docutils | 1.02x | 0.98x | 0.95x | 0.92x | 1.01x | 0.98x | 1.00x | 0.87x | 1.01x | 0.88x |
| fannkuch | 1.22x | 1.13x | 1.13x | 1.12x | 1.13x | 1.18x | 1.00x | 0.83x | 1.01x | 0.84x |
| float | 1.24x | 1.20x | 1.13x | 1.12x | 1.22x | 1.21x | 1.00x | 0.84x | 1.01x | 0.87x |
| gc_traversal | 1.00x | 0.99x | 0.92x | 0.94x | 0.95x | 0.99x | 1.00x | 0.95x | 1.00x | 0.79x |
| generators | 1.01x | 1.03x | 1.01x | 1.00x | 1.00x | 1.00x | 1.00x | 0.92x | 1.02x | 0.97x |
| go | 1.01x | 1.01x | 1.00x | 0.99x | 1.01x | 1.01x | 1.00x | 0.93x | 0.98x | 0.93x |
| hexiom | 1.10x | 1.07x | 1.01x | 1.02x | 1.03x | 1.08x | 1.00x | 0.86x | 0.99x | 0.86x |
| html5lib | 1.08x | 1.07x | 1.02x | 1.03x | 1.08x | 1.07x | 1.00x | 0.93x | 1.02x | 0.93x |
| json_dumps | 1.23x | 1.18x | 1.17x | 1.12x | 1.22x | 1.15x | 1.00x | 0.91x | 1.02x | 0.92x |
| json_loads | 1.10x | 0.67x | 1.07x | 1.06x | 1.17x | 0.81x | 1.00x | 0.91x | 1.03x | 0.92x |
| logging_format | 1.09x | 1.09x | 0.98x | 0.99x | 1.08x | 1.03x | 1.00x | 0.86x | 1.01x | 0.86x |
| mako | 1.22x | 1.02x | 1.07x | 0.94x | 1.29x | 1.02x | 1.00x | 0.85x | 1.11x | 0.94x |
| many_optionals | 1.12x | 1.11x | 1.05x | 1.03x | 1.11x | 1.10x | 1.00x | 0.88x | 1.01x | 0.88x |
| mdp | 1.17x | 1.16x | 1.02x | 1.02x | 1.15x | 1.11x | 1.00x | 0.83x | 1.00x | 0.84x |
| meteor_contest | 1.26x | 1.26x | 1.17x | 1.19x | 1.25x | 1.22x | 1.00x | 0.94x | 1.02x | 0.94x |
| nbody | 1.26x | 1.27x | 1.21x | 1.18x | 1.24x | 1.25x | 1.00x | 0.84x | 1.04x | 0.86x |
| nqueens | 1.19x | 1.16x | 1.05x | 1.05x | 1.16x | 1.17x | 1.00x | 0.82x | 1.01x | 0.83x |
| pathlib | 1.10x | 1.07x | 1.05x | 1.00x | 1.09x | 1.06x | 1.00x | 0.87x | 1.05x | 0.88x |
| pickle | 1.19x | 1.11x | 1.05x | 0.98x | 1.18x | 1.12x | 1.00x | 0.92x | 1.03x | 0.97x |
| pidigits | 1.03x | 0.63x | 1.02x | 0.63x | 1.03x | 0.63x | 1.00x | 0.99x | 0.98x | 1.00x |
| pprint_pformat | 1.16x | 1.17x | 1.00x | 1.01x | 1.14x | 1.10x | 1.00x | 0.80x | 1.01x | 0.80x |
| pprint_safe_repr | 1.16x | 1.16x | 0.99x | 1.00x | 1.14x | 1.10x | 1.00x | 0.80x | 1.01x | 0.81x |
| pyflate | 1.09x | 1.01x | 1.00x | 0.94x | 1.08x | 1.00x | 1.00x | 0.92x | 1.01x | 0.92x |
| python_startup | 1.10x | 0.97x | 1.06x | 0.94x | 1.09x | 0.97x | 1.00x | 0.90x | 0.96x | 0.88x |
| quadtree_nbody | 1.26x | 1.25x | 1.11x | 1.16x | 1.26x | 1.27x | 1.00x | 0.85x | 1.01x | 0.87x |
| raytrace | 1.14x | 1.14x | 1.04x | 1.04x | 1.14x | 1.15x | 1.00x | 0.83x | 0.99x | 0.83x |
| regex_compile | 1.13x | 1.12x | 1.02x | 1.02x | 1.10x | 1.09x | 1.00x | 0.86x | 1.00x | 0.86x |
| regex_dna | 0.96x | 0.84x | 0.93x | 0.85x | 1.00x | 0.86x | 1.00x | 0.97x | 1.00x | 0.99x |
| regex_effbot | 0.92x | 0.90x | 0.86x | 0.96x | 0.91x | 0.91x | 1.00x | 0.99x | 1.04x | 0.92x |
| regex_v8 | 1.03x | 0.96x | 1.01x | 0.94x | 1.00x | 0.94x | 1.00x | 0.96x | 1.03x | 0.94x |
| richards | 1.03x | 1.03x | 1.00x | 0.96x | 1.05x | 1.05x | 1.00x | 0.92x | 1.02x | 0.88x |
| richards_super | 1.04x | 1.03x | 0.98x | 0.97x | 1.06x | 1.05x | 1.00x | 0.90x | 1.01x | 0.88x |
| scimark_fft | 1.47x | 1.43x | 1.18x | 1.18x | 1.46x | 1.42x | 1.00x | 0.82x | 1.01x | 0.81x |
| shortest_path | 1.02x | 0.97x | 1.01x | 0.96x | 1.02x | 0.97x | 1.00x | 0.99x | 1.02x | 1.00x |
| spectral_norm | 1.28x | 1.11x | 1.10x | 1.10x | 1.22x | 1.16x | 1.00x | 0.83x | 1.08x | 0.83x |
| sphinx | 1.04x | 1.01x | 0.97x | 0.94x | 1.04x | 1.01x | 1.00x | 0.87x | 1.02x | 0.87x |
| sqlglot_v2_parse | 1.08x | 1.03x | 1.02x | 1.03x | 1.05x | 1.06x | 1.00x | 0.87x | 1.01x | 0.88x |
| sqlite_synth | 1.15x | 1.20x | 1.04x | 1.06x | 1.13x | 1.10x | 1.00x | 0.85x | 1.01x | 0.85x |
| telco | 1.47x | 1.30x | 1.15x | 1.03x | 1.42x | 1.28x | 1.00x | 0.80x | 1.01x | 0.82x |
| tomli_loads | 1.19x | 1.16x | 1.08x | 1.08x | 1.16x | 1.17x | 1.00x | 0.87x | 1.03x | 0.87x |
| tornado_http | 1.09x | 0.78x | 1.06x | 0.76x | 1.09x | 0.77x | 1.00x | 0.90x | 1.04x | 0.93x |
| typing_runtime_protocols | 1.24x | 1.16x | 1.08x | 1.09x | 1.19x | 1.17x | 1.00x | 0.78x | 1.00x | 0.77x |
| unpack_sequence_list | 0.86x | 0.85x | 0.82x | 0.86x | 0.86x | 0.86x | 1.00x | 0.93x | 1.07x | 1.10x |
| urlsafe_base64_small | 0.97x | 0.95x | 0.91x | 0.90x | 0.98x | 0.96x | 1.00x | 0.87x | 1.00x | 0.89x |
| xdsl_constant_fold | 1.11x | 1.09x | 1.02x | 1.03x | 1.09x | 1.10x | 1.00x | 0.84x | 1.00x | 0.84x |
| xml_etree_parse | 1.11x | 1.02x | 1.02x | 0.92x | 1.13x | 1.01x | 1.00x | 0.95x | 1.00x | 0.96x |
| yaml | 1.04x | 1.05x | 0.99x | 1.00x | 1.06x | 1.03x | 1.00x | 0.85x | 0.98x | 0.85x |

Geomean vs baseline (>1 is faster):

- default: 1.080x
- nomimalloc: 0.975x
- nolto: 0.996x
- nolto-nomimalloc: 0.923x
- seplto: 1.065x
- seplto-nomimalloc: 0.970x
- reference-nolto: 0.891x
- reference-mimalloc: 1.018x
- reference-nolto-mimalloc: 0.897x
