# Statically Linked Python

A stupid project where I attempt to build a functional, dependency-free python
interpreter on Linux.

Only native toolchains are supported because the resulting python binary needs
to be executed on the host system + a bunch of other stupid errors.

Python ABI support is mostly there plus or minus epsilon (No deprecated ABI's
are included in my hacked module). The Makefile injects some code into the
Python source tree to make all of this work :)

The resulting build comes with pip and venv supported, so you can actually
install packages (mostly) as normal. Running Python modules dependent on C code
is not a goal because those modules would have to be shoved into the resulting
python binary.

## Setup

Here's a somewhat comprehensive list of things you should have on your system:
- **unzip**
- **perl** (apparently required by OpenSSL's build system?)
- cURL, tar, make, other basic utilities

A C compiler should not be strictly necessary, as the build system compiles
eveything with a musl toolchain that it downloads.

## Building

To build, just use the Makefile and run `make python3`. You should be able to
find the resulting output in `./python-static-$(ARCH)`, where `$(ARCH)` is the
architecture of your system.
