# Python with Static + Cross + LTO

Building a (mostly) functional cross-compiled python interpreter with zero
shared libraries and full link-time optimization (-O3 -flto).

**Warning:** this project is exclusively as a hobby. For basically all intents
and purposes, you should use your standard dynamically linked python
interpreter.

Contrary to a bunch of Stack Overflow/forum posts, this was far harder than
initially anticipated and involved extensive fiddling/patching. This repo is the
result of my madness while procrastinating studying for MIT finals :).

Python ABI (Application Binary Interface) support through `ctypes` is mostly
there plus or minus epsilon (No deprecated ABI's are included). The Makefile
injects some monkey-patched code into the Python source tree to make all of this
work^^.

The resulting build comes with almost the entire standard library supported, so
pure python packages should just work (It runs a django app perfectly). But any
modules dependent on C/Rust shared libraries for performance (e.g. numpy) will
just fail. It may be a future goal to bundle some of these modules, but
universal coverage will be impossible. The python ecosystem just depends way too
much on dynamic loading.

## Usage

Binaries should be ready to use in [the releases
page](https://github.com/junikimm717/static-python/releases/tag/binaries). Only
Linux (5.8+) is supported, and only the latest version of python3 (3.13) is
supported. Feel free to do more monkey patching if you want older versions :)

## Setup

If you have a sufficiently modern version of docker, just run
```sh
docker compose up -d
./dev.sh
```

Alternatively, make sure you have all of the following packages on your system
(mostly just common build tools).

- **meson, ninja, flex, bison** (for libuuid)
- **ncurses** (stupid terminfo things)
- **unzip**
- **perl** with **FindBin.pm** (apparently on some distros you need to install
  perl-core?)
- cURL, tar, make, rsync

## Building Native

```sh
# Build python while downloading gcc binaries from musl.cc
make
# Or build python while manually compiling your own gcc toolchain
make CROSSMAKE=1
# run your statically linked python3!
./python-static-$(uname -m)/bin/python3
```

## Cross Compiling

```sh
# First, compile a native python interpreter (assuming on x86_64 system).
make
# Next, cross-compile to aarch64 while downloading gcc binaries.
make ARCH=aarch64
# Then, compile to riscv64 while bootstrapping the toolchain.
make ARCH=riscv64 USE_CROSSMAKE=1
```

Cross-compiling is now officially supported from x86_64 and aarch64! This
took soooo long to do, and it doesn't seem like that I will be able to support
all the architectures I initially wanted to :/

As seen above, if you are cross compiling, **You MUST build the native
interpreter first**. Cross-compiled python interpreters can't be run on the
system, so you'll need a native python to install all your libraries correctly.

The resulting output should be findable in
`./python-static-$(ARCH)-linux-$(MUSLABI)`, where `$(ARCH)` is the architecture
that you chose (defaults to native architecture if blank). If you are on some
weird architecture, you might want to additionally specify ABI type through
`$(MUSLABI)`. You can check out different musl ABI types at
[musl.cc](https://musl.cc/)

If you don't want to build gcc from scratch, the build system will install
toolchains from either musl.cc or
[dev.mit.junic.kim](https://dev.mit.junic.kim/cross), where I have pre-built
cross-compiling toolchains from aarch64. Otherwise, supply the `USE_CROSSMAKE`
argument to make to build the cross-compiling toolchain.

You can also view supported architectures in the `supported.txt` file. (I assume
if you are actually trying to run this project, you for sure know what you are
doing 😇)

## Benchmarking (AI-Assisted)

A small benchmark harness in `benchmark/` exists to put numbers on the
"is `-O3 -flto` + static linking actually worth anything?" question.
Always native-vs-native. Run it from inside the dev container:

```sh
# (one-off) build a stock dynamic Python of the same version, using the
# container's gcc and apk-installed openssl-dev/zlib-dev/sqlite-dev/...
./benchmark/dynamic-build.sh

# run the comparison; report lands in benchmark/reports/ and is echoed
# to stdout
./benchmark/run.sh
```

The runner compares whichever of these interpreters resolve to an executable:

- `python-static-$(uname -m)-linux-musl/bin/python$(PYTHONV)` (this repo, required)
- `python-dynamic-$(uname -m)-linux-musl/bin/python$(PYTHONV)` (above, optional)
- `/usr/bin/python3` (the container's system python, optional)

It runs a CPU-bound interpreter micro-benchmark suite (`benchmark/microbench.py`)
plus an external startup / first-import probe (`benchmark/measure_startup.py`).
Each report is timestamped + arch-tagged under `benchmark/reports/` (e.g.
`2026-05-17T1818Z_x86_64.md`) so the run history is reviewable, and includes
an Environment block (CPU model, core count, cache hierarchy, kernel) so a
random row of numbers can't get mistaken for a different machine. The report
shows per-row `X / static` ratios and a final geometric-mean row per
non-static interpreter. Each path is overridable via `STATIC=` / `DYNAMIC=` /
`SYSTEM=`. Architecture comes from `uname -m`; the Python version comes from
`make print-PYTHON` -- nothing is hard-coded.

The script appends an empty `## Analysis` section to every report; the agent
or human running the benchmark is expected to fill it in with what moved and
why. See [`AGENTS.md`](AGENTS.md) for the full workflow.

### What the numbers look like

Sample run on x86_64 against a same-version stock dynamic build (see
[`benchmark/reports/2026-05-17T1818Z_x86_64.md`](benchmark/reports/2026-05-17T1818Z_x86_64.md)
for the full table and the per-run analysis).
Compiler and library versions are matched as tightly as we can get them:
static side is `musl-cross-make` `gcc 15.1.0` + `openssl 3.5.6` + `sqlite
3.51.2` + `libffi 3.5.2` + `xz 5.8.3`; dynamic side is Alpine's `gcc 15.2.0` +
the corresponding `apk` dev packages (effectively the same upstream versions).
So the remaining deltas are about link mode and `-O3 -flto` vs `-O2`, not
about compiler vintage or out-of-date libraries.

- **CPU micro-benchmarks (geomean):** the static build is about **1.17x**
  faster than a stock `-O2 --enable-shared` build of the same 3.13.13 source.
  Pure-interpreter loops dominate the gain (`arith_loop` 1.51x, `except_path`
  1.45x, `func_call` 1.40x, `attr_access` 1.29x, `fib_iter` 1.27x). A few
  C-extension benches stay stubbornly red -- `json_roundtrip` ~0.74x,
  `str_format` ~0.94x, `fib_recursive` ~0.92x. With library versions now in
  sync these are clearly *intrinsic* to static + LTO on those workloads, most
  likely an icache/inlining trade-off in the much-larger LTO'd binary, not a
  library-version artefact.
- **Startup / first-import (geomean):** the static build spawns about **1.13x**
  faster than the same-version dynamic build -- the frozen-stdlib +
  no-`dlopen` benefit, isolated. Bare-interpreter spawn alone is ~1.13x;
  `import json/os/re/...` is also ~1.15x.

So: a ~17% interpreter-loop win, a ~13% spawn-time win, paid for by ~5--25%
on a handful of C-extension hot paths that are *not* explained by version
mismatch (we matched versions and they didn't move). Worth doing if you care
about a self-contained binary or fast-spawning short-lived processes; less
worth it if your hot path lives in `_json` / `_struct` / `_codecs` /
`_decimal`-flavoured extension modules.
