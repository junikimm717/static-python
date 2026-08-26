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
# Build python. The toolchain is downloaded, not built here.
make
# run your statically linked python3!
./python-static-$(uname -m)/bin/python3
```

## Cross Compiling

```sh
# First, compile a native python interpreter (assuming on x86_64 system).
make
# Next, cross-compile to aarch64.
make ARCH=aarch64
# ...or to riscv64.
make ARCH=riscv64
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

The toolchains themselves are not built here. They come from
[gccfactory](https://github.com/junikimm717/gccfactory), which builds the whole
host x target matrix of GCC + musl toolchains and publishes one relocatable
tarball per cell to
[dev.mit.junic.kim](https://dev.mit.junic.kim/cross/); `make` fetches the one
it needs. Every binary in them is static-musl, so a tarball drops onto any
Linux rootfs -- glibc, near-empty, whatever -- and just works. `test-portability/`
proves exactly that, in a Debian image with no compiler in it.

You can also view supported architectures in the `supported.txt` file. (I assume
if you are actually trying to run this project, you for sure know what you are
doing 😇)

## Benchmarking

The dynamic baseline still lives at `benchmark/dynamic-build.sh` -- a stock
`--enable-shared` Python of the same pinned version, which is the only honest
comparison target for a static build:

```sh
docker compose exec -T spython sh -c 'cd /workspace && ./benchmark/dynamic-build.sh'
```

The hand-rolled microbenchmark harness is gone. pyperformance is the canonical
suite -- it is what speed.python.org publishes against -- and `staticpy bench`
will drive it once bundles land, since a pip-less, dlopen-less interpreter
needs its pure-Python dependencies compiled in rather than installed.
