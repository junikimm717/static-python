# Statically Linked Python

A stupid project where I attempt to build a functional, dependency-free python
interpreter on Linux.

Only native toolchains are supported because the resulting python binary needs
to be executed on the host system + a bunch of other stupid errors.

Python ABI support is mostly there plus or minus epsilon (No deprecated ABI's
are included in my hacked module). The Makefile injects some code into the
Python source tree to make all of this work :)

The resulting build comes with pip and venv supported, so you can actually
install packages (mostly) as normal. Running python modules dependent on c code
is not a goal because those modules would have to be shoved into the resulting
python binary.

## Setup

You should have basic tools on your system like curl, tar, make, ...

To build, just use the Makefile and run `make python3`. You should be able to
find the resulting output in `./python-static-$(ARCH)`.
