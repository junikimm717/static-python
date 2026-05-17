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
# `-I.../include/uuid` mirrors the existing ncursesw entry: util-linux installs
# libuuid's header to $prefix/include/uuid/uuid.h, but Python's _uuidmodule.c
# does `#include <uuid.h>` when HAVE_UUID_H is defined. Without an explicit -I
# for the subdir, the compile fails. We still also have the bare include path
# so headers in our prefix root (zlib.h, ffi.h, bzlib.h, etc.) keep resolving.
export CFLAGS="-I$ROOT/build-$TARGET/include \
  -I$ROOT/build-$TARGET/include/ncursesw \
  -I$ROOT/build-$TARGET/include/uuid \
  -O3 -flto -Wno-error -no-pie -w -pipe -ffunction-sections -fdata-sections"

# Sandbox pkg-config so it only sees the .pc files we produced inside this
# repo. Without this, Python's configure happily reaches into Alpine's
# /usr/lib/pkgconfig and pulls in things like
#   pkg_cv_LIBFFI_LIBS='-L/usr/lib/../lib -lffi'
#   pkg_cv_LIBUUID_CFLAGS='-I/usr/include/uuid'
# which silently mix the host's libffi/libuuid with our static prefix. For
# packages we don't ship a .pc for (bzip2, ncurses, openssl) pkg-config simply
# fails and Python falls back to its built-in header tests, which work because
# CFLAGS/LDFLAGS already point at our prefix.
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
