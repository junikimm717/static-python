# pyperformance comparison

## Environment

- protocol: 2
- python_version: 3.14.7
- git_revision: 2e44df0243cea7b4c60235445ccc7c454210b9a5
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
- rows: 79
- skipped: 8 (see skipped.json)

## Interpreters

| label | sha256 | linkage | lto | allocator | pgo | size |
|---|---|---|---|---|---|---:|
| default | `3a8f79b9fcc6` | static | whole-graph | mimalloc | yes | 19.0 MiB |
| nomimalloc | `085bd628d865` | static | whole-graph | musl | yes | 18.9 MiB |
| nolto | `920ded538546` | static | none | mimalloc | yes | 18.0 MiB |
| nolto-nomimalloc | `38975d3591e7` | static | none | musl | yes | 17.9 MiB |
| seplto | `b8c44461a838` | static | per-dep | mimalloc | yes | 18.2 MiB |
| seplto-nomimalloc | `83da84e1f509` | static | per-dep | musl | yes | 18.1 MiB |
| reference | `d766a06abf2a` | dynamic | whole-graph | glibc | yes | 17.9 KiB |
| reference-nolto | `855882bc279d` | dynamic | none | glibc | yes | 17.4 KiB |
| reference-mimalloc | `4a8f9b0fc4ce` | dynamic | whole-graph | mimalloc | yes | 230.6 KiB |
| reference-nolto-mimalloc | `5666f42b44c0` | dynamic | none | mimalloc | yes | 230.1 KiB |

