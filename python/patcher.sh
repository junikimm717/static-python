#!/bin/sh

test "$#" -eq 2 || exit 1

DIR="$(realpath "$(dirname "$0" )" )"
cd "$DIR/.." || exit 1

ARCH="$1"
MUSLABI="$2"
TARGET="$ARCH-linux-$MUSLABI"

echo "$TARGET" >> supported.txt || exit 1
make crossmake USE_CROSSMAKE=1 ARCH="$ARCH" MUSLABI="$MUSLABI" || exit 1
make patcher USE_CROSSMAKE=1 ARCH="$ARCH" MUSLABI="$MUSLABI" || exit 1

cp ./python/pyconfig-patches.h ./python/pyconfig/$TARGET-patches.h || exit 1
./build-$TARGET/patcher >> ./python/pyconfig/$TARGET-patches.h || exit 1
