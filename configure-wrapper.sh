#!/bin/sh

ROOT="$(realpath "$(dirname "$0")")"

if test -z "$ARCH"; then
  ARCH="x86_64"
fi
TOOLCHAIN="$ARCH-linux-musl"
DEPS_DIR="$(realpath "$(dirname "$0")")/deps-$ARCH"
echo "====================="
echo "configure-wrapper.sh: Using Arch $ARCH..."
echo "====================="

export CC="$DEPS_DIR/$TOOLCHAIN-native/bin/gcc"
export AR="$DEPS_DIR/$TOOLCHAIN-native/bin/ar"
export RANLIB="$DEPS_DIR/$TOOLCHAIN-native/bin/ranlib"
export LD="$DEPS_DIR/$TOOLCHAIN-native/bin/ld"

export LDFLAGS="-Wl,--export-dynamic -static --static -L$ROOT/build-$ARCH/lib -L$ROOT/build-$ARCH/lib64"
export LINKFORSHARED=" "
export CFLAGS="-I$ROOT/build-$ARCH/include -I$ROOT/build-$ARCH/include/ncursesw -g0 -O2 -fno-align-functions -fno-align-jumps -fno-align-loops -fno-align-labels -Wno-error -fPIC"
export PREFIX="$ROOT/build-$ARCH"

if ! test -z "$PYTHON"; then
  export PREFIX="$ROOT/python-static-$ARCH"
  exec "$@" LDFLAGS="$LDFLAGS" CFLAGS="$CFLAGS"
else
  exec "$@"
fi
