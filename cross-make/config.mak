TARGET=
OUTPUT=

# Override musl-cross-make's stale upstream GCC default (9.4.0). Everything
# else (binutils 2.44, musl 1.2.5, gmp 6.3.0, mpc 1.3.1, mpfr 4.2.2, linux
# 5.15.184) is already current as of musl-cross-make master, so no pin needed.
# Upstream ships patches for gcc 15.1.0 in patches/gcc-15.1.0/, which is what
# we ride; bumping past that would require either upstream movement or a
# locally-maintained patch series.
GCC_VER = 15.1.0

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
