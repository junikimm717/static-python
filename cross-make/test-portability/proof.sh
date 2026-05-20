#!/bin/sh
# Host-side driver for the portability proof. See ai/PORTABILITY_PROOF.md.
#
# Builds the alien (debian:stable-slim) image, runs run.sh inside it with
# the toolchain tarball and tests mounted in, and tees output to
# build-logs/portability-alien.log on the repo root.
#
# Uses the host's native toolchain: <arch>-linux-musl-native.tgz where
# <arch> is `uname -m` (x86_64 or aarch64). Re-run after rebuilding the
# toolchain. Regenerate the tarball from deps-<arch>-linux-musl/ with:
#   docker compose exec -T spython sh -lc \
#     'cd /workspace && HOST=$(uname -m) && \
#      tar -czf cross-make/test-portability/${HOST}-linux-musl-native.tgz \
#        -C deps-${HOST}-linux-musl ${HOST}-linux-musl-native'
#
# If the tarball is missing but deps-<arch>-linux-musl/<arch>-linux-musl-native/
# exists, this script packs it automatically before running the proof.

set -eu

cd "$(dirname "$0")"
HERE=$(pwd)
REPO=$(cd ../.. && pwd)
LOG=$REPO/build-logs/portability-alien.log

HOST_ARCH=$(uname -m)
case "$HOST_ARCH" in
	x86_64|aarch64) ;;
	*)
		echo "ERROR: unsupported host arch '$HOST_ARCH' (want x86_64 or aarch64)" >&2
		exit 1
		;;
esac

TC_DIR="${HOST_ARCH}-linux-musl-native"
TARBALL=$HERE/${TC_DIR}.tgz
DEPS_TREE=$REPO/deps-${HOST_ARCH}-linux-musl/$TC_DIR
IMAGE=static-python-portability-alien:latest

if [ ! -f "$TARBALL" ]; then
	if [ -d "$DEPS_TREE" ]; then
		echo "packing toolchain tarball from $DEPS_TREE ..."
		tar -czf "$TARBALL" -C "$(dirname "$DEPS_TREE")" "$(basename "$DEPS_TREE")"
	else
		echo "ERROR: missing toolchain tarball: $TARBALL" >&2
		echo "       (build with 'make USE_CROSSMAKE=1 crossmake' or see top of $0)" >&2
		exit 1
	fi
fi

mkdir -p "$(dirname "$LOG")"

echo "building alien image ..."
docker build -t "$IMAGE" -f "$HERE/Dockerfile.alien" "$HERE"

echo "running proof (host=$HOST_ARCH); tee'd to $LOG"
{
	echo "=== portability-alien proof ==="
	echo "=== host arch: $HOST_ARCH"
	echo "=== started: $(date -u +%FT%TZ)"
	echo "=== tarball: $TARBALL ($(stat -c%s "$TARBALL") bytes)"
	# No --platform: use the host's default (amd64 on x86_64, arm64 on aarch64).
	docker run --rm \
		-v "$TARBALL:/toolchain.tgz:ro" \
		-v "$HERE/tests:/tests:ro" \
		-v "$HERE/run.sh:/run.sh:ro" \
		"$IMAGE" \
		/bin/sh /run.sh
	echo "=== finished: $(date -u +%FT%TZ) exit=$?"
} 2>&1 | tee "$LOG"
