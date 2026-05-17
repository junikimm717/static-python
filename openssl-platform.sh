#!/bin/sh

# Stupid script to translate linux cpu architecture names to openssl

case "$1" in
  x86_64|aarch64)
    echo "linux-$1"
    ;;
  arm)
    echo "linux-armv4"
    ;;
  powerpc64)
    echo "linux-ppc64"
    ;;
  powerpc64le)
    echo "linux-ppc64le"
    ;;
  mips64|riscv64|s390x|sparcv9)
    echo "linux64-$1"
    ;;
  riscv32)
    # OpenSSL only ships linux32-riscv32 (no linux64-riscv32).
    echo "linux32-$1"
    ;;
  i386)
    # i386 GCC is a true 32-bit toolchain; linux-x86 is the right config.
    # Without an explicit target OpenSSL guesses linux-x32 (x86_64 ILP32 ABI)
    # which forces -mx32 and breaks 32-bit-only GCC.
    echo "linux-x86"
    ;;
  *)
    echo ""
    ;;
esac
