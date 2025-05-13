FROM alpine

# Extremely basic dockerfile for dev purposes.

RUN apk add --no-cache\
  git make curl tar perl meson ninja unzip\
  xz build-base flex bison ncurses rsync\
  qemu-x86_64\
  qemu-ppc64\
  qemu-ppc64le\
  qemu-s390x\
  qemu-aarch64\
  qemu-mips64

WORKDIR /workspace
