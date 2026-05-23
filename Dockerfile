FROM alpine

# Extremely basic dockerfile for dev purposes.

RUN apk add --no-cache\
  git make curl tar perl meson ninja unzip\
  xz build-base flex bison ncurses rsync patchelf\
  openssl-dev zlib-dev sqlite-dev libffi-dev bzip2-dev xz-dev\
  ncurses-dev readline-dev util-linux-dev expat-dev linux-headers\
  qemu-x86_64\
  qemu-ppc64\
  qemu-ppc64le\
  qemu-s390x\
  qemu-aarch64\
  qemu-arm\
  qemu-armeb\
  qemu-i386\
  qemu-riscv64\
  qemu-mips64

WORKDIR /workspace
