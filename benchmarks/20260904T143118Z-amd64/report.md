# pyperformance comparison

## Environment

- protocol: 2
- git_revision: 446a7d30c960582def7b42be0b5f6f558f891b05-dirty
- suite: pyperformance 1.14.0, pyperf 2.10.0
- kernel: Linux 6.12.94+deb13-amd64
- cpu: AMD Ryzen 5 3600 6-Core Processor
- memory: 62.7 GiB (61.9 GiB available)
- logical cores: 2
- caches: L1d 32K / L1i 32K / L2 512K / L3 16384K
- topology: 12 logical cpus, hybrid (6×4.21GHz, 6×capacity=0)
- affinity: pinned to cpu3
- fingerprint: ab00d0fb5ac252ad2c28c76497deb352fcd0a9daa8ddaa4c7938ae934506873e
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
| default | `04bf4cfcd9f0` | static | whole-graph | mimalloc | yes | 17.4 MiB |
| nomimalloc | `6785eabe8fa1` | static | whole-graph | musl | yes | 17.3 MiB |
| nolto | `e3f705603605` | static | none | mimalloc | yes | 16.5 MiB |
| nolto-nomimalloc | `8a2e4ceb06e6` | static | none | musl | yes | 16.4 MiB |
| seplto | `482384ef7dfe` | static | per-dep | mimalloc | yes | 16.6 MiB |
| seplto-nomimalloc | `27183f51f370` | static | per-dep | musl | yes | 16.5 MiB |
| reference | `68bc27bcd2d3` | dynamic | whole-graph | glibc | yes | 17.3 KiB |
| reference-nolto | `3acc33e95359` | dynamic | none | glibc | yes | 17.3 KiB |
| reference-mimalloc | `c262e5f9f8bd` | dynamic | whole-graph | mimalloc | yes | 230.2 KiB |
| reference-nolto-mimalloc | `02a2579907b8` | dynamic | none | mimalloc | yes | 230.2 KiB |

