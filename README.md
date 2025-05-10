# Statically Linked Python

A stupid project where I attempt to build a functional, dependency-free python
interpreter on Linux. This project is mostly as a hobby. For almost all intents
and purposes, you really should use your standard dynamically linked python
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

- **meson and ninja** (for libuuid)
- **unzip**
- **perl** (apparently required by OpenSSL's build system?)
- **python** (only if you are cross-compiling)
- cURL, tar, makea

A C compiler should not be strictly necessary, as the build system compiles
eveything with a musl toolchain that it downloads.

## Building

Cross-compiling is now supported from x86_64!

To build, just use the Makefile and run `make python3 ARCH={insert}`. You
should be able to find the resulting output in `./python-static-$(ARCH)`, where
`$(ARCH)` is the architecture that you chose (defaults to native architecture).

You can view supported architectures in the Makefile under the `SUPPORTED`
variable. (I assume if you are actually trying to run this project, you know
what you are doing)
