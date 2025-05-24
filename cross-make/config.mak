TARGET=
OUTPUT=

COMMON_CONFIG += CFLAGS="-O2 -flto"
COMMON_CONFIG += CXXFLAGS="-O2 -flto"
COMMON_CONFIG += LDFLAGS="-flto -s"
COMMON_CONFIG += --disable-nls

GCC_CONFIG += --enable-default-pie
GCC_CONFIG += --enable-libatomic
GCC_CONFIG += --disable-nls
GCC_CONFIG += --disable-libquadmath --disable-decimal-float
GCC_CONFIG += --disable-fixed-point
GCC_CONFIG += --enable-lto
BINUTILS_CONFIG += --enable-plugins --disable-nls
