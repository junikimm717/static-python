# Python with Static + Cross + LTO (Rewritten in Go)

Building a (mostly) functional cross-compiled python interpreter with zero
shared libraries and full link-time optimization (-O3 -flto). We gain up to
**20% geomean speedup** on pyperf over dynamically linked python on glibc (!!)

**Warning:** this project is exclusively as a hobby. For basically all intents
and purposes, you should use your standard dynamically linked python
interpreter.

Contrary to a bunch of Stack Overflow/forum posts, this was far harder than
initially anticipated and involved extensive fiddling/patching. This repo is the
result of my madness while procrastinating studying for MIT finals :).

**Update in 2026**: I have come back to this project to procrastinate from
studying for finals again. The LLM's are way better now, so I developed some
additional profiling infrastructure and let opus iron out some latent bugs.

Python ABI (Application Binary Interface) support through `ctypes` is mostly
there plus or minus epsilon (No deprecated ABI's are included). The build
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

That container also registers qemu in binfmt, which is why it runs privileged.

Otherwise, `./staticpy doctor` says what your machine is missing. It is a short
list -- go, perl, patch, busybox, cURL, tar, and a qemu-user for whichever
target you want to verify.

## The build system

`src/staticpy` is a Go build system with a content-addressed job graph:
rebuilding with identical inputs is a no-op, and changing one configure flag
rebuilds exactly what depends on it. `./staticpy` is a shim around it that
rebuilds the binary when the sources move and fetches the toolchain a build
needs.

```sh
./staticpy doctor   # what this machine has and is missing
./staticpy help     # or `help <command>` for the details
```

`./staticpy help` is the real documentation and is kept honest;
[`AGENTS.md`](AGENTS.md) has the design, the toolchain story and the workflows.

## Building Native

```sh
# Build python. The toolchain is downloaded, not built here.
./staticpy build
# run your statically linked python3!
./dist/artifacts/pynative_default_$(./staticpy print host)/bin/python3
```

Add `--pack` and the result is also written as a relocatable tarball under
`dist/out/<profile>/<triple>/`, next to its sha256.

The compilers come from [gccfactory](https://github.com/junikimm717/gccfactory),
which builds the whole host x target matrix of GCC + musl and publishes one
relocatable tarball per cell; the shim fetches the one it needs.

## Cross Compiling

```sh
# Cross-compile to aarch64, verify it under qemu, and pack the tarball.
./staticpy build --target aarch64-linux-musl --verify core --pack

# ...or every target at once.
./staticpy build --target all --verify core --pack
```

Cross-compiling is now officially supported from x86_64 and aarch64! This
took soooo long to do, and it doesn't seem like that I will be able to support
all the architectures I initially wanted to :/

`--verify` runs the thing under qemu before it is allowed to become an
artifact: `smoke` is import probes, `core` is a curated subset of CPython's
suite, `full` is all of it.

Supported architectures are one row each in `config/targets.toml`;
`./staticpy print targets-all` lists them. (I assume if you are actually trying
to run this project, you for sure know what you are doing 😇)

## Benchmarking

```sh
# a stock --enable-shared build of the same pinned source, by this machine's gcc
./staticpy build --profile reference
./staticpy bench --interp static --interp reference --baseline reference

# eight-arm LTO × allocator × static/dynamic sweep; baseline defaults to reference
./staticpy bench --interp ablation
```

That runs pyperformance -- what speed.python.org publishes against -- and
writes a session directory under `dist/bench/<UTC-stamp>-<arch>/` containing
`report.md`, `report.html`, `manifest.json` (protocol and pins), `env.json`
(kernel, cpu, memory, affinity), and the raw pyperf JSON.
`--suite micro` runs a stdlib-only loop set instead, which needs no network.
