TARGET=
OUTPUT=

# Override musl-cross-make's upstream default. Patches for this version
# ship in patches/gcc-$(GCC_VER)/.
GCC_VER = 15.1.0

# Host gcc/binutils are dynamically linked so ld.bfd can dlopen
# liblto_plugin.so (static musl cannot dlopen). Relocatability is restored
# by cross-make/post-install.sh, which shadows every host binary with a
# static-musl launcher that exec's a bundled musl loader. See wrapper.c
# and post-install.sh.
#
# `-static-libgcc -static-libstdc++` keeps gcc's own runtime inside each
# host binary so the only runtime .so dep is the bundled libc.
COMMON_CONFIG += CC="gcc" CXX="g++" FC="gfortran"
COMMON_CONFIG += CFLAGS="-O2 -pipe"
COMMON_CONFIG += CXXFLAGS="-O2 -pipe"
COMMON_CONFIG += LDFLAGS="-Wl,-O1 -Wl,--as-needed -static-libgcc -static-libstdc++"
COMMON_CONFIG += --disable-nls

GCC_CONFIG += --enable-default-pie --enable-static-pie --disable-cet
GCC_CONFIG += --enable-libatomic
GCC_CONFIG += --disable-nls
GCC_CONFIG += --disable-libquadmath --disable-decimal-float
GCC_CONFIG += --disable-fixed-point
GCC_CONFIG += --enable-lto
GCC_CONFIG += --enable-linker-build-id
# Do NOT add `--disable-shared`: it suppresses liblto_plugin.so, and gcc's
# driver then refuses `-fuse-linker-plugin` outright. Target libgcc.so /
# libstdc++.so also get built but our consumers link with -static, so they
# don't end up in the final binary.

BINUTILS_CONFIG += --enable-plugins --disable-nls
BINUTILS_CONFIG += --enable-gold=yes
