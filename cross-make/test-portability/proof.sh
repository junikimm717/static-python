#!/bin/sh
# Host-side driver for the portability proof. See ai/PORTABILITY_PROOF.md.
#
# Builds the alien (debian:stable-slim) image, runs run.sh inside it with
# the toolchain tarball and tests mounted in, and tees output to
# build-logs/portability-alien.log on the repo root.
#
# Re-run after rebuilding the toolchain. The tarball at
#   cross-make/test-portability/x86_64-linux-musl-native.tgz
# is the artefact being proved; regenerate it from a current
#   deps-x86_64-linux-musl/x86_64-linux-musl-native/
# tree with:
#   docker compose exec -T spython sh -lc \
#     'cd /workspace && tar -czf cross-make/test-portability/x86_64-linux-musl-native.tgz \
#        -C deps-x86_64-linux-musl x86_64-linux-musl-native'

set -eu

cd "$(dirname "$0")"
HERE=$(pwd)
REPO=$(cd ../.. && pwd)
LOG=$REPO/build-logs/portability-alien.log
TARBALL=$HERE/x86_64-linux-musl-native.tgz
IMAGE=static-python-portability-alien:latest

if [ ! -f "$TARBALL" ]; then
	echo "ERROR: missing toolchain tarball: $TARBALL" >&2
	echo "       (regenerate from deps-x86_64-linux-musl/, see top of $0)" >&2
	exit 1
fi

mkdir -p "$(dirname "$LOG")"

echo "building alien image ..."
docker build -t "$IMAGE" -f "$HERE/Dockerfile.alien" "$HERE"

echo "running proof; tee'd to $LOG"
{
	echo "=== portability-alien proof ==="
	echo "=== started: $(date -u +%FT%TZ)"
	echo "=== tarball: $TARBALL ($(stat -c%s "$TARBALL") bytes)"
	docker run --rm \
		--platform=linux/amd64 \
		-v "$TARBALL:/toolchain.tgz:ro" \
		-v "$HERE/tests:/tests:ro" \
		-v "$HERE/run.sh:/run.sh:ro" \
		"$IMAGE" \
		/bin/sh /run.sh
	echo "=== finished: $(date -u +%FT%TZ) exit=$?"
} 2>&1 | tee "$LOG"
