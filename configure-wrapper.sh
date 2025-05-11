#!/bin/sh

ROOT="$(realpath "$(dirname "$0")")"

TOOLCHAIN="$ARCH-linux-musl"
DEPS_DIR="$(realpath "$(dirname "$0")")/deps-$ARCH"

case "$TCTYPE" in
  "native")
    export CC="$DEPS_DIR/$TOOLCHAIN-native/bin/gcc"
    export AR="$DEPS_DIR/$TOOLCHAIN-native/bin/ar"
    export RANLIB="$DEPS_DIR/$TOOLCHAIN-native/bin/ranlib"
    export LD="$DEPS_DIR/$TOOLCHAIN-native/bin/ld"
    ;;
  "cross")
    export CC="$DEPS_DIR/$TOOLCHAIN-cross/bin/$TOOLCHAIN-gcc"
    export AR="$DEPS_DIR/$TOOLCHAIN-cross/bin/$TOOLCHAIN-ar"
    export RANLIB="$DEPS_DIR/$TOOLCHAIN-cross/bin/$TOOLCHAIN-ranlib"
    export LD="$DEPS_DIR/$TOOLCHAIN-cross/bin/$TOOLCHAIN-ld"
    ;;
  *)
    echo "\$TCTYPE must be either 'cross' or 'native' (set to '$TCTYPE')! Exiting..."
    exit 1
    ;;
esac

echo "====================="
echo "configure-wrapper.sh: Using Arch $ARCH in configuration $TCTYPE..."
echo "====================="

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
