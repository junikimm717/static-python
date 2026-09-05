# pyperformance comparison

## Environment

- protocol: 2
- python_version: 3.13.13
- git_revision: 70987be221b480dd5d9c969edcb59a5cf8203546
- kit_version: 1
- triple: x86_64-linux-musl
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
| default | `80e2586cce4b` | static | whole-graph | mimalloc | yes | 17.4 MiB |
| nomimalloc | `6785eabe8fa1` | static | whole-graph | musl | yes | 17.3 MiB |
| nolto | `19705ac74869` | static | none | mimalloc | yes | 16.5 MiB |
| nolto-nomimalloc | `8a2e4ceb06e6` | static | none | musl | yes | 16.4 MiB |
| seplto | `b0d73a1e9835` | static | per-dep | mimalloc | yes | 16.6 MiB |
| seplto-nomimalloc | `27183f51f370` | static | per-dep | musl | yes | 16.5 MiB |
| reference | `b7be5741f69a` | dynamic | whole-graph | glibc | yes | 17.3 KiB |
| reference-nolto | `060bd2f7037d` | dynamic | none | glibc | yes | 17.3 KiB |
| reference-mimalloc | `5dff6c9fe828` | dynamic | whole-graph | mimalloc | yes | 230.0 KiB |
| reference-nolto-mimalloc | `fade9896a773` | dynamic | none | mimalloc | yes | 230.0 KiB |

