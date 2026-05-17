#!/bin/sh
# benchmark/dynamic-build.sh -- build a stock dynamic Python in-container.
# (Named with the action second so the .gitignore `build-*` rule does not
# swallow this tracked script.)
#
# Arch is detected from `uname -m`; the Python version is read from the
# project Makefile (PYTHON / PYTHONV), so this script never duplicates any
# version or architecture string from the Makefile.
#
# Designed to run inside the `spython` Docker service.  The result lands at
# python-dynamic-${HOST_ARCH}-linux-musl/bin/python${PYTHONV}, mirroring the
# static layout the main Makefile produces.

set -eu

if ! command -v apk >/dev/null 2>&1; then
    cat >&2 <<'EOF'
error: this script must be run inside the spython dev container (Alpine + apk).
       from your host shell:
           docker compose exec spython ./benchmark/dynamic-build.sh
EOF
    exit 1
fi

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"

HOST_ARCH="${HOST_ARCH:-$(uname -m)}"
# $(info ...) directives in the Makefile fire at parse time and bleed into
# `make`'s stdout; `tail -n1` picks the actual echoed value of the variable.
PYTHON="$(make -C "$ROOT" -s print-PYTHON 2>/dev/null | tail -n1)"
PYTHONV="$(make -C "$ROOT" -s print-PYTHONV 2>/dev/null | tail -n1)"

if [ -z "${PYTHON:-}" ] || [ -z "${PYTHONV:-}" ]; then
    echo "error: could not read PYTHON/PYTHONV from $ROOT/Makefile" >&2
    exit 1
fi

TARGET="${HOST_ARCH}-linux-musl"
SRC="$ROOT/tarballs/Python-${PYTHON}.tgz"
BUILD="$ROOT/deps-dynamic-${TARGET}/Python-${PYTHON}"
PREFIX="$ROOT/python-dynamic-${TARGET}"

if [ ! -f "$SRC" ]; then
    echo "error: missing source tarball: $SRC" >&2
    echo "       run \`make $SRC\` (or any other target that depends on it) first." >&2
    exit 1
fi

echo ">>> installing Alpine -dev headers for python ${PYTHON} on ${HOST_ARCH}"
apk add --no-cache \
    openssl-dev zlib-dev sqlite-dev libffi-dev bzip2-dev xz-dev \
    ncurses-dev readline-dev util-linux-dev expat-dev linux-headers

echo ">>> extracting Python-${PYTHON}.tgz"
rm -rf "$BUILD" "$PREFIX"
mkdir -p "$(dirname "$BUILD")"
tar -xzf "$SRC" -C "$(dirname "$BUILD")"

echo ">>> configuring (--enable-shared --enable-optimizations --with-lto)"
# Matches python.org's release-build flags so the dynamic baseline reflects
# stock optimized CPython. DYNAMIC_NO_PGO=1 reverts to plain `-O3 -Wall`.
cd "$BUILD"
if [ "${DYNAMIC_NO_PGO:-0}" = "1" ]; then
    OPT_FLAGS=""
    echo ">>> DYNAMIC_NO_PGO=1: skipping --enable-optimizations / --with-lto"
else
    OPT_FLAGS="--enable-optimizations --with-lto"
fi
# LDFLAGS_NODIST bakes the install-time libpython directory into the binary
# as an rpath, so the dynamic interpreter is self-contained -- no
# LD_LIBRARY_PATH dance for the benchmark harness or subprocess spawns.
LDFLAGS_NODIST="-Wl,-rpath,${PREFIX}/lib" \
    ./configure \
        --prefix="$PREFIX" \
        --enable-shared \
        $OPT_FLAGS \
        --without-ensurepip \
        --with-system-ffi --with-system-expat \
        --enable-loadable-sqlite-extensions \
        --with-computed-gotos

echo ">>> building"
# `-x test_re` skips two locale tests that fail on musl and abort PGO.
make -j"$(nproc)" PROFILE_TASK='-m test --pgo -x test_re'

echo ">>> installing to $PREFIX"
make install

echo ">>> sanity check (no LD_LIBRARY_PATH; rpath should take care of it)"
"$PREFIX/bin/python${PYTHONV}" -c '
import sys, ssl, zlib, sqlite3, ctypes, _lzma, _hashlib
print(sys.version)
print("ssl    :", ssl.OPENSSL_VERSION)
print("sqlite :", sqlite3.sqlite_version)
print("zlib   :", zlib.ZLIB_VERSION)
'

echo ">>> done: $PREFIX/bin/python${PYTHONV}"
