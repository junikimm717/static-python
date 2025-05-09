#!/bin/sh

ROOT="$(realpath "$(dirname "$0")")"
DEPS_DIR="$(realpath "$(dirname "$0")")/deps"

if test -z "$ARCH"; then
  ARCH="x86_64"
fi
TOOLCHAIN="$ARCH-linux-musl"
echo "====================="
echo "configure-wrapper.sh: Using Arch $ARCH..."
echo "====================="

export CC="$DEPS_DIR/$TOOLCHAIN-cross/bin/$TOOLCHAIN-gcc"
export AR="$DEPS_DIR/$TOOLCHAIN-cross/bin/$TOOLCHAIN-ar"
export RANLIB="$DEPS_DIR/$TOOLCHAIN-cross/bin/$TOOLCHAIN-ranlib"
export LD="$DEPS_DIR/$TOOLCHAIN-cross/bin/$TOOLCHAIN-ld"
export LDFLAGS="-Wl,--export-dynamic -static --static -L$ROOT/build/lib -L$ROOT/build/lib64"
export LINKFORSHARED=" "
export CFLAGS="-I$ROOT/build/include -I$ROOT/build/include/ncursesw -g0 -O2 -fno-align-functions -fno-align-jumps -fno-align-loops -fno-align-labels -Wno-error -fPIC"
export PREFIX="$ROOT/build"

if ! test -z "$PYTHON"; then
  export PREFIX="$ROOT/python-static-$ARCH"
  exec "$@" LDFLAGS="$LDFLAGS" CFLAGS="$CFLAGS"
else
  exec "$@"
fi
