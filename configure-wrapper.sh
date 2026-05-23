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

case "${DEBUG_SYMBOLS:-0}" in
  0)
    DEBUG_CFLAGS=""
    STRIP_LDFLAGS="-s"
    ;;
  1)
    DEBUG_CFLAGS="-g"
    STRIP_LDFLAGS=""
    ;;
  *)
    echo "DEBUG_SYMBOLS must be either 0 or 1 (set to '${DEBUG_SYMBOLS:-}')! Exiting..."
    exit 1
    ;;
esac

# Hand-roll what CPython's `--with-lto` would inject on gcc with
# --disable-shared: -flto=auto -flto-partition=none -fuse-linker-plugin.
# We don't actually pass --with-lto because it also wraps the PGO-instrument
# bootstrap link in -fno-lto, which then can't read slim LTO bitcode out of
# our static sub-dep archives (libsqlite3.a et al.). Driving LTO from the
# wrapper keeps the plugin loaded for every link, so slim archives work and
# we avoid building fat sub-deps we'd otherwise never need.
export LDFLAGS="-Wl,--export-dynamic -static -no-pie \
  -flto=auto -flto-partition=none -fuse-linker-plugin \
  $STRIP_LDFLAGS --static -L$ROOT/build-$TARGET/lib \
  -L$ROOT/build-$TARGET/lib64\
  -L$DEPS_DIR/$TARGET-$TCTYPE/$TARGET/lib\
  -Wl,--gc-sections -Wl,-O1 -Wl,--as-needed \
  ${EXTRA_LDFLAGS:-}"

export LINKFORSHARED=" "
# include/uuid: util-linux installs uuid.h under a subdir but Python does
# `#include <uuid.h>`. include/ncursesw: same idea for ncurses wide headers.
export CFLAGS="-I$ROOT/build-$TARGET/include \
  -I$ROOT/build-$TARGET/include/ncursesw \
  -I$ROOT/build-$TARGET/include/uuid \
  -O3 $DEBUG_CFLAGS -flto=auto -flto-partition=none -fuse-linker-plugin \
  -Wno-error -no-pie -w -pipe -ffunction-sections -fdata-sections \
  ${EXTRA_CFLAGS:-}"

# Pin pkg-config to our prefix so configure scripts can't leak into
# /usr/lib/pkgconfig and silently mix host libs (libffi, libuuid) with our
# static build.
export PKG_CONFIG_LIBDIR="$ROOT/build-$TARGET/lib/pkgconfig"
export PKG_CONFIG_PATH="$ROOT/build-$TARGET/lib/pkgconfig"

export PREFIX="$ROOT/build-$TARGET"

if ! test -z "$PYTHON_BUILD"; then
  export PREFIX="$ROOT/python-static-$ARCH"
  exec "$@" LDFLAGS="$LDFLAGS" CFLAGS="$CFLAGS"
elif ! test -z "$MESON"; then
  exec "$@" -Dc_link_args="$LDFLAGS" -Dc_args="$CFLAGS" --prefix="$PREFIX"
else
  exec "$@"
fi
