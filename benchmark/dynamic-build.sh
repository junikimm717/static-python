#!/bin/sh
# benchmark/dynamic-build.sh -- build a stock dynamic Python in-container.
# (Named with the action second so the .gitignore `build-*` rule does not
# swallow this tracked script.)
#
# Arch is detected from `uname -m`; the Python version comes from `staticpy
# print`, so the baseline cannot drift from the interpreter it is compared
# against.
#
# Designed to run inside the `spython` Docker service.  The result lands at
# python-dynamic-${HOST_ARCH}-linux-musl/bin/python${PYTHONV}, mirroring the
# static layout staticpy produces.

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
PYTHON="$("$ROOT/staticpy" print python-version)"
PYTHONV="$("$ROOT/staticpy" print python-abi)"

TARGET="${HOST_ARCH}-linux-musl"
BUILD="$ROOT/deps-dynamic-${TARGET}/Python-${PYTHON}"
PREFIX="$ROOT/python-dynamic-${TARGET}"

# staticpy names its cache entries <sha256-prefix>-<basename>, so the tarball is
# found by glob rather than by construction.
find_src() { ls "$ROOT/dist/src/"*"-Python-${PYTHON}.tgz" 2>/dev/null | head -n1; }
SRC="$(find_src)"
if [ -z "$SRC" ]; then
    "$ROOT/staticpy" sources fetch python
    SRC="$(find_src)"
fi
if [ -z "$SRC" ]; then
    echo "error: no Python-${PYTHON}.tgz in $ROOT/dist/src" >&2
    exit 1
fi

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
if [ -n "${EXTRA_CFLAGS:-}" ]; then
    export CFLAGS="$EXTRA_CFLAGS"
    echo ">>> EXTRA_CFLAGS=$EXTRA_CFLAGS"
fi
if [ -n "${EXTRA_LDFLAGS:-}" ]; then
    export LDFLAGS="$EXTRA_LDFLAGS"
    echo ">>> EXTRA_LDFLAGS=$EXTRA_LDFLAGS"
fi
# LDFLAGS_NODIST bakes the install-time libpython directory into the binary
# as an rpath, so the dynamic interpreter is self-contained -- no
# LD_LIBRARY_PATH dance for the benchmark harness or subprocess spawns.
export LDFLAGS_NODIST="-Wl,-rpath,${PREFIX}/lib"
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
# `-i test_fma_zero_result` skips a musl-1.2.5 software-fma sign-of-zero bug
# that the upstream CPython `linked_to_musl()` skip already covers for shared
# musl builds, but we mirror it here so static and dynamic stay in sync. See
# .agents/skills/staticpy-traps/references/MUSL_REPORT.md.
# JOBS defaults to `nproc` but can be overridden (e.g. `JOBS=8 ./dynamic-build.sh`)
# to keep this build from saturating the host when another build runs alongside.
JOBS="${JOBS:-$(nproc)}"
make -j"$JOBS" PROFILE_TASK='-m test --pgo -x test_re -i test_fma_zero_result'

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
