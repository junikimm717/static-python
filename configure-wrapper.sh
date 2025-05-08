#!/bin/sh

ROOT="$(realpath "$(dirname "$0")")"
DEPS_DIR="$(realpath "$(dirname "$0")")/deps"

if test -z "$ARCH"; then
  ARCH="x86_64-linux-musl"
fi
ARCH="$ARCH-linux-musl"
echo "====================="
echo "configure-wrapper.sh: Using Arch $ARCH..."
echo "====================="

export CC="$DEPS_DIR/$ARCH-cross/bin/$ARCH-gcc"
export AR="$DEPS_DIR/$ARCH-cross/bin/$ARCH-ar"
export RANLIB="$DEPS_DIR/$ARCH-cross/bin/$ARCH-ranlib"
export LD="$DEPS_DIR/$ARCH-cross/bin/$ARCH-ld"
export LDFLAGS="-s -static --static -L$ROOT/build/lib -L$ROOT/build/lib64"
export LINKFORSHARED=" "
export CFLAGS="-I$ROOT/build/include -I$ROOT/build/include/ncursesw -g0 -O2 -fno-align-functions -fno-align-jumps -fno-align-loops -fno-align-labels -Wno-error -fPIC"
export PREFIX="$ROOT/build"

if ! test -z "$PYTHON"; then
  exec "$@" LDFLAGS="$LDFLAGS" CFLAGS="$CFLAGS"
else
  exec "$@"
fi
