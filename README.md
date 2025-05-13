# Statically Linked Python

A stupid project where I attempt to build a functional, dependency-free python
interpreter for Linux. Contrary to a bunch of Stack Overflow/forum posts, this
was a far more difficult problem than initially anticipated and involved
extensive fiddling/patching. This repo is the result of my madness while
procrastinating studying for MIT finals :).

This project is exclusively as a hobby. For basically all
intents and purposes, you should use your standard dynamically linked python
interpreter.

Python ABI support is mostly there plus or minus epsilon (No deprecated ABI's
are included in my hacked module). The Makefile injects some monkey-patched code
into the Python source tree to make all of this work^^.

The resulting build comes with almost the entire standard library supported, so
you can actually install packages (mostly) as normal. But any modules dependent
on C/Rust shared libraries for performance (e.g. numpy) will obviously
fail immediately. It may be a future goal to bundle some of these modules, but
universal coverage is impossible.

## Setup

Here's a somewhat comprehensive list of things you should have on your system,
most of which should be present if you're already building a lot of things:

- **meson, ninja, flex, bison** (for libuuid)
- **ncurses** (stupid terminfo things)
- **unzip**
- **perl** with **FindBin.pm** (apparently on some distros you need to install
  perl-core?)
- cURL, tar, make, rsync

Alternatively, you can just use the docker configs provided and run
```sh
docker compose up -d
./dev.sh
```

## Building Native

```sh
# using the standard binary toolchains from the internet
make
# manually compiling your own gcc toolchain
make CROSSMAKE=1
```

## Cross Compiling

```sh
# If you are trying to compile to an aarch64 target from x86_64
make && make python3 ARCH=aarch64
```

Cross-compiling is now officially supported from x86_64 and aarch64! This took
soooo long to do, and it doesn't seem like that I will be able to support all
the architectures I initially wanted to :/

As seen above, if you are cross compiling, **You MUST build the native
interpreter first**. Cross-compiled python interpreters can't be run on the
system, so you need a native python to install all your libraries correctly.

The resulting output should be findable in `./python-static-$(ARCH)`, where
`$(ARCH)` is the architecture that you chose (defaults to native architecture if
blank). You can also supply `NEED_CROSSMAKE` to force building the musl
toolchain from scratch.

You can also view supported architectures in the Makefile under the `SUPPORTED`
variable. (I assume if you are actually trying to run this project, you for sure
know what you are doing 😇)
