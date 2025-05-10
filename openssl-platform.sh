#!/bin/sh

# Stupid script to translate linux cpu architecture names to openssl

case "$1" in
  x86_64|aarch64)
    echo "linux-$1"
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
  *)
    echo ""
    ;;
esac
