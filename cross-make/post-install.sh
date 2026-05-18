#!/bin/sh
# Relocatable wrapper fixup for a freshly-installed musl-cross-make tree.
#
# Wraps every dynamic host binary with a static-musl launcher that exec's
# a bundled musl loader (runtime/libc.so), sidestepping .interp lookup
# so the toolchain works on any rootfs. See wrapper.c for the launcher,
# and ai/PORTABILITY_PROOF.md for the end-to-end portability test.
#
# Usage:
#   post-install.sh <install-dir> <target-triple> <gcc-version>
#
# Idempotent: a second run is a no-op because wrappers are static ELFs
# while the originals we shadow are dynamic.

set -eu

if [ $# -ne 3 ]; then
	echo "usage: $0 <install-dir> <target-triple> <gcc-version>" >&2
	exit 2
fi

INSTALL=$1
TARGET=$2
GCC_VER=$3
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)

if [ ! -d "$INSTALL" ]; then
	echo "post-install.sh: install dir does not exist: $INSTALL" >&2
	exit 1
fi

WRAPPER_SRC=$SCRIPT_DIR/wrapper.c
WRAPPER_BIN=$INSTALL/.tc-wrapper.bin

if [ ! -f "$WRAPPER_SRC" ]; then
	echo "post-install.sh: missing wrapper source: $WRAPPER_SRC" >&2
	exit 1
fi

if ! command -v patchelf >/dev/null 2>&1; then
	echo "post-install.sh: patchelf not found in PATH" >&2
	exit 1
fi

# 1) Build the launcher with the host compiler. Always plain `cc`: the
# launcher runs on whatever host the toolchain is invoked on, not the
# cross target. `-no-pie` pins `file -b` output to "statically linked",
# which keeps the idempotency check below an exact match across distros.
echo "post-install.sh: building launcher ..."
cc -static -no-pie -O2 -Wall -o "$WRAPPER_BIN" "$WRAPPER_SRC"
if ! file -b "$WRAPPER_BIN" 2>/dev/null \
		| grep -qE 'ELF.*(statically linked|static-pie linked)'; then
	echo "post-install.sh: launcher is not statically linked; aborting" >&2
	file -b "$WRAPPER_BIN" >&2 || true
	exit 1
fi

# 2) Bundle the host musl loader as runtime/libc.so. On musl the loader
# is libc, so this single file satisfies both the explicit exec target
# and the DT_NEEDED libc.so lookup (via --library-path).
echo "post-install.sh: bundling host musl loader ..."
mkdir -p "$INSTALL/runtime"
HOST_LOADER=$(readelf -p .interp /bin/sh 2>/dev/null \
	| awk '/musl/ { for (i=1; i<=NF; i++) if ($i ~ /\//) print $i }' \
	| head -n 1)
if [ -z "${HOST_LOADER:-}" ] || [ ! -f "$HOST_LOADER" ]; then
	HOST_LOADER=$(ls /lib/ld-musl-*.so.1 2>/dev/null | head -n 1)
fi
if [ -z "${HOST_LOADER:-}" ] || [ ! -f "$HOST_LOADER" ]; then
	HOST_LOADER=$(ls /lib/libc.musl-*.so.1 2>/dev/null | head -n 1)
fi
if [ -z "${HOST_LOADER:-}" ] || [ ! -f "$HOST_LOADER" ]; then
	echo "post-install.sh: could not find host musl loader (.interp of /bin/sh, /lib/ld-musl-*.so.1)" >&2
	exit 1
fi
cp -f "$HOST_LOADER" "$INSTALL/runtime/libc.so"
chmod 0755 "$INSTALL/runtime/libc.so"
HOST_ARCH=$(uname -m)
# Aliases for every DT_NEEDED libc-style SONAME the host's binutils/gcc
# might emit. Alpine's musl uses libc.musl-<arch>.so.1; other distros
# use ld-musl-<arch>.so.1. Both must resolve via --library-path.
ln -sf libc.so "$INSTALL/runtime/ld-musl-$HOST_ARCH.so.1"
ln -sf libc.so "$INSTALL/runtime/libc.musl-$HOST_ARCH.so.1"
echo "post-install.sh:   host loader = $HOST_LOADER"

