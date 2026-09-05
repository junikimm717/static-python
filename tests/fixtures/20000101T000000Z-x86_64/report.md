# FIXTURE / demo — not a published score

This session is synthetic. The stamp is 20000101T000000Z and the cpu model
contains FIXTURE. Do not compare these ratios to a real machine.

## Environment

- protocol: 2
- git_revision: 0000000000000000000000000000000000000000
- suite: pyperformance 1.14.0, pyperf 2.10.0
- kernel: Linux 0.0.0-FIXTURE
- cpu: FIXTURE CPU (synthetic demo, not a measured score)
- memory: 0 B
- baseline: reference

## Interpreters

| label | sha256 | linkage | lto | allocator | pgo | size |
|---|---|---|---|---|---|---:|
| reference | `000000000000` | dynamic | whole-graph | glibc | yes | 1 B |
| default | `111111111111` | static | whole-graph | mimalloc | yes | 1 B |

| benchmark | reference | default |
|---|---:|---:|
| bm_x | 1.00x | 1.20x |

Geomean vs baseline (>1 is faster):

- default: 1.200x
