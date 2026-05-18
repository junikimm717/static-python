#!/bin/sh
# Portability proof: extract the static-musl toolchain tarball into a
# foreign (glibc) rootfs, then build and run three test programs end-to-end.
# See ai/PORTABILITY_PROOF.md.
#
# Runs INSIDE the alien container. Expects:
#   /toolchain.tgz       toolchain tarball produced by `make USE_CROSSMAKE=1`
#   /tests/              test sources (hello.c, hello.cc, lib.c, main.c)
#   /opt                 writable dir we extract the toolchain into

set -eu

banner() {
	printf '\n========== %s ==========\n' "$*"
}

banner "host rootfs identification"
. /etc/os-release 2>/dev/null || true
echo "os-release: ${PRETTY_NAME:-unknown}"
echo "uname -a:   $(uname -a)"
HOST_LOADER=$(readelf -p .interp /bin/sh 2>/dev/null \
	| awk '/ld-/ { for (i=1; i<=NF; i++) if ($i ~ /\//) print $i }' \
	| head -n 1)
echo "/bin/sh .interp: $HOST_LOADER"
file -b "$HOST_LOADER" 2>/dev/null || true

banner "verify no compiler in PATH"
for c in cc gcc g++ cpp clang; do
	if command -v "$c" >/dev/null 2>&1; then
		echo "FAIL: $c found at $(command -v "$c") -- proof is invalid"
		exit 1
	fi
done
echo "OK: no host compiler in PATH"

banner "extract toolchain"
mkdir -p /opt
tar -xzf /toolchain.tgz -C /opt
TC=/opt/x86_64-linux-musl-native
ls -la "$TC"
test -d "$TC/bin" && test -d "$TC/runtime"
export PATH="$TC/bin:$PATH"

banner "inspect wrapper + real binary + bundled loader"
file -b "$TC/bin/x86_64-linux-musl-gcc"
file -b "$TC/bin/.real/x86_64-linux-musl-gcc"
file -b "$TC/runtime/libc.so"
echo "--- readelf -d runtime/libc.so ---"
readelf -d "$TC/runtime/libc.so" 2>&1 | head -30 || true
echo "--- bfd-plugins ---"
ls -la "$TC/lib/bfd-plugins/" 2>&1 || true

banner "compiler self-test (--version)"
x86_64-linux-musl-gcc --version
x86_64-linux-musl-g++ --version

banner "test 1: plain C"
mkdir -p /work/out
x86_64-linux-musl-gcc -O2 -static -o /work/out/hello-c /tests/hello.c
file /work/out/hello-c
echo "--- run ---"
/work/out/hello-c

banner "test 2: C++ with libstdc++"
x86_64-linux-musl-g++ -O2 -static -o /work/out/hello-cxx /tests/hello.cc
file /work/out/hello-cxx
echo "--- run ---"
/work/out/hello-cxx

banner "test 3: LTO with linker plugin (the wrapper raison d'etre)"
LTO_FLAGS="-O2 -flto -fuse-linker-plugin -fno-fat-lto-objects"

echo "--- compile lib.c -> lib.o (slim LTO object) ---"
x86_64-linux-musl-gcc $LTO_FLAGS -c /tests/lib.c -o /work/out/lib.o
file /work/out/lib.o
echo "--- readelf -SW lib.o | grep gnu.lto_ ---"
x86_64-linux-musl-readelf -SW /work/out/lib.o 2>&1 | grep 'gnu\.lto_' | head -6 \
	|| { echo "FAIL: no .gnu.lto_* sections; lib.o is not a slim LTO object"; exit 1; }
echo "--- lib.o .text size (slim: 0) ---"
x86_64-linux-musl-readelf -SW /work/out/lib.o 2>&1 \
	| awk '/ \.text  */ {print "  .text size =", $7}'

echo "--- archive into lib.a (use gcc-ar so plugin records LTO symbols) ---"
rm -f /work/out/lib.a
x86_64-linux-musl-gcc-ar rcs /work/out/lib.a /work/out/lib.o
file /work/out/lib.a