| benchmark | default | nomimalloc | nolto | nolto-nomimalloc | seplto | seplto-nomimalloc | reference | reference-nolto | reference-mimalloc | reference-nolto-mimalloc |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 2to3 | 1.51x | 1.45x | 1.40x | 1.37x | 1.51x | 1.46x | 1.00x | 1.00x | 1.04x | 1.00x |
| ascii85_large | 1.43x | 1.28x | 1.33x | 1.21x | 1.45x | 1.27x | 1.00x | 1.04x | 1.11x | 1.06x |
| ascii85_small | 1.52x | 1.41x | 1.41x | 1.32x | 1.55x | 1.39x | 1.00x | 1.03x | 1.09x | 1.02x |
| async_generators | 1.85x | 1.81x | 1.60x | 1.61x | 1.81x | 1.81x | 1.00x | 0.99x | 1.08x | 0.98x |
| async_tree_none | 1.49x | 1.46x | 1.36x | 1.38x | 1.49x | 1.48x | 1.00x | 1.00x | 1.06x | 1.00x |
| asyncio_tcp | 0.83x | 0.18x | 0.81x | 0.18x | 0.84x | 0.18x | 1.00x | 1.01x | 0.98x | 0.97x |
| asyncio_websockets | 0.99x | 0.80x | 0.99x | 0.80x | 1.00x | 0.80x | 1.00x | 0.97x | 1.00x | 1.00x |
| base16_large | 0.83x | 0.89x | 1.09x | 0.93x | 0.81x | 0.86x | 1.00x | 1.00x | 0.87x | 0.85x |
| base16_small | 1.10x | 1.26x | 1.32x | 1.28x | 1.11x | 1.26x | 1.00x | 1.00x | 0.95x | 0.92x |
| base32_large | 1.69x | 1.72x | 1.49x | 1.46x | 1.83x | 1.75x | 1.00x | 1.00x | 1.08x | 1.00x |
| base32_small | 1.79x | 1.77x | 1.52x | 1.52x | 1.87x | 1.79x | 1.00x | 1.01x | 1.10x | 1.02x |
| base64_large | 0.97x | 0.72x | 0.92x | 0.70x | 0.96x | 0.74x | 1.00x | 1.00x | 1.00x | 1.00x |
| base64_small | 1.20x | 1.20x | 1.22x | 1.18x | 1.24x | 1.22x | 1.00x | 1.01x | 1.05x | 1.00x |
| base85_large | 1.66x | 1.43x | 1.50x | 1.31x | 1.65x | 1.41x | 1.00x | 1.01x | 1.10x | 1.01x |
| base85_small | 1.75x | 1.59x | 1.56x | 1.44x | 1.76x | 1.56x | 1.00x | 0.99x | 1.09x | 1.00x |
| bench_mp_pool | 1.34x | 1.33x | 1.33x | 1.33x | 1.33x | 1.33x | 1.00x | 1.00x | 1.00x | 1.00x |
| bench_thread_pool | 1.25x | 1.22x | 1.19x | 1.15x | 1.26x | 1.22x | 1.00x | 1.00x | 1.01x | 0.99x |
| bpe_tokeniser | 1.90x | 1.75x | 1.66x | 1.59x | 1.83x | 1.76x | 1.00x | 1.00x | 1.06x | 1.02x |
| chameleon | 1.78x | 1.62x | 1.61x | 1.52x | 1.81x | 1.64x | 1.00x | 1.00x | 1.07x | 0.99x |
| chaos | 1.66x | 1.62x | 1.46x | 1.49x | 1.65x | 1.68x | 1.00x | 1.01x | 1.07x | 1.02x |
| comprehensions | 1.68x | 1.70x | 1.56x | 1.05x | 1.63x | 1.11x | 1.00x | 1.00x | 1.09x | 1.02x |
| coroutines | 1.29x | 1.26x | 1.20x | 1.25x | 1.36x | 1.25x | 1.00x | 1.02x | 1.03x | 0.99x |
| coverage | 3.27x | 1.56x | 1.53x | 1.42x | 1.68x | 1.63x | 1.00x | 1.00x | 1.06x | 1.02x |
| create_gc_cycles | 1.22x | 1.23x | 1.12x | 1.10x | 1.23x | 1.23x | 1.00x | 1.01x | 1.02x | 1.01x |
| crypto_pyaes | 1.56x | 1.50x | 1.40x | 1.38x | 1.58x | 1.53x | 1.00x | 1.00x | 1.08x | 1.01x |
| decimal_factorial | 1.19x | 1.14x | 1.10x | 1.13x | 1.17x | 1.18x | 1.00x | 1.00x | 1.02x | 1.01x |
| decimal_pi | 1.95x | 2.01x | 1.55x | 1.63x | 1.96x | 1.94x | 1.00x | 1.02x | 1.04x | 1.02x |
| deepcopy | 1.65x | 1.68x | 1.48x | 1.52x | 1.65x | 1.69x | 1.00x | 1.00x | 1.05x | 0.99x |
| deepcopy_memo | 1.28x | 1.29x | 1.23x | 1.24x | 1.42x | 1.37x | 1.00x | 1.00x | 1.02x | 0.97x |
| deepcopy_reduce | 1.82x | 1.83x | 1.58x | 1.64x | 1.83x | 1.84x | 1.00x | 1.00x | 1.07x | 0.98x |
| deltablue | 1.37x | 1.38x | 1.32x | 1.32x | 1.42x | 1.41x | 1.00x | 1.00x | 1.01x | 0.96x |
| docutils | 1.40x | 1.37x | 1.32x | 1.28x | 1.42x | 1.38x | 1.00x | 1.00x | 1.05x | 1.00x |
| fannkuch | 1.67x | 1.64x | 1.52x | 1.52x | 1.64x | 1.69x | 1.00x | 1.00x | 1.02x | 1.00x |
| float | 1.62x | 1.60x | 1.47x | 1.45x | 1.62x | 1.64x | 1.00x | 0.99x | 1.07x | 1.00x |
| gc_traversal | 1.28x | 1.20x | 1.09x | 1.09x | 1.28x | 1.17x | 1.00x | 0.98x | 1.11x | 1.08x |
| generators | 1.16x | 1.21x | 1.21x | 1.26x | 1.21x | 1.20x | 1.00x | 1.00x | 1.04x | 1.00x |
| genshi_text | 1.57x | 1.50x | 1.45x | 1.43x | 1.55x | 1.48x | 1.00x | 1.00x | 1.06x | 1.00x |
| go | 1.20x | 1.18x | 1.15x | 1.16x | 1.21x | 1.20x | 1.00x | 1.00x | 1.02x | 1.00x |
| hexiom | 1.38x | 1.38x | 1.31x | 1.35x | 1.36x | 1.37x | 1.00x | 1.00x | 1.04x | 0.98x |
| html5lib | 1.27x | 1.30x | 1.25x | 1.25x | 1.29x | 1.31x | 1.00x | 1.00x | 1.01x | 0.98x |
| json_dumps | 1.83x | 1.74x | 1.73x | 1.69x | 1.84x | 1.78x | 1.00x | 1.00x | 1.10x | 1.04x |
| json_loads | 1.42x | 1.38x | 1.53x | 1.08x | 1.51x | 1.54x | 1.00x | 0.99x | 1.02x | 0.99x |
| logging_format | 1.63x | 1.67x | 1.49x | 1.49x | 1.69x | 1.71x | 1.00x | 0.99x | 1.04x | 0.99x |
| mako | 1.70x | 1.37x | 1.43x | 1.28x | 1.76x | 1.38x | 1.00x | 1.00x | 1.13x | 1.11x |
| many_optionals | 1.52x | 1.49x | 1.42x | 1.40x | 1.54x | 1.51x | 1.00x | 1.00x | 1.04x | 1.01x |
| mdp | 1.66x | 1.68x | 1.64x | 1.78x | 1.66x | 1.71x | 1.00x | 1.00x | 0.93x | 0.91x |
| meteor_contest | 1.40x | 1.38x | 1.33x | 1.35x | 1.39x | 1.39x | 1.00x | 1.00x | 1.06x | 1.01x |
| nbody | 1.38x | 1.40x | 1.33x | 1.39x | 1.39x | 1.43x | 1.00x | 1.00x | 1.06x | 0.99x |
| nqueens | 2.01x | 1.99x | 1.76x | 1.82x | 1.99x | 2.04x | 1.00x | 1.00x | 1.09x | 1.02x |
| pathlib | 1.48x | 1.45x | 1.38x | 1.35x | 1.49x | 1.44x | 1.00x | 1.00x | 1.04x | 1.01x |
| pickle | 1.72x | 1.51x | 1.38x | 1.28x | 1.67x | 1.50x | 1.00x | 0.99x | 1.04x | 1.02x |
| pidigits | 1.07x | 0.62x | 1.06x | 0.62x | 1.07x | 0.62x | 1.00x | 1.00x | 1.04x | 1.04x |
| pprint_pformat | 1.77x | 1.74x | 1.56x | 1.61x | 1.73x | 1.80x | 1.00x | 1.00x | 1.06x | 0.99x |
| pprint_safe_repr | 1.77x | 1.75x | 1.58x | 1.61x | 1.74x | 1.78x | 1.00x | 1.00x | 1.06x | 0.99x |
| pyflate | 1.27x | 1.17x | 1.18x | 1.12x | 1.24x | 1.19x | 1.00x | 1.01x | 1.04x | 1.00x |
| python_startup | 1.35x | 1.18x | 1.29x | 1.13x | 1.35x | 1.17x | 1.00x | 1.00x | 0.98x | 0.96x |
| quadtree_nbody | 1.50x | 1.50x | 1.38x | 1.41x | 1.48x | 1.54x | 1.00x | 1.01x | 1.07x | 0.99x |
| raytrace | 1.51x | 1.54x | 1.44x | 1.46x | 1.56x | 1.57x | 1.00x | 1.00x | 1.06x | 1.01x |
| regex_compile | 1.54x | 1.49x | 1.42x | 1.40x | 1.51x | 1.53x | 1.00x | 1.00x | 1.06x | 1.00x |
| regex_dna | 0.92x | 0.87x | 0.99x | 0.90x | 0.96x | 0.84x | 1.00x | 1.00x | 0.95x | 0.95x |
| regex_effbot | 0.84x | 1.03x | 1.04x | 1.05x | 0.96x | 1.03x | 1.00x | 0.99x | 0.98x | 0.96x |
| regex_v8 | 1.22x | 1.21x | 1.10x | 1.16x | 1.34x | 1.14x | 1.00x | 1.00x | 1.06x | 1.02x |
| richards | 1.31x | 1.34x | 1.28x | 1.29x | 1.39x | 1.37x | 1.00x | 1.01x | 1.03x | 0.97x |
| richards_super | 1.33x | 1.33x | 1.30x | 1.28x | 1.38x | 1.39x | 1.00x | 0.99x | 1.02x | 0.97x |
| scimark_fft | 1.99x | 2.04x | 1.58x | 1.70x | 2.02x | 1.95x | 1.00x | 1.00x | 1.09x | 1.02x |
| shortest_path | 1.09x | 1.02x | 1.07x | 1.02x | 1.08x | 1.02x | 1.00x | 1.00x | 1.02x | 1.02x |
| spectral_norm | 1.55x | 1.54x | 1.40x | 1.36x | 1.53x | 1.59x | 1.00x | 1.01x | 1.05x | 1.01x |
| sphinx | 1.42x | 1.41x | 1.35x | 1.32x | 1.44x | 1.41x | 1.00x | 1.00x | 1.04x | 0.99x |
| sqlglot_v2_parse | 1.48x | 1.48x | 1.38x | 1.40x | 1.51x | 1.52x | 1.00x | 1.00x | 1.05x | 1.01x |
| sqlite_synth | 1.81x | 1.78x | 1.61x | 1.66x | 1.76x | 1.77x | 1.00x | 1.01x | 1.08x | 1.00x |
| telco | 2.15x | 2.00x | 1.68x | 1.66x | 2.07x | 1.99x | 1.00x | 1.02x | 1.07x | 1.02x |
| tomli_loads | 1.46x | 1.43x | 1.40x | 1.40x | 1.48x | 1.50x | 1.00x | 1.00x | 1.10x | 1.02x |
| tornado_http | 1.39x | 0.98x | 1.33x | 0.96x | 1.40x | 0.99x | 1.00x | 1.00x | 1.06x | 1.03x |
| typing_runtime_protocols | 2.05x | 2.09x | 1.75x | 1.83x | 2.08x | 2.10x | 1.00x | 0.99x | 1.07x | 1.00x |
| unpack_sequence_list | 0.81x | 0.86x | 0.89x | 0.89x | 0.93x | 0.92x | 1.00x | 1.00x | 1.00x | 1.00x |
| urlsafe_base64_small | 1.19x | 1.18x | 1.19x | 1.14x | 1.20x | 1.20x | 1.00x | 1.01x | 1.03x | 0.99x |
| xdsl_constant_fold | 1.67x | 1.68x | 1.53x | 1.55x | 1.68x | 1.70x | 1.00x | 1.00x | 1.05x | 1.00x |
| xml_etree_parse | 1.42x | 1.29x | 1.31x | 1.19x | 1.38x | 1.27x | 1.00x | 1.01x | 1.04x | 0.99x |
| yaml | 1.44x | 1.45x | 1.37x | 1.35x | 1.44x | 1.47x | 1.00x | 1.00x | 1.05x | 1.00x |

Geomean vs baseline (>1 is faster):

- default: 1.445x
- nomimalloc: 1.356x
- nolto: 1.343x
- nolto-nomimalloc: 1.269x
- seplto: 1.450x
- seplto-nomimalloc: 1.361x
- reference-nolto: 1.001x
- reference-mimalloc: 1.043x
- reference-nolto-mimalloc: 0.999x
