TARGET=
OUTPUT=

STAT = -static --static

COMMON_CONFIG += CC="gcc ${STAT}" CXX="g++ ${STAT}" FC="gfortran ${STAT}"
COMMON_CONFIG += CFLAGS="-O2 -flto -s ${STAT}"
COMMON_CONFIG += CXXFLAGS="-O2 -flto -s ${STAT}"
COMMON_CONFIG += LDFLAGS="-flto -static -no-pie -flto -s --static"
COMMON_CONFIG += --disable-nls
GCC_CONFIG += --enable-default-pie --enable-static-pie --disable-cet
GCC_CONFIG += --enable-libatomic --disable-shared
GCC_CONFIG += --disable-nls
GCC_CONFIG += --disable-libquadmath --disable-decimal-float
GCC_CONFIG += --disable-fixed-point
GCC_CONFIG += --enable-lto

BINUTILS_CONFIG += --enable-plugins --disable-nls
BINUTILS_CONFIG += --enable-gold=yes
