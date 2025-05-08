# Statically Linked Python

A stupid project of mine where I attempt to manually create a dependency-free
python interpreter that is actually functional. Currently only on x86_64, but
should be fairly easy to extend to other architectures.

I have currently managed to get pip to work with packages that don't involve c
code. More developments should be on the way.

## Setup

You should have basic tools on your system like curl, tar, make, ...

To build, just use the Makefile and run `make python3`. You should be able to
find all output in `./build`.
