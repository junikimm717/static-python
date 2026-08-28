# Python with Static + Cross + LTO

Building a (mostly) functional cross-compiled python interpreter with zero
shared libraries and full link-time optimization (-O3 -flto). We gain up to
**20% geomean speedup** on benchmarks over dynamically linked python (!!)

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

Alternatively, make sure you have the following on your system. `./staticpy
doctor` checks for them and says what is missing.

- **go**, to build the builder itself
- **perl** with **FindBin.pm** (openssl's Configure; apparently on some distros
  you need to install perl-core?)
- **patch**
- **busybox**, which supplies `sh`/`awk`/`sed` to the hermetic builds. Without
  it the host's own tools get used and the result is not reproducible elsewhere.
- **qemu-user** for whichever target you want to verify
- cURL and tar

## The build system

`src/staticpy` is a Go build system. Versions, checksums, configure flags and
per-target quirks live in `config/*.toml`, and underneath is a
content-addressed job graph: every job's key is hashed over its sources,
patches, flags, triples and its dependencies' keys, so rebuilding with identical
inputs is a no-op and changing one configure flag rebuilds exactly what depends
on it.

`./staticpy` is a shim around that binary. It rebuilds the binary whenever the
sources are newer, and fetches the toolchain a given build needs. staticpy
itself never fetches a compiler -- it is handed one, and fails loudly when one
is missing rather than falling back to whatever the host happens to have.

```sh
./staticpy doctor   # what this machine has and is missing
./staticpy help     # or `help <command>` for the details
```

## Building Native

```sh
# Build python. The toolchain is downloaded, not built here.
./staticpy build
# run your statically linked python3!
./dist/artifacts/pynative_default_$(./staticpy print host)/bin/python3
```

Add `--pack` and the result is also written as a relocatable tarball under
`dist/out/<profile>/<triple>/`, next to its sha256.

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

A cross build needs a static host interpreter of the same version to freeze
bytecode against -- never the host's shipped python, which would not agree
exactly with the one being built. staticpy builds that itself as a dependency,
so you don't have to remember to do it first.

`--verify` runs the built interpreter under qemu before it is allowed to become
an artifact. `smoke` is import probes only, `core` is a curated subset of
CPython's own test suite covering the language core plus every extension module
staticpy links in by hand, and `full` is the whole suite.

The toolchains themselves are not built here. They come from
[gccfactory](https://github.com/junikimm717/gccfactory), which builds the whole
host x target matrix of GCC + musl toolchains and publishes one relocatable
tarball per cell to
[dev.mit.junic.kim](https://dev.mit.junic.kim/cross/); the `./staticpy` shim
fetches the one it needs into `dist/toolchains/`. Every binary in them is
static-musl, so a tarball drops onto any Linux rootfs -- glibc, near-empty,
whatever -- and just works. `test-portability/` proves exactly that, in a Debian
image with no compiler in it.

Supported architectures are one row each in `config/targets.toml`;
`./staticpy print targets-all` lists them. (I assume if you are actually trying
to run this project, you for sure know what you are doing 😇)

## Benchmarking

`benchmark/dynamic-build.sh` builds the dynamic baseline -- a stock
`--enable-shared` Python of the same pinned version, which is the honest
comparison target for a static build:

```sh
docker compose exec -T spython sh -c 'cd /workspace && ./benchmark/dynamic-build.sh'
```

pyperformance is the intended suite, driven by `staticpy bench`. It needs its
pure-Python dependencies compiled into the interpreter first, since a build
with no pip and no dlopen cannot install them: that arrives with bundles.
