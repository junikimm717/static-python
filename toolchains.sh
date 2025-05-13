#!/bin/sh

DIR="$(realpath "$(dirname "$0" )" )"
cd "$DIR" || exit 1

mkdir -p tarballs
for ARCH in $@; do
TCTYPE="cross"
if test "$ARCH" = "$(uname -m)"; then
  TCTYPE="native"
fi
sh <<EOF || exit 1
make crossmake USE_CROSSMAKE=1 ARCH="$ARCH" || exit 1
cd deps-$ARCH || exit 1
tar -czf ../tarballs/$ARCH-linux-musl-$TCTYPE.tar.gz $ARCH-linux-musl-$TCTYPE || exit 1
EOF
done