# Walk a symlink chain to its final basename. Bounded to defend against
# cycles.
resolve_chain_basename() {
	cur=$1
	depth=16
	while [ -L "$cur" ] && [ "$depth" -gt 0 ]; do
		t=$(readlink "$cur")
		case "$t" in
			/*) cur=$t ;;
			*) cur=$(dirname "$cur")/$t ;;
		esac
		depth=$((depth - 1))
	done
	basename "$cur"
}

# 3) Shadow dynamic ELF executables in $dir, then re-point any symlinks
# that resolved to one of them. Pass 1 only matches executables; .so
# files (e.g. liblto_plugin.so) are left in place and patchelf'd later.
shadow_dir() {
	dir=$1
	[ -d "$dir" ] || return 0
	mkdir -p "$dir/.real"

	find "$dir" -maxdepth 1 -type f -print | while IFS= read -r f; do
		[ "$f" = "$dir/.real" ] && continue
		case "$f" in *"/.real"|*"/.real/"*) continue ;; esac
		desc=$(file -b "$f" 2>/dev/null || true)
		case "$desc" in
			*"ELF"*"executable"*"dynamically linked"*|*"ELF"*"pie executable"*"dynamically linked"*)
				name=$(basename "$f")
				mv -f "$f" "$dir/.real/$name"
				cp "$WRAPPER_BIN" "$f"
				chmod 0755 "$f"
				;;
		esac
	done

	# Iterate until quiescent so chains like cc -> gcc -> gcc-15 all get
	# repointed regardless of find ordering.
	while : ; do
		converted=0
		find "$dir" -maxdepth 1 -type l -print | while IFS= read -r s; do
			case "$s" in *"/.real"|*"/.real/"*) continue ;; esac
			name=$(basename "$s")
			tname=$(resolve_chain_basename "$s")
			[ -n "$tname" ] || continue
			[ -e "$dir/$tname" ] || continue
			[ -e "$dir/.real/$tname" ] || continue
			tdesc=$(file -b "$dir/$tname" 2>/dev/null || true)
			case "$tdesc" in
				*"ELF"*"statically linked"*|*"ELF"*"static-pie linked"*)
					rm -f "$s"
					cp "$WRAPPER_BIN" "$s"
					chmod 0755 "$s"
					ln -sf "$tname" "$dir/.real/$name"
					echo CONVERTED
					;;
			esac
		done | grep -q CONVERTED || break
	done
}

echo "post-install.sh: shadowing host binaries ..."
shadow_dir "$INSTALL/bin"
shadow_dir "$INSTALL/libexec/gcc/$TARGET/$GCC_VER"
shadow_dir "$INSTALL/libexec/gcc/$TARGET/$GCC_VER/install-tools"
shadow_dir "$INSTALL/$TARGET/bin"

# 4) Belt-and-suspenders RPATH on the now-shadowed real binaries. The
# launcher always passes --library-path; this just lets a curious user
# run a `.real/` binary directly under a working host loader.
set_rpath_dir() {
	dir=$1
	rpath=$2
	[ -d "$dir/.real" ] || return 0
	find "$dir/.real" -maxdepth 1 -type f -print | while IFS= read -r f; do
		desc=$(file -b "$f" 2>/dev/null || true)
		case "$desc" in
			*"ELF"*"dynamically linked"*)
				patchelf --set-rpath "$rpath" "$f" 2>/dev/null || true
				;;
		esac
	done
}

echo "post-install.sh: setting RPATHs ..."
set_rpath_dir "$INSTALL/bin"                                       '$ORIGIN/../../runtime'
set_rpath_dir "$INSTALL/libexec/gcc/$TARGET/$GCC_VER"              '$ORIGIN/../../../../../runtime'
set_rpath_dir "$INSTALL/libexec/gcc/$TARGET/$GCC_VER/install-tools" '$ORIGIN/../../../../../../runtime'
set_rpath_dir "$INSTALL/$TARGET/bin"                               '$ORIGIN/../../../runtime'

# 5) liblto_plugin.so: RPATH it to runtime/, and add a bfd-plugins
# symlink so ld.bfd autoloads it without an explicit --plugin.
PLUGIN=$INSTALL/libexec/gcc/$TARGET/$GCC_VER/liblto_plugin.so
if [ -f "$PLUGIN" ] && file -b "$PLUGIN" 2>/dev/null | grep -q 'shared object'; then
	echo "post-install.sh: configuring liblto_plugin.so ..."
	patchelf --set-rpath '$ORIGIN/../../../../runtime' "$PLUGIN"
	mkdir -p "$INSTALL/lib/bfd-plugins"
	ln -sf "../../libexec/gcc/$TARGET/$GCC_VER/liblto_plugin.so" \
		"$INSTALL/lib/bfd-plugins/liblto_plugin.so"
else
	echo "post-install.sh: WARNING: liblto_plugin.so not present at $PLUGIN" >&2
	echo "post-install.sh:          -fuse-linker-plugin will not work." >&2
fi

rm -f "$WRAPPER_BIN"

cat <<EOF
post-install.sh: done.
  install     = $INSTALL
  target      = $TARGET
  gcc         = $GCC_VER
  loader      = $INSTALL/runtime/libc.so
EOF
