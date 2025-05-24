# Statically Linked + Cross Compiled Python

An absolutely stupid project where I attempt to build a functional python
interpreter with zero shared libraries (those pesky .so/.dll files). It also
cross-compiles to a bunch of architectures!

**Warning:** this project is almost exclusively as a hobby. For basically all
intents and purposes, you should use your standard dynamically linked python
interpreter.

Contrary to a bunch of Stack Overflow/forum posts, this was far harder than
initially anticipated and involved extensive fiddling/patching. This repo is the
result of my madness while procrastinating studying for MIT finals :).

Python ABI (Application Binary Interface) support through `ctypes` is mostly
there plus or minus epsilon (No deprecated ABI's are included in my hacked
module). The Makefile injects some monkey-patched code into the Python source
tree to make all of this work^^.

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

Cross-compiling+LTO is now officially supported from x86_64 and aarch64! This
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
