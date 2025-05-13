#!/bin/sh

if test "$TCTYPE" = "native"; then
  echo "https://musl.cc/"
  exit 0
fi

case "$NATIVE_ARCH" in
  x86_64)
    if test "$ARCH" = "riscv64"; then
      echo "https://dev.mit.junic.kim/cross/x86_64/"
    else
      echo "https://musl.cc/"
    fi
    ;;
  aarch64)
    echo "https://dev.mit.junic.kim/cross/aarch64/"
    ;;
  *)
    exit 1
esac
