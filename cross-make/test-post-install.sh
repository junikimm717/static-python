#!/bin/sh

set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

TARGET=x86_64-linux-musl
GCC_VER=15.1.0
INSTALL=$TMP/install
LIBEXEC=$INSTALL/libexec/gcc/$TARGET/$GCC_VER

mkdir -p "$INSTALL/bin" "$LIBEXEC/.real" "$INSTALL/$TARGET/bin"

cat > "$TMP/tool.c" <<'EOF'
int main(void) { return 0; }
EOF
cc -O2 -o "$INSTALL/bin/$TARGET-gcc" "$TMP/tool.c"
cc -O2 -o "$LIBEXEC/cc1" "$TMP/tool.c"

cat > "$TMP/plugin.c" <<'EOF'
int plugin_marker(void) { return 42; }
EOF
cc -shared -fPIC -o "$LIBEXEC/future_plugin.so" "$TMP/plugin.c"

# Exercise recovery of old tarballs where a previous post-install wrapped
# liblto_plugin.so and left the real shared object in .real/.
cc -static -no-pie -O2 -o "$LIBEXEC/liblto_plugin.so" "$TMP/tool.c"
cc -shared -fPIC -o "$LIBEXEC/.real/liblto_plugin.so" "$TMP/plugin.c"

# Simulate a future file(1) classification change. Wrapping must be based on
# ELF program headers, not on descriptive text that can drift across versions.
mkdir -p "$TMP/fakebin"
cat > "$TMP/fakebin/file" <<EOF
#!/bin/sh
case " \$* " in
	*future_plugin.so*)
		echo "ELF 64-bit LSB pie executable, x86-64, dynamically linked"
		exit 0
		;;
esac
exec /usr/bin/file "\$@"
EOF
chmod 0755 "$TMP/fakebin/file"

PATH="$TMP/fakebin:$PATH" "$ROOT/cross-make/post-install.sh" \
	"$INSTALL" "$TARGET" "$GCC_VER" >/dev/null

if ! file -b "$INSTALL/bin/$TARGET-gcc" | grep -qE 'statically linked|static-pie linked'; then
	echo "test-post-install: expected dynamic executable to be wrapped" >&2
	exit 1
fi
if ! readelf -l "$INSTALL/bin/.real/$TARGET-gcc" | grep -q 'Requesting program interpreter'; then
	echo "test-post-install: wrapped executable was not moved to .real/" >&2
	exit 1
fi

if ! file -b "$LIBEXEC/future_plugin.so" | grep -q 'shared object'; then
	echo "test-post-install: future_plugin.so was wrapped or damaged" >&2
	exit 1
fi
if [ -e "$LIBEXEC/.real/future_plugin.so" ]; then
	echo "test-post-install: shared library should not be moved to .real/" >&2
	exit 1
fi

if ! file -b "$LIBEXEC/liblto_plugin.so" | grep -q 'shared object'; then
	echo "test-post-install: liblto_plugin.so recovery failed" >&2
	exit 1
fi
if [ ! -L "$INSTALL/lib/bfd-plugins/liblto_plugin.so" ]; then
	echo "test-post-install: missing bfd-plugins liblto_plugin.so symlink" >&2
	exit 1
fi

echo "test-post-install: PASS"