| benchmark | default | nomimalloc | nolto | nolto-nomimalloc | seplto | seplto-nomimalloc | reference | reference-nolto | reference-mimalloc | reference-nolto-mimalloc |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 2to3 | 1.52x | 1.46x | 1.42x | 1.35x | 1.51x | 1.47x | 1.00x | 1.00x | 1.03x | 1.00x |
| ascii85_large | 1.37x | 1.22x | 1.32x | 1.16x | 1.38x | 1.23x | 1.00x | 1.01x | 1.04x | 1.01x |
| ascii85_small | 1.53x | 1.40x | 1.44x | 1.31x | 1.54x | 1.42x | 1.00x | 1.01x | 1.09x | 1.04x |
| async_generators | 1.83x | 1.83x | 1.64x | 1.61x | 1.82x | 1.85x | 1.00x | 1.00x | 1.07x | 1.00x |
| async_tree_none | 1.49x | 1.48x | 1.37x | 1.35x | 1.51x | 1.51x | 1.00x | 1.00x | 1.05x | 1.01x |
| asyncio_tcp | 0.82x | 0.18x | 0.81x | 0.18x | 0.82x | 0.18x | 1.00x | 1.00x | 0.95x | 0.96x |
| asyncio_websockets | 1.02x | 0.82x | 1.02x | 0.82x | 1.03x | 0.83x | 1.00x | 1.00x | 1.03x | 1.03x |
| base16_large | 1.07x | 0.89x | 1.09x | 0.93x | 1.04x | 0.87x | 1.00x | 1.00x | 1.03x | 1.01x |
| base16_small | 1.33x | 1.27x | 1.33x | 1.26x | 1.29x | 1.25x | 1.00x | 1.00x | 1.04x | 1.00x |
| base32_large | 1.84x | 1.72x | 1.60x | 1.44x | 1.83x | 1.75x | 1.00x | 1.00x | 1.07x | 1.01x |
| base32_small | 1.87x | 1.76x | 1.63x | 1.49x | 1.88x | 1.80x | 1.00x | 1.00x | 1.07x | 1.01x |
| base64_large | 0.95x | 0.74x | 0.92x | 0.70x | 0.97x | 0.72x | 1.00x | 1.00x | 1.00x | 1.00x |
| base64_small | 1.20x | 1.19x | 1.20x | 1.16x | 1.19x | 1.21x | 1.00x | 1.00x | 1.00x | 0.97x |
| base85_large | 1.65x | 1.42x | 1.54x | 1.30x | 1.67x | 1.39x | 1.00x | 1.01x | 1.06x | 1.01x |
| base85_small | 1.77x | 1.60x | 1.62x | 1.43x | 1.80x | 1.58x | 1.00x | 1.00x | 1.06x | 0.99x |
| bench_mp_pool | 1.33x | 1.33x | 1.33x | 1.33x | 1.33x | 1.33x | 1.00x | 1.00x | 1.00x | 1.00x |
| bench_thread_pool | 1.24x | 1.23x | 1.20x | 1.14x | 1.26x | 1.23x | 1.00x | 1.00x | 1.00x | 0.97x |
| bpe_tokeniser | 1.89x | 1.76x | 1.69x | 1.57x | 1.89x | 1.75x | 1.00x | 1.00x | 1.04x | 0.99x |
| chameleon | 1.80x | 1.63x | 1.65x | 1.47x | 1.79x | 1.64x | 1.00x | 1.00x | 1.06x | 0.99x |
| chaos | 1.66x | 1.61x | 1.50x | 1.47x | 1.66x | 1.67x | 1.00x | 1.00x | 1.06x | 0.99x |
| comprehensions | 1.72x | 1.69x | 1.59x | 1.53x | 1.73x | 1.68x | 1.00x | 1.00x | 1.05x | 1.00x |
| coroutines | 1.31x | 1.25x | 1.18x | 1.24x | 1.21x | 1.29x | 1.00x | 1.00x | 1.04x | 0.95x |
| coverage | 3.30x | 1.56x | 1.52x | 1.43x | 1.64x | 1.63x | 1.00x | 0.99x | 1.03x | 0.98x |
| create_gc_cycles | 1.23x | 1.22x | 1.09x | 1.12x | 1.22x | 1.22x | 1.00x | 1.00x | 1.02x | 1.02x |
| crypto_pyaes | 1.56x | 1.51x | 1.43x | 1.38x | 1.63x | 1.55x | 1.00x | 1.00x | 1.05x | 0.99x |
| decimal_factorial | 1.17x | 1.13x | 1.13x | 1.13x | 1.17x | 1.17x | 1.00x | 0.99x | 0.99x | 1.00x |
| decimal_pi | 1.95x | 1.99x | 1.54x | 1.59x | 1.96x | 1.92x | 1.00x | 0.99x | 1.04x | 1.01x |
| deepcopy | 1.65x | 1.69x | 1.50x | 1.48x | 1.62x | 1.71x | 1.00x | 1.00x | 1.05x | 0.99x |
| deepcopy_memo | 1.32x | 1.35x | 1.25x | 1.25x | 1.28x | 1.39x | 1.00x | 1.01x | 1.06x | 1.02x |
| deepcopy_reduce | 1.84x | 1.86x | 1.63x | 1.58x | 1.84x | 1.89x | 1.00x | 1.00x | 1.07x | 1.00x |
| deltablue | 1.42x | 1.39x | 1.32x | 1.30x | 1.41x | 1.42x | 1.00x | 1.00x | 1.01x | 0.98x |
| docutils | 1.41x | 1.38x | 1.34x | 1.27x | 1.41x | 1.38x | 1.00x | 1.00x | 1.04x | 1.00x |
| fannkuch | 1.65x | 1.65x | 1.50x | 1.51x | 1.69x | 1.70x | 1.00x | 0.99x | 1.02x | 1.00x |
| float | 1.67x | 1.59x | 1.49x | 1.48x | 1.69x | 1.64x | 1.00x | 1.00x | 1.05x | 1.00x |
| gc_traversal | 1.23x | 1.16x | 1.13x | 1.06x | 1.23x | 1.16x | 1.00x | 0.97x | 1.05x | 1.05x |
| generators | 1.20x | 1.20x | 1.22x | 1.24x | 1.18x | 1.20x | 1.00x | 1.00x | 1.03x | 0.99x |
| genshi_text | 1.53x | 1.52x | 1.47x | 1.39x | 1.56x | 1.50x | 1.00x | 1.00x | 1.03x | 0.99x |
| go | 1.19x | 1.18x | 1.14x | 1.14x | 1.20x | 1.20x | 1.00x | 1.00x | 1.01x | 0.99x |
| hexiom | 1.36x | 1.39x | 1.34x | 1.31x | 1.32x | 1.38x | 1.00x | 1.00x | 1.02x | 0.97x |
| html5lib | 1.31x | 1.30x | 1.28x | 1.24x | 1.32x | 1.32x | 1.00x | 1.00x | 1.03x | 1.00x |
| json_dumps | 1.88x | 1.73x | 1.72x | 1.64x | 1.79x | 1.79x | 1.00x | 1.00x | 1.07x | 1.02x |
| json_loads | 1.44x | 0.83x | 1.40x | 1.38x | 1.48x | 1.17x | 1.00x | 1.00x | 1.02x | 1.00x |
| logging_format | 1.67x | 1.66x | 1.53x | 1.47x | 1.67x | 1.73x | 1.00x | 0.99x | 1.01x | 1.00x |
| mako | 1.72x | 1.37x | 1.61x | 1.28x | 1.65x | 1.38x | 1.00x | 1.00x | 1.12x | 1.11x |
| many_optionals | 1.53x | 1.51x | 1.44x | 1.40x | 1.55x | 1.53x | 1.00x | 1.01x | 1.03x | 1.00x |
| mdp | 1.69x | 1.69x | 1.73x | 1.76x | 1.71x | 1.72x | 1.00x | 1.00x | 0.92x | 0.92x |
| meteor_contest | 1.41x | 1.39x | 1.37x | 1.35x | 1.42x | 1.40x | 1.00x | 1.00x | 1.06x | 1.00x |
| nbody | 1.38x | 1.40x | 1.36x | 1.37x | 1.45x | 1.43x | 1.00x | 1.00x | 1.05x | 0.99x |
| nqueens | 1.99x | 2.01x | 1.81x | 1.77x | 2.05x | 2.06x | 1.00x | 1.00x | 1.05x | 0.99x |
| pathlib | 1.49x | 1.45x | 1.40x | 1.33x | 1.53x | 1.45x | 1.00x | 1.00x | 1.04x | 1.00x |
| pickle | 1.73x | 1.53x | 1.53x | 1.28x | 1.75x | 1.52x | 1.00x | 1.03x | 1.05x | 1.03x |
| pidigits | 1.07x | 0.62x | 1.07x | 0.63x | 1.07x | 0.63x | 1.00x | 1.00x | 1.00x | 0.99x |
| pprint_pformat | 1.78x | 1.78x | 1.59x | 1.58x | 1.75x | 1.83x | 1.00x | 1.00x | 1.05x | 0.99x |
| pprint_safe_repr | 1.77x | 1.77x | 1.59x | 1.57x | 1.75x | 1.83x | 1.00x | 1.00x | 1.05x | 0.98x |
| pyflate | 1.25x | 1.17x | 1.18x | 1.10x | 1.28x | 1.19x | 1.00x | 0.99x | 1.03x | 0.99x |
| python_startup | 1.34x | 1.18x | 1.28x | 1.12x | 1.36x | 1.18x | 1.00x | 0.99x | 0.98x | 0.97x |
| quadtree_nbody | 1.51x | 1.50x | 1.36x | 1.40x | 1.53x | 1.53x | 1.00x | 1.00x | 1.07x | 0.99x |
| raytrace | 1.57x | 1.55x | 1.45x | 1.44x | 1.59x | 1.58x | 1.00x | 1.00x | 1.05x | 0.99x |
| regex_compile | 1.55x | 1.52x | 1.41x | 1.39x | 1.54x | 1.56x | 1.00x | 1.00x | 1.04x | 1.00x |
| regex_dna | 0.96x | 0.87x | 1.03x | 0.90x | 0.96x | 0.84x | 1.00x | 1.00x | 0.97x | 0.97x |
| regex_effbot | 1.00x | 1.03x | 1.05x | 1.06x | 0.98x | 1.06x | 1.00x | 1.00x | 0.98x | 0.98x |
| regex_v8 | 1.32x | 1.19x | 1.24x | 1.16x | 1.25x | 1.13x | 1.00x | 0.99x | 1.07x | 1.05x |
| richards | 1.36x | 1.34x | 1.29x | 1.27x | 1.36x | 1.36x | 1.00x | 1.01x | 1.01x | 0.97x |
| richards_super | 1.36x | 1.34x | 1.31x | 1.29x | 1.38x | 1.37x | 1.00x | 1.00x | 1.00x | 0.96x |
| scimark_fft | 1.87x | 2.02x | 1.64x | 1.61x | 2.07x | 1.97x | 1.00x | 0.99x | 1.04x | 0.98x |
| shortest_path | 1.08x | 1.02x | 1.07x | 1.01x | 1.08x | 1.02x | 1.00x | 1.00x | 1.02x | 1.02x |
| spectral_norm | 1.58x | 1.53x | 1.41x | 1.35x | 1.56x | 1.60x | 1.00x | 1.00x | 1.05x | 0.99x |
| sphinx | 1.44x | 1.42x | 1.37x | 1.30x | 1.45x | 1.41x | 1.00x | 1.00x | 1.04x | 1.00x |
| sqlglot_v2_parse | 1.47x | 1.49x | 1.38x | 1.38x | 1.50x | 1.53x | 1.00x | 1.00x | 1.04x | 1.00x |
| sqlite_synth | 1.77x | 1.76x | 1.67x | 1.55x | 1.85x | 1.86x | 1.00x | 1.01x | 1.05x | 0.99x |
| telco | 2.12x | 1.91x | 1.75x | 1.52x | 2.15x | 2.03x | 1.00x | 0.98x | 1.05x | 0.99x |
| tomli_loads | 1.49x | 1.44x | 1.38x | 1.38x | 1.50x | 1.51x | 1.00x | 1.00x | 1.06x | 1.01x |
| tornado_http | 1.40x | 0.98x | 1.34x | 0.95x | 1.41x | 0.99x | 1.00x | 1.00x | 1.06x | 1.03x |
| typing_runtime_protocols | 2.07x | 2.10x | 1.77x | 1.76x | 2.16x | 2.15x | 1.00x | 0.99x | 1.05x | 1.00x |
| unpack_sequence_list | 0.90x | 0.87x | 0.90x | 0.89x | 0.78x | 0.92x | 1.00x | 1.00x | 1.00x | 1.00x |
| urlsafe_base64_small | 1.19x | 1.19x | 1.17x | 1.13x | 1.18x | 1.20x | 1.00x | 0.99x | 1.00x | 0.98x |
| xdsl_constant_fold | 1.69x | 1.69x | 1.54x | 1.51x | 1.68x | 1.70x | 1.00x | 1.00x | 1.02x | 0.98x |
| xml_etree_parse | 1.38x | 1.29x | 1.29x | 1.17x | 1.44x | 1.29x | 1.00x | 1.00x | 1.03x | 1.00x |
| yaml | 1.48x | 1.44x | 1.36x | 1.33x | 1.47x | 1.46x | 1.00x | 1.00x | 1.01x | 0.99x |

Geomean vs baseline (>1 is faster):

- default: 1.469x
- nomimalloc: 1.349x
- nolto: 1.364x
- nolto-nomimalloc: 1.263x
- seplto: 1.458x
- seplto-nomimalloc: 1.372x
- reference-nolto: 0.999x
- reference-mimalloc: 1.033x
- reference-nolto-mimalloc: 0.997x
