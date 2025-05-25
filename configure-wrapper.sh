#!/bin/sh

ROOT="$(realpath "$(dirname "$0")")"

TARGET="$ARCH-linux-$MUSLABI"
DEPS_DIR="$(realpath "$(dirname "$0")")/deps-$TARGET"

case "$TCTYPE" in
  "native"|"cross")
    export CC="$DEPS_DIR/$TARGET-$TCTYPE/bin/$TARGET-gcc"
    export AR="$DEPS_DIR/$TARGET-$TCTYPE/bin/$TARGET-gcc-ar"
    export RANLIB="$DEPS_DIR/$TARGET-$TCTYPE/bin/$TARGET-gcc-ranlib"
    export LD="$DEPS_DIR/$TARGET-$TCTYPE/bin/$TARGET-ld"
    ;;
  *)
    echo "\$TCTYPE must be either 'cross' or 'native' (set to '$TCTYPE')! Exiting..."
    exit 1
    ;;
esac

echo "====================="
echo "configure-wrapper.sh: Using target $TARGET in configuration $TCTYPE..."
echo "====================="

export LDFLAGS="-Wl,--export-dynamic -static -no-pie -flto \
  -s --static -L$ROOT/build-$TARGET/lib \
  -L$ROOT/build-$TARGET/lib64\
  -L$DEPS_DIR/$TARGET-$TCTYPE/$TARGET/lib\
  -Wl,--gc-sections -Wl,-O1 -Wl,--as-needed"

export LINKFORSHARED=" "
export CFLAGS="-I$ROOT/build-$TARGET/include \
  -I$ROOT/build-$TARGET/include/ncursesw \
  -O3 -flto -Wno-error -no-pie -w -pipe -ffunction-sections -fdata-sections"

export PREFIX="$ROOT/build-$TARGET"

if ! test -z "$PYTHON_BUILD"; then
  export PREFIX="$ROOT/python-static-$ARCH"
  exec "$@" LDFLAGS="$LDFLAGS" CFLAGS="$CFLAGS"
elif ! test -z "$MESON"; then
  exec "$@" -Dc_link_args="$LDFLAGS" -Dc_args="$CFLAGS" --prefix="$PREFIX"
else
  exec "$@"
fi
