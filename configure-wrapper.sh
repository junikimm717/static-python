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

# `-fuse-linker-plugin` lets ld.bfd dlopen liblto_plugin.so and resolve LTO
# bitcode out of static archives, which is what gives us cross-archive
# inlining (e.g. libssl.a -> python). Requires a dynamically-linked host
# toolchain; see cross-make/config.mak. `-fno-fat-lto-objects` is legal
# once the plugin is in play and halves .o size.
export LDFLAGS="-Wl,--export-dynamic -static -no-pie \
  -flto -flto-partition=none -fuse-linker-plugin -fno-fat-lto-objects \
  -s --static -L$ROOT/build-$TARGET/lib \
  -L$ROOT/build-$TARGET/lib64\
  -L$DEPS_DIR/$TARGET-$TCTYPE/$TARGET/lib\
  -Wl,--gc-sections -Wl,-O1 -Wl,--as-needed"

export LINKFORSHARED=" "
# include/uuid: util-linux installs uuid.h under a subdir but Python does
# `#include <uuid.h>`. include/ncursesw: same idea for ncurses wide headers.
export CFLAGS="-I$ROOT/build-$TARGET/include \
  -I$ROOT/build-$TARGET/include/ncursesw \
  -I$ROOT/build-$TARGET/include/uuid \
  -O3 -flto -flto-partition=none -fuse-linker-plugin -fno-fat-lto-objects \
  -Wno-error -no-pie -w -pipe -ffunction-sections -fdata-sections"

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
