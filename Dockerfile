FROM alpine

# Extremely basic dockerfile for dev purposes.

RUN apk add --no-cache\
  git make curl tar perl jimtcl tcl meson ninja unzip go\
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
  qemu-riscv32\
  qemu-mips64

# Alpine v3.24 community tops out at qemu 11.0.3, which still has the i386
# SAHF/cc_op TCG bug (GitLab #3537, fixed in 11.0.4 / 11.1). qemu-user
# packages are static-pie musl binaries with no shared-lib deps, so
# pulling 11.1.1 from edge does not rebuild the world.
# See .agents/skills/staticpy-traps/references/I386_QEMU_SAHF.md
RUN apk add --no-cache --upgrade \
  --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community \
  qemu-x86_64=11.1.1-r0 qemu-ppc64=11.1.1-r0 qemu-ppc64le=11.1.1-r0 \
  qemu-s390x=11.1.1-r0 qemu-aarch64=11.1.1-r0 qemu-arm=11.1.1-r0 \
  qemu-armeb=11.1.1-r0 qemu-i386=11.1.1-r0 qemu-riscv64=11.1.1-r0 \
  qemu-riscv32=11.1.1-r0 qemu-mips64=11.1.1-r0

# Host Ubuntu qemu-user-binfmt registers /usr/libexec/qemu-binfmt/<arch>-binfmt-P
# in the machine-wide binfmt table. Alpine does not ship that path, so a
# target python re-exec (assert_python_ok / CPython's suite) is ENOENT.
# Point the Ubuntu names at our qemu-user binaries. qemu 9.1+ is required
# for /proc/self/stat num_threads (CPython's os.fork warning).
RUN mkdir -p /usr/libexec/qemu-binfmt \
 && for a in x86_64 i386 aarch64 arm armeb ppc64 ppc64le s390x riscv64 riscv32 mips64; do \
      [ -x /usr/bin/qemu-$a ] && ln -sfn /usr/bin/qemu-$a /usr/libexec/qemu-binfmt/${a}-binfmt-P; \
    done

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

# Hermetic PATH is toolchain/bin + dirname(busybox). Alpine leaves most applets
# in /usr/bin only, so configure dies on `expr: not found` and then exhausts
# the default 1024 nofile retrying. See staticpy-traps (hermetic PATH / Alpine).
RUN /bin/busybox --install -s /bin \
 && ln -sf /usr/bin/perl /bin/perl \
 && ln -sf /usr/bin/jimsh /bin/jimsh \
 && ln -sf /usr/bin/tclsh /bin/tclsh \
 && ln -sf /usr/bin/make /bin/make \
 && ln -sf /usr/bin/patch /bin/patch

WORKDIR /workspace
