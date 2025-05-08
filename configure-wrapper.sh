#!/bin/sh

ROOT="$(realpath "$(dirname "$0")")"
DEPS_DIR="$(realpath "$(dirname "$0")")/deps"

export CC="$DEPS_DIR/x86_64-linux-musl-cross/bin/x86_64-linux-musl-gcc"
export AR="$DEPS_DIR/x86_64-linux-musl-cross/bin/x86_64-linux-musl-ar"
export RANLIB="$DEPS_DIR/x86_64-linux-musl-cross/bin/x86_64-linux-musl-ranlib"
export LD="$DEPS_DIR/x86_64-linux-musl-cross/bin/x86_64-linux-musl-ld"
export LDFLAGS="-s -static --static -L$ROOT/build/lib -L$ROOT/build/lib64"
export LINKFORSHARED=" "
export CFLAGS="-I$ROOT/build/include -I$ROOT/build/include/ncursesw -g0 -O2 -fno-align-functions -fno-align-jumps -fno-align-loops -fno-align-labels -Wno-error -fPIC"
export PREFIX="$ROOT/build"

exec "$@"
