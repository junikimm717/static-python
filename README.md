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

- **meson and ninja** (for libuuid)
- **unzip**
- **perl** with **FindBin.pm** (apparently on some distros you need to install
  perl-core?)
- cURL, tar, make

A C compiler should not be strictly necessary, as the build system compiles
eveything with a musl toolchain that it downloads.

## Building

Cross-compiling is now supported from x86_64! It turns out though that to
do cross-compilation, a native interpreter must first get built 💀

To build, just use the Makefile and run `make python3 ARCH={insert}`. You
should be able to find the resulting output in `./python-static-$(ARCH)`, where
`$(ARCH)` is the architecture that you chose (defaults to native architecture if
blank).

You can view supported architectures in the Makefile under the `SUPPORTED`
variable. (I assume if you are actually trying to run this project, you for sure
know what you are doing 😇)

It turns out that a limitation to further support is OpenSSL injecting custom
assembly that the musl toolchains don't support :/ In any case, x86 and aarch64
should cover most modern devices, with the other architectures being supported
for vanity reasons :)