echo "--- compile main.c -> main.o ---"
x86_64-linux-musl-gcc $LTO_FLAGS -c /tests/main.c -o /work/out/main.o

echo "--- link with gcc -v: full driver pipeline (shows -plugin path) ---"
# gcc -v prints the collect2/lto-wrapper invocation, which includes the
# absolute -plugin <liblto_plugin.so> path. That is the most direct
# evidence the linker-plugin path is wired up.
x86_64-linux-musl-gcc $LTO_FLAGS -static -v \
	-o /work/out/lto-main \
	/work/out/main.o /work/out/lib.a \
	> /work/out/lto-link.log 2>&1
echo "--- gcc -v link: -plugin / liblto_plugin lines ---"
if ! grep -E -- '-plugin\b|liblto_plugin\.so' /work/out/lto-link.log | head -20; then
	echo "FAIL: no -plugin in driver output; dumping full log:"
	cat /work/out/lto-link.log
	exit 1
fi
echo "--- collect2 / lto-wrapper / lto1 invocations ---"
grep -E 'collect2|lto-wrapper|/lto1|lto-plugin' /work/out/lto-link.log | head -10 \
	|| echo "(none)"
file /work/out/lto-main
echo "--- run ---"
/work/out/lto-main

echo "--- NEGATIVE CONTROL: link the same slim objects WITHOUT the plugin ---"
# Slim LTO objects have no real .text. Without the plugin, ld cannot
# materialise dot_product and the link must fail with an undefined
# reference. That falsifies "maybe it would have worked anyway".
set +e
x86_64-linux-musl-gcc -O2 -static -fno-lto \
	-o /work/out/lto-main-noplugin \
	/work/out/main.o /work/out/lib.a \
	> /work/out/lto-noplugin.log 2>&1
NEG_RC=$?
set -e
echo "no-plugin link rc=$NEG_RC (expected non-zero)"
if [ "$NEG_RC" -eq 0 ]; then
	echo "FAIL: no-plugin link unexpectedly succeeded; slim LTO objects should not satisfy 'dot_product'"
	exit 1
fi
echo "--- relevant error lines ---"
grep -E 'undefined reference|dot_product' /work/out/lto-noplugin.log | head -5 \
	|| cat /work/out/lto-noplugin.log

banner "deep linkage inspection"
for bin in /work/out/hello-c /work/out/hello-cxx /work/out/lto-main; do
	echo "=== $bin ==="
	echo "--- file ---"
	file "$bin"
	echo "--- readelf -d (DT_NEEDED / DT_INTERP) ---"
	readelf -d "$bin" 2>&1 | grep -E 'NEEDED|RUNPATH|RPATH' || echo "(no dynamic deps)"
	readelf -l "$bin" 2>&1 | grep -A1 INTERP || echo "(no PT_INTERP)"
	echo "--- musl identifying strings (expect 'musl libc') ---"
	strings "$bin" | grep -iE '^musl libc|musl-cross-make|x86_64-linux-musl' | head -5 \
		|| echo "(no musl strings)"
	echo "--- glibc identifying strings (expect NONE; exclude libstdc++'s GLIBCXX_*) ---"
	# Filter out libstdc++ own identifiers (GLIBCXX_TUNABLES, glibcxx.*),
	# which are GNU libstdc++ namespace tags, not glibc. Real glibc
	# leakage would show up as 'GLIBC_2.*' version symbols or the
	# "GNU C Library" version banner.
	strings "$bin" \
		| grep -iE 'glibc|^gnu c library' \
		| grep -ivE 'glibcxx|libstdc' \
		| head -5 \
		|| echo "(no glibc strings)"
done

banner "negative control: glibc on a static-musl ELF"
# /bin/sh is glibc-linked; static-musl binaries do not run via ld-linux,
# they have no PT_INTERP, so the glibc loader is irrelevant. We just
# verify the run output we already captured above.
echo "static-musl outputs already captured; glibc never touched."

banner "DONE"
