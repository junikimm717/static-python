#!/bin/sh
# Build qemu-user 11.1.1 from the official tarball.
#
# Ubuntu 24.04's qemu-user is 8.2.2: too old for /proc/self/stat
# num_threads (>= 9.1) and still on the wrong side of the i386 SAHF/cc_op
# TCG bug (fixed in 11.0.4 / 11.1). download.qemu.org keeps release
# tarballs; Alpine edge does not. See I386_QEMU_SAHF.md
set -eu

QEMU_VER=11.1.1
QEMU_URL="https://download.qemu.org/qemu-${QEMU_VER}.tar.xz"
QEMU_SHA256=079ffbff8a7111bbc89022107cbabf3bbfd614d5fc9d7cc675991196aca12482

# linux-user only. Names match /usr/bin/qemu-<arch> and the binfmt shims.
TARGETS=i386-linux-user,x86_64-linux-user,arm-linux-user,armeb-linux-user,aarch64-linux-user,ppc64-linux-user,ppc64le-linux-user,s390x-linux-user,riscv32-linux-user,riscv64-linux-user,mips64-linux-user
ARCHES="x86_64 i386 aarch64 arm armeb ppc64 ppc64le s390x riscv64 riscv32 mips64"

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

curl -fsSL -o "$workdir/qemu.tar.xz" "$QEMU_URL"
echo "$QEMU_SHA256  $workdir/qemu.tar.xz" | sha256sum -c -
tar -xJf "$workdir/qemu.tar.xz" -C "$workdir"
src="$workdir/qemu-${QEMU_VER}"
cd "$src"
# configure relocates into ./build when invoked from the source tree.
./configure \
  --prefix=/usr/local \
  --target-list="$TARGETS" \
  --disable-system \
  --enable-linux-user \
  --disable-tools \
  --disable-guest-agent \
  --disable-docs \
  --disable-werror

jobs=$(nproc)
if [ -f build/build.ninja ]; then
  ninja -C build -j"$jobs"
  ninja -C build install
else
  make -j"$jobs"
  make install
fi

for arch in $ARCHES; do
  qemu=/usr/local/bin/qemu-$arch
  [ -x "$qemu" ] || { echo "qemu-user: missing $qemu" >&2; exit 1; }
  "$qemu" --version | head -n1
done