| benchmark | default | nomimalloc | nolto | nolto-nomimalloc | seplto | seplto-nomimalloc | reference | reference-nolto | reference-mimalloc | reference-nolto-mimalloc |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 2to3 | 1.08x | 1.04x | 1.02x | 0.96x | 1.09x | 1.03x | 1.00x | 0.86x | 0.99x | 0.86x |
| ascii85_large | 1.13x | 1.01x | 1.11x | 0.97x | 1.17x | 1.02x | 1.00x | 0.90x | 1.01x | 0.89x |
| ascii85_small | 1.14x | 1.09x | 1.10x | 1.01x | 1.16x | 1.09x | 1.00x | 0.87x | 1.01x | 0.87x |
| async_generators | 1.35x | 1.37x | 1.17x | 1.19x | 1.32x | 1.38x | 1.00x | 0.81x | 0.99x | 0.80x |
| async_tree_none | 1.11x | 1.13x | 1.04x | 1.05x | 1.12x | 1.14x | 1.00x | 0.89x | 0.99x | 0.88x |
| asyncio_tcp | 0.78x | 0.17x | 0.77x | 0.17x | 0.78x | 0.17x | 1.00x | 0.97x | 0.96x | 0.95x |
| asyncio_websockets | 1.02x | 0.82x | 1.02x | 0.82x | 1.03x | 0.83x | 1.00x | 1.00x | 1.03x | 1.03x |
| base16_large | 1.10x | 0.63x | 0.95x | 0.58x | 1.10x | 0.63x | 1.00x | 0.92x | 0.88x | 1.00x |
| base16_small | 1.18x | 1.12x | 1.11x | 1.00x | 1.21x | 1.10x | 1.00x | 0.97x | 1.03x | 1.02x |
| base32_large | 1.30x | 1.32x | 1.14x | 1.08x | 1.32x | 1.30x | 1.00x | 0.84x | 1.08x | 0.87x |
| base32_small | 1.28x | 1.34x | 1.12x | 1.10x | 1.29x | 1.34x | 1.00x | 0.84x | 1.07x | 0.85x |
| base64_large | 0.73x | 0.52x | 0.67x | 0.50x | 0.64x | 0.51x | 1.00x | 1.09x | 1.00x | 1.09x |
| base64_small | 0.89x | 0.83x | 0.86x | 0.83x | 0.86x | 0.84x | 1.00x | 0.91x | 0.98x | 0.90x |
| base85_large | 1.25x | 1.03x | 1.15x | 0.98x | 1.23x | 1.04x | 1.00x | 0.83x | 1.00x | 0.83x |
| base85_small | 1.27x | 1.13x | 1.14x | 1.05x | 1.25x | 1.15x | 1.00x | 0.81x | 1.01x | 0.82x |
| bench_mp_pool | 1.08x | 0.95x | 1.09x | 0.89x | 1.08x | 0.99x | 1.00x | 0.97x | 1.00x | 0.92x |
| bench_thread_pool | 1.01x | 1.02x | 0.99x | 0.97x | 1.00x | 1.02x | 1.00x | 0.93x | 0.93x | 0.86x |
| bpe_tokeniser | 1.22x | 1.15x | 1.08x | 1.02x | 1.23x | 1.14x | 1.00x | 0.79x | 0.98x | 0.78x |
| chameleon | 1.10x | 1.00x | 0.99x | 0.88x | 1.11x | 0.99x | 1.00x | 0.85x | 0.98x | 0.85x |
| chaos | 1.30x | 1.25x | 1.11x | 1.08x | 1.20x | 1.19x | 1.00x | 0.87x | 1.03x | 0.88x |
| comprehensions | 1.09x | 1.08x | 1.01x | 0.94x | 1.12x | 1.12x | 1.00x | 0.87x | 0.97x | 0.86x |
| coroutines | 1.26x | 1.24x | 1.02x | 1.06x | 1.31x | 1.26x | 1.00x | 0.85x | 1.00x | 0.88x |
| coverage | 2.37x | 1.12x | 1.10x | 1.01x | 1.21x | 1.15x | 1.00x | 0.85x | 0.98x | 0.82x |
| create_gc_cycles | 1.02x | 1.01x | 0.92x | 0.91x | 1.02x | 1.02x | 1.00x | 0.90x | 0.99x | 0.88x |
| crypto_pyaes | 1.23x | 1.17x | 1.14x | 1.07x | 1.22x | 1.19x | 1.00x | 0.92x | 0.98x | 0.90x |
| decimal_factorial | 1.24x | 1.18x | 1.15x | 1.14x | 1.23x | 1.22x | 1.00x | 0.93x | 1.04x | 0.94x |
| decimal_pi | 1.44x | 1.34x | 1.28x | 1.24x | 1.44x | 1.43x | 1.00x | 0.96x | 1.04x | 0.97x |
| deepcopy | 1.12x | 1.18x | 1.02x | 1.01x | 1.15x | 1.15x | 1.00x | 0.81x | 0.97x | 0.81x |
| deepcopy_memo | 1.10x | 1.14x | 1.09x | 1.01x | 1.15x | 1.12x | 1.00x | 0.94x | 0.96x | 0.92x |
| deepcopy_reduce | 1.13x | 1.20x | 1.01x | 0.99x | 1.13x | 1.16x | 1.00x | 0.79x | 0.98x | 0.79x |
| deltablue | 1.06x | 1.03x | 0.96x | 0.98x | 1.06x | 1.09x | 1.00x | 0.85x | 1.01x | 0.85x |
| docutils | 1.01x | 0.99x | 0.94x | 0.91x | 1.02x | 0.98x | 1.00x | 0.88x | 1.00x | 0.88x |
| fannkuch | 1.20x | 1.27x | 1.09x | 1.06x | 1.26x | 1.26x | 1.00x | 0.83x | 1.03x | 0.83x |
| float | 1.28x | 1.25x | 1.16x | 1.18x | 1.32x | 1.29x | 1.00x | 0.89x | 1.06x | 0.89x |
| gc_traversal | 1.00x | 0.98x | 0.91x | 0.88x | 0.91x | 0.95x | 1.00x | 0.98x | 0.94x | 0.93x |
| generators | 1.15x | 1.12x | 1.08x | 1.09x | 1.11x | 1.11x | 1.00x | 0.92x | 1.02x | 0.94x |
| genshi_text | 1.07x | 1.10x | 1.04x | 1.01x | 1.12x | 1.10x | 1.00x | 0.85x | 0.98x | 0.85x |
| go | 1.12x | 1.10x | 1.07x | 1.05x | 1.11x | 1.12x | 1.00x | 0.96x | 1.02x | 0.95x |
| hexiom | 1.06x | 1.09x | 1.01x | 1.01x | 1.06x | 1.10x | 1.00x | 0.87x | 0.99x | 0.87x |
| html5lib | 1.03x | 1.06x | 0.99x | 0.96x | 1.06x | 1.06x | 1.00x | 0.89x | 1.00x | 0.90x |
| json_dumps | 1.19x | 1.18x | 1.11x | 1.08x | 1.19x | 1.17x | 1.00x | 0.89x | 1.00x | 0.89x |
| json_loads | 1.17x | 0.70x | 1.08x | 1.06x | 1.22x | 1.16x | 1.00x | 0.89x | 0.97x | 0.93x |
| logging_format | 1.07x | 1.11x | 0.97x | 0.99x | 1.09x | 1.05x | 1.00x | 0.85x | 0.99x | 0.85x |
| mako | 1.25x | 1.03x | 1.11x | 0.86x | 1.28x | 1.05x | 1.00x | 0.88x | 1.16x | 0.93x |
| many_optionals | 1.12x | 1.11x | 1.04x | 1.02x | 1.12x | 1.11x | 1.00x | 0.87x | 1.01x | 0.88x |
| mdp | 1.16x | 1.19x | 1.04x | 1.03x | 1.18x | 1.17x | 1.00x | 0.82x | 0.99x | 0.82x |
| meteor_contest | 1.10x | 1.11x | 1.06x | 1.06x | 1.09x | 1.11x | 1.00x | 0.93x | 0.99x | 0.94x |
| nbody | 1.43x | 1.47x | 1.22x | 1.23x | 1.46x | 1.50x | 1.00x | 0.91x | 1.03x | 0.93x |
| nqueens | 1.26x | 1.32x | 1.13x | 1.13x | 1.27x | 1.30x | 1.00x | 0.83x | 1.03x | 0.83x |
| pathlib | 1.08x | 1.08x | 1.00x | 1.00x | 1.05x | 1.08x | 1.00x | 0.83x | 0.98x | 0.85x |
| pickle | 1.23x | 1.17x | 1.08x | 1.03x | 1.22x | 1.16x | 1.00x | 0.95x | 1.03x | 0.97x |
| pidigits | 1.09x | 0.60x | 1.08x | 0.60x | 1.09x | 0.61x | 1.00x | 1.03x | 1.03x | 1.01x |
| pprint_pformat | 1.12x | 1.13x | 0.99x | 0.96x | 1.10x | 1.14x | 1.00x | 0.82x | 0.99x | 0.80x |
| pprint_safe_repr | 1.12x | 1.14x | 0.99x | 0.96x | 1.11x | 1.14x | 1.00x | 0.81x | 0.99x | 0.80x |
| pyflate | 1.17x | 1.09x | 1.09x | 0.99x | 1.17x | 1.07x | 1.00x | 0.95x | 1.08x | 0.99x |
| python_startup | 1.09x | 0.95x | 1.05x | 0.90x | 1.09x | 0.94x | 1.00x | 0.91x | 0.95x | 0.87x |
| quadtree_nbody | 1.36x | 1.38x | 1.21x | 1.18x | 1.45x | 1.42x | 1.00x | 0.91x | 1.08x | 0.90x |
| raytrace | 1.18x | 1.20x | 1.06x | 1.08x | 1.21x | 1.21x | 1.00x | 0.85x | 1.02x | 0.86x |
| regex_compile | 1.08x | 1.09x | 1.00x | 0.96x | 1.10x | 1.11x | 1.00x | 0.84x | 0.99x | 0.85x |
| regex_dna | 0.91x | 0.83x | 0.93x | 0.82x | 0.97x | 0.84x | 1.00x | 0.95x | 0.96x | 0.90x |
| regex_effbot | 0.80x | 0.87x | 0.83x | 0.81x | 0.90x | 0.84x | 1.00x | 0.90x | 0.84x | 0.84x |
| regex_v8 | 1.00x | 0.95x | 0.97x | 0.91x | 1.00x | 0.95x | 1.00x | 0.89x | 0.96x | 0.94x |
| richards | 1.03x | 1.05x | 0.97x | 1.00x | 1.05x | 1.07x | 1.00x | 0.88x | 0.99x | 0.88x |
| richards_super | 1.08x | 1.07x | 1.00x | 1.03x | 1.09x | 1.11x | 1.00x | 0.91x | 1.02x | 0.91x |
| scimark_fft | 1.40x | 1.43x | 1.14x | 1.15x | 1.45x | 1.44x | 1.00x | 0.83x | 1.03x | 0.83x |
| shortest_path | 1.01x | 0.95x | 1.00x | 0.94x | 1.00x | 0.95x | 1.00x | 0.98x | 1.02x | 1.00x |
| spectral_norm | 1.32x | 1.26x | 1.12x | 1.12x | 1.29x | 1.31x | 1.00x | 0.82x | 1.10x | 0.80x |
| sphinx | 1.04x | 1.03x | 0.97x | 0.93x | 1.05x | 1.03x | 1.00x | 0.88x | 1.00x | 0.87x |
| sqlglot_v2_parse | 1.09x | 1.11x | 1.04x | 1.01x | 1.08x | 1.12x | 1.00x | 0.88x | 0.99x | 0.88x |
| sqlite_synth | 1.16x | 1.19x | 1.01x | 1.00x | 1.13x | 1.18x | 1.00x | 0.81x | 0.96x | 0.81x |
| telco | 1.36x | 1.28x | 1.13x | 1.05x | 1.37x | 1.34x | 1.00x | 0.84x | 1.00x | 0.85x |
| tomli_loads | 1.07x | 1.07x | 0.99x | 0.95x | 1.04x | 1.10x | 1.00x | 0.82x | 0.94x | 0.81x |
| tornado_http | 1.07x | 0.77x | 1.04x | 0.74x | 1.09x | 0.77x | 1.00x | 0.88x | 1.01x | 0.92x |
| typing_runtime_protocols | 1.23x | 1.28x | 1.05x | 1.07x | 1.22x | 1.26x | 1.00x | 0.77x | 0.97x | 0.76x |
| unpack_sequence_list | 0.99x | 0.97x | 0.99x | 0.89x | 0.81x | 0.99x | 1.00x | 1.07x | 1.08x | 0.99x |
| urlsafe_base64_small | 0.95x | 0.91x | 0.91x | 0.89x | 0.91x | 0.92x | 1.00x | 0.90x | 1.00x | 0.91x |
| xdsl_constant_fold | 1.11x | 1.14x | 1.01x | 1.01x | 1.12x | 1.13x | 1.00x | 0.84x | 0.99x | 0.85x |
| xml_etree_parse | 1.09x | 1.00x | 1.03x | 0.88x | 1.09x | 1.00x | 1.00x | 0.89x | 0.98x | 0.95x |
| yaml | 1.09x | 1.11x | 1.02x | 1.01x | 1.11x | 1.11x | 1.00x | 0.86x | 1.00x | 0.86x |

Geomean vs baseline (>1 is faster):

- default: 1.130x
- nomimalloc: 1.045x
- nolto: 1.035x
- nolto-nomimalloc: 0.953x
- seplto: 1.123x
- seplto-nomimalloc: 1.056x
- reference-nolto: 0.884x
- reference-mimalloc: 1.000x
- reference-nolto-mimalloc: 0.885x
