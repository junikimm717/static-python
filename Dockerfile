FROM alpine

# Extremely basic dockerfile for dev purposes.

RUN apk add --no-cache\
  git make curl tar perl meson ninja unzip go\
  xz build-base flex bison ncurses rsync patchelf\
  openssl-dev zlib-dev sqlite-dev libffi-dev bzip2-dev xz-dev\
  ncurses-dev readline-dev util-linux-dev expat-dev linux-headers\
  openrc qemu-openrc\
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

# staticpy execs qemu by path, so verification runs without binfmt. The test
# suite is what needs it: a target python re-execing itself is an ELF the
# kernel has to know how to start.
#
# binfmt_misc is one global kernel table, not a namespaced one, so this
# registers the interpreters for the whole host and needs a privileged
# container to reach a writable /proc/sys. The runscript wants openrc state
# that nothing has created in a container.
RUN printf '%s\n' \
  '#!/bin/sh' \
  'mkdir -p /run/openrc && touch /run/openrc/softlevel' \
  'exec /etc/init.d/qemu-binfmt "${1:-start}"' \
  > /usr/local/bin/binfmt && chmod +x /usr/local/bin/binfmt

WORKDIR /workspace
