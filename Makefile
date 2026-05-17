ROOT_DIR := $(dir $(realpath $(lastword $(MAKEFILE_LIST))))
CROSSMAKE := master
OPENSSL := 3.5.6
LIBFFI := 3.5.2
LIBLZMA := 5.8.3
ZLIB := 1.3.2
READLINE := 8.3
NCURSES := 6.5
SQLITE := 3510200
SQLITE_YEAR := 2026
BZIP2 := 1.0.8
UTILLINUX := 2.41
PYTHON := 3.13.13
LINUX_VER := 5.15.184

SPLIT := $(subst ., ,$(PYTHON))
PYTHONV := $(word 1, $(SPLIT)).$(word 2, $(SPLIT))

print-%:
	@echo $($*)
.PHONY: print-%

# External (third-party) source tarballs. Each is sha256-verified at download
# time against hashes/<basename>.sha256. Toolchain prebuilts from
# dev.mit.junic.kim are intentionally excluded (those are produced by us).
EXTERNAL_TARBALLS := \
	tarballs/musl-cross-make-$(CROSSMAKE).tar.gz \
	tarballs/openssl-$(OPENSSL).tar.gz \
	tarballs/libffi-$(LIBFFI).tar.gz \
	tarballs/xz-$(LIBLZMA).tar.gz \
	tarballs/zlib-$(ZLIB).tar.gz \
	tarballs/ncurses-$(NCURSES).tar.gz \
	tarballs/readline-$(READLINE).tar.gz \
	tarballs/sqlite-src-$(SQLITE).zip \
	tarballs/bzip2-$(BZIP2).tar.gz \
	tarballs/util-linux-$(UTILLINUX).tar.gz \
	tarballs/Python-$(PYTHON).tgz

# Set SKIP_VERIFY=1 to bypass the integrity check (the `update-hashes`
# target does this via target-specific assignment while refreshing hashes).
SKIP_VERIFY ?=

# Recipe snippet: verify $@ against hashes/<basename>.sha256. On mismatch
# (or a missing hash file) the downloaded tarball is deleted so the next
# attempt re-downloads rather than reusing a poisoned file.
VERIFY_SHA256 = if test -z "$(SKIP_VERIFY)"; then \
		if ! test -f hashes/$(@F).sha256; then \
			echo "ERROR: missing hashes/$(@F).sha256 (run 'make update-hashes')" >&2; \
			rm -f $@; exit 1; \
		fi; \
		(cd tarballs && sha256sum -c ../hashes/$(@F).sha256) \
			|| { rm -f $@; exit 1; }; \
	fi

ARCH := $(shell uname -m)
MUSLABI := musl
JOBS := $(shell nproc)

override TARGET = $(ARCH)-linux-$(MUSLABI)
override NATIVE_ARCH := $(shell uname -m)
override NATIVE_TARGET := $(NATIVE_ARCH)-linux-musl

USE_CROSSMAKE := 0

ifeq ($(shell grep '$(TARGET)' ./supported.txt),)
$(error Platform '$(TARGET)' is not supported)
endif

# do a bunch of architecture fiddling.

ifneq ($(ARCH),$(NATIVE_ARCH))
override TCTYPE=cross
$(info Cross-Compiling to $(ARCH) from $(NATIVE_ARCH)...)
# TODO: conditions where you have to force musl cross make
else
override TCTYPE=native
$(info Native Compiling in $(NATIVE_ARCH)...)
endif

# PGO trains against the freshly built interpreter, so it needs a runnable
# native binary. Default on for native, off for cross.
ifeq ($(TCTYPE),native)
USE_PGO ?= 1
else
USE_PGO ?= 0
endif
$(info USE_PGO=$(USE_PGO))

# `-x test_re` skips two locale tests that fail on musl (no non-C byte-level
# case folding) and would abort the PGO build. Same workaround as Alpine apk.
PROFILE_TASK ?= -m test --pgo -x test_re


export TCTYPE
export ARCH
export NATIVE_ARCH
export MUSLABI

# first target should be python3

.PHONY: python3 clean distclean update-hashes

python3: python-static-$(TARGET)/bin/python$(PYTHONV)

clean:
	rm -rf deps-* build-* python-static-*

distclean: clean
	rm -rf tarballs

# Refresh hashes/<basename>.sha256 for every external tarball.
update-hashes: SKIP_VERIFY := 1
update-hashes: $(EXTERNAL_TARBALLS)
	@mkdir -p hashes
	@for t in $(notdir $(EXTERNAL_TARBALLS)); do \
		(cd tarballs && sha256sum "$$t") > "hashes/$$t.sha256"; \
		echo "updated hashes/$$t.sha256"; \
	done

# build steps for musl toolchain.

tarballs/musl-cross-make-master.tar.gz:
	mkdir -p tarballs
	curl -Lf https://github.com/richfelker/musl-cross-make/archive/$(CROSSMAKE).tar.gz -o $@
	@$(VERIFY_SHA256)

deps-$(TARGET)/musl-cross-make-master/.extracted: tarballs/musl-cross-make-$(CROSSMAKE).tar.gz
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	cp -a ./cross-make/hashes/. deps-$(TARGET)/musl-cross-make-$(CROSSMAKE)/hashes/
	cp -a ./cross-make/patches/. deps-$(TARGET)/musl-cross-make-$(CROSSMAKE)/patches/
	ln -sfn $(ROOT_DIR)tarballs deps-$(TARGET)/musl-cross-make-$(CROSSMAKE)/sources
	touch $@

tarballs/$(TARGET)-$(TCTYPE).tgz:
	mkdir -p tarballs
	curl -Lf https://dev.mit.junic.kim/cross/$(NATIVE_ARCH)/$(TARGET)-$(TCTYPE).tgz\
		-o tarballs/$(TARGET)-$(TCTYPE).tgz

.PHONY: crossmake
crossmake: deps-$(TARGET)/$(ARCH)-linux-$(MUSLABI)-$(TCTYPE)/.extracted

ifeq ($(USE_CROSSMAKE),1)
# manually compile the toolchain.
deps-$(TARGET)/$(ARCH)-linux-$(MUSLABI)-$(TCTYPE)/.extracted: deps-$(TARGET)/musl-cross-make-$(CROSSMAKE)/.extracted
	sed\
		-e 's|^TARGET=.*|TARGET=$(ARCH)-linux-$(MUSLABI)|g'\
		-e 's|^OUTPUT=.*|OUTPUT=$(ROOT_DIR)deps-$(TARGET)/$(ARCH)-linux-$(MUSLABI)-$(TCTYPE)|g'\
		./cross-make/config.mak\
		> deps-$(TARGET)/musl-cross-make-$(CROSSMAKE)/config.mak
	sed -i\
		-e 's/\([jJz]x\)vf/\1f/g'\
		-e 's|^LINUX_VER =.*|LINUX_VER = $(LINUX_VER)|g'\
		deps-$(TARGET)/musl-cross-make-$(CROSSMAKE)/Makefile
	# just enable c++ for now, makes it easier for other toolchains.
	# sed -i\
	# 	-e 's/--enable-languages=c,c++/--enable-languages=c/g'\
	# 	-e 's|--enable-libstdcxx-time=rt||g'\
	# 	deps-$(TARGET)/musl-cross-make-$(CROSSMAKE)/litecross/Makefile
	# Unset MAKEFLAGS/MAKEOVERRIDES so our `ARCH=...` command-line override
	# does not leak through MAKEFLAGS into musl's Makefile (which sets ARCH
	# itself in config.mak). Without this, targets like `powerpc64le` break
	# because musl uses arch/powerpc64/ while ARCH=powerpc64le is forced.
	# Use `env -u` (truly unset) rather than `VAR=` (empty string); the
	# empty-string form breaks downstream propagation of MAKEOVERRIDES, so
	# nested invocations like the kernel headers install lose INSTALL_HDR_PATH.
	cd deps-$(TARGET)/musl-cross-make-$(CROSSMAKE) && env -u MAKEFLAGS -u MAKEOVERRIDES make -j$(JOBS)
	cd deps-$(TARGET)/musl-cross-make-$(CROSSMAKE) && env -u MAKEFLAGS -u MAKEOVERRIDES make install
	touch $@
else
deps-$(TARGET)/$(TARGET)-$(TCTYPE)/.extracted: tarballs/$(TARGET)-$(TCTYPE).tgz
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	touch $@
endif

# stupid patcher script that takes a cross-compiled toolchain and gives us info.
.PHONY: patcher
patcher:
	mkdir -p build-$(TARGET)
	./configure-wrapper.sh sh -c '$$CC ./python/patcher.c -static -o ./build-$(TARGET)/patcher'

# compile openssl

tarballs/openssl-$(OPENSSL).tar.gz:
	mkdir -p tarballs
	curl -Lf https://github.com/openssl/openssl/releases/download/openssl-$(OPENSSL)/openssl-$(OPENSSL).tar.gz -o $@
	@$(VERIFY_SHA256)

deps-$(TARGET)/openssl-$(OPENSSL)/.extracted: tarballs/openssl-$(OPENSSL).tar.gz
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	# OpenSSL's Configure auto-disables `static`, `pic`, and `threads`
	# when it sees `-static` in LDFLAGS. We pass `-static` deliberately
	# for the static build, so we strip the entire perl block (anchored
	# by content rather than line number so it survives upstream
	# version bumps). The block looks like:
	#   if (grep { $_ =~ /(?:^|\s)-static(?:\s|$$)/ } @{$$config{LDFLAGS}}) {
	#       disable('static', 'pic', 'threads');
	#   }
	cd deps-$(TARGET)/openssl-$(OPENSSL) && \
		sed -i '/if (grep.*-static.*\$$config{LDFLAGS}/,/^}$$/d' ./Configure
	touch $@

build-$(TARGET)/include/openssl/ssl.h: deps-$(TARGET)/$(TARGET)-$(TCTYPE)/.extracted deps-$(TARGET)/openssl-$(OPENSSL)/.extracted
	mkdir -p build-$(TARGET)
	cd deps-$(TARGET)/openssl-$(OPENSSL) &&\
		../../configure-wrapper.sh \
		./Configure $(shell ./openssl-platform.sh $(ARCH))\
			no-shared no-dso no-asm no-engine no-tests no-ssl3 no-comp no-idea no-rc5\
			no-ec2m no-weak-ssl-ciphers no-apps\
			--prefix=$(ROOT_DIR)build-$(TARGET) --openssldir=$(ROOT_DIR)build-$(TARGET)
	cd deps-$(TARGET)/openssl-$(OPENSSL) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(TARGET)/openssl-$(OPENSSL) && ../../configure-wrapper.sh make install_sw

openssl: build-$(TARGET)/include/openssl/ssl.h
.PHONY: openssl

# compile libffi

tarballs/libffi-$(LIBFFI).tar.gz:
	mkdir -p tarballs
	curl -Lf https://github.com/libffi/libffi/releases/download/v$(LIBFFI)/libffi-$(LIBFFI).tar.gz -o $@
	@$(VERIFY_SHA256)

deps-$(TARGET)/libffi-$(LIBFFI)/.extracted: tarballs/libffi-$(LIBFFI).tar.gz
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	touch $@

build-$(TARGET)/lib/libffi.a: deps-$(TARGET)/$(ARCH)-linux-$(MUSLABI)-$(TCTYPE)/.extracted deps-$(TARGET)/libffi-$(LIBFFI)/.extracted
	mkdir -p build-$(TARGET)
	cd deps-$(TARGET)/libffi-$(LIBFFI) &&\
		../../configure-wrapper.sh ./configure \
			--prefix=$(ROOT_DIR)build-$(TARGET) \
			--host=$(ARCH)-linux-$(MUSLABI)
			--exec-prefix=$(ROOT_DIR)build-$(TARGET) \
			--enable-static --disable-shared
	cd deps-$(TARGET)/libffi-$(LIBFFI) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(TARGET)/libffi-$(LIBFFI) && ../../configure-wrapper.sh make install

libffi: build-$(TARGET)/lib/libffi.a
.PHONY: libffi

# compile libxz

tarballs/xz-$(LIBLZMA).tar.gz:
	mkdir -p tarballs
	curl -Lf https://github.com/tukaani-project/xz/releases/download/v$(LIBLZMA)/xz-$(LIBLZMA).tar.gz -o $@
	@$(VERIFY_SHA256)

deps-$(TARGET)/xz-$(LIBLZMA)/.extracted: tarballs/xz-$(LIBLZMA).tar.gz
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	touch $@

build-$(TARGET)/lib/liblzma.a: deps-$(TARGET)/xz-$(LIBLZMA)/.extracted deps-$(TARGET)/$(ARCH)-linux-$(MUSLABI)-$(TCTYPE)/.extracted
	mkdir -p build-$(TARGET)
	cd deps-$(TARGET)/xz-$(LIBLZMA) &&\
		ARCH="$(ARCH)"\
		TCTYPE="$(TCTYPE)"\
		../../configure-wrapper.sh ./configure \
			--prefix=$(ROOT_DIR)build-$(TARGET) \
			--host=$(ARCH)-linux-$(MUSLABI)\
			--exec-prefix=$(ROOT_DIR)build-$(TARGET)\
			--enable-static --disable-shared
	cd deps-$(TARGET)/xz-$(LIBLZMA) && ../../configure-wrapper.sh make V=1 -j$(JOBS)
	cd deps-$(TARGET)/xz-$(LIBLZMA) && ../../configure-wrapper.sh make install

liblzma: build-$(TARGET)/lib/liblzma.a
.PHONY: liblzma

# compile zlib

tarballs/zlib-$(ZLIB).tar.gz:
	mkdir -p tarballs
	curl -Lf http://zlib.net/zlib-$(ZLIB).tar.gz -o $@
	@$(VERIFY_SHA256)

deps-$(TARGET)/zlib-$(ZLIB)/.extracted: tarballs/zlib-$(ZLIB).tar.gz
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	touch $@

build-$(TARGET)/lib/libz.a: deps-$(TARGET)/zlib-$(ZLIB)/.extracted deps-$(TARGET)/$(ARCH)-linux-$(MUSLABI)-$(TCTYPE)/.extracted
	mkdir -p build-$(TARGET)
	# --disable-crcvx: zlib 1.3.2 detects s390x VX but its Makefile.in -> Makefile
	# sed substitution has no rule for VGFMAFLAG, so crc32_vx.c is compiled
	# without -mzarch/-march=z13 and the VX builtins fail. Skipping the VX
	# CRC32 contrib avoids the upstream bug on s390x and is a no-op elsewhere.
	cd deps-$(TARGET)/zlib-$(ZLIB) &&\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build-$(TARGET) --eprefix=$(ROOT_DIR)build-$(TARGET) --static --disable-crcvx
	cd deps-$(TARGET)/zlib-$(ZLIB) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(TARGET)/zlib-$(ZLIB) && ../../configure-wrapper.sh make install

zlib: build-$(TARGET)/lib/libz.a
.PHONY: zlib

# compile ncurses

tarballs/ncurses-$(NCURSES).tar.gz:
	mkdir -p tarballs
	curl -Lf https://ftp.gnu.org/gnu/ncurses/ncurses-$(NCURSES).tar.gz -o $@
	@$(VERIFY_SHA256)

deps-$(TARGET)/ncurses-$(NCURSES)/.extracted: tarballs/ncurses-$(NCURSES).tar.gz
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	touch $@

build-$(TARGET)/lib/libncursesw.a: deps-$(TARGET)/ncurses-$(NCURSES)/.extracted deps-$(TARGET)/$(ARCH)-linux-$(MUSLABI)-$(TCTYPE)/.extracted
	cd deps-$(TARGET)/ncurses-$(NCURSES) &&\
		../../configure-wrapper.sh ./configure --without-cxx --without-cxx-binding\
			--without-shared --prefix=$(ROOT_DIR)build-$(TARGET)\
			--exec-prefix=$(ROOT_DIR)build-$(TARGET) --enable-static\
			--host=$(ARCH)-linux-$(MUSLABI)\
			--without-ada \
			--without-manpages \
			--without-tests \
			--without-progs\
			--enable-termcap\
			--disable-shared
	cd deps-$(TARGET)/ncurses-$(NCURSES) && make -j$(JOBS)
	cd deps-$(TARGET)/ncurses-$(NCURSES) &&\
		TIC_PATH=$(shell command -v tic) make install

ncurses: build-$(TARGET)/lib/libncursesw.a
.PHONY: ncurses

# compile readline

tarballs/readline-$(READLINE).tar.gz:
	mkdir -p tarballs
	curl -Lf https://ftp.gnu.org/gnu/readline/readline-$(READLINE).tar.gz -o $@
	@$(VERIFY_SHA256)

deps-$(TARGET)/readline-$(READLINE)/.extracted: tarballs/readline-$(READLINE).tar.gz 
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	touch $@

build-$(TARGET)/lib/libreadline.a: deps-$(TARGET)/$(ARCH)-linux-$(MUSLABI)-$(TCTYPE)/.extracted deps-$(TARGET)/readline-$(READLINE)/.extracted
	mkdir -p build-$(TARGET)
	cd deps-$(TARGET)/readline-$(READLINE) &&\
		../../configure-wrapper.sh ./configure \
			--prefix=$(ROOT_DIR)build-$(TARGET)\
			--exec-prefix=$(ROOT_DIR)build-$(TARGET)\
			--with-curses\
			--host=$(ARCH)-linux-$(MUSLABI)\
			--disable-install-examples\
			--enable-static\
			--disable-shared
	cd deps-$(TARGET)/readline-$(READLINE) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(TARGET)/readline-$(READLINE) && ../../configure-wrapper.sh make install

readline: build-$(TARGET)/lib/libreadline.a
.PHONY: readline

# compile steps for libsqlite

tarballs/sqlite-src-$(SQLITE).zip:
	mkdir -p tarballs
	curl -Lf https://www.sqlite.org/$(SQLITE_YEAR)/sqlite-src-$(SQLITE).zip -o $@
	@$(VERIFY_SHA256)

deps-$(TARGET)/sqlite-src-$(SQLITE)/.extracted: tarballs/sqlite-src-$(SQLITE).zip
	mkdir -p deps-$(TARGET)
	cd deps-$(TARGET) && unzip -o ../tarballs/sqlite-src-$(SQLITE).zip
	touch $@

build-$(TARGET)/lib/libsqlite3.a: deps-$(TARGET)/$(ARCH)-linux-$(MUSLABI)-$(TCTYPE)/.extracted build-$(TARGET)/lib/libreadline.a deps-$(TARGET)/sqlite-src-$(SQLITE)/.extracted
	mkdir -p build-$(TARGET)
	cd deps-$(TARGET)/sqlite-src-$(SQLITE) &&\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build-$(TARGET)\
			--exec-prefix=$(ROOT_DIR)build-$(TARGET)\
			--host=$(ARCH)-linux-$(MUSLABI)\
			--enable-static --disable-shared
	cd deps-$(TARGET)/sqlite-src-$(SQLITE) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(TARGET)/sqlite-src-$(SQLITE) && ../../configure-wrapper.sh make install

libsqlite: build-$(TARGET)/lib/libsqlite3.a
.PHONY: libsqlite

# compile bzip2

tarballs/bzip2-$(BZIP2).tar.gz:
	mkdir -p tarballs
	curl -Lf https://sourceware.org/pub/bzip2/bzip2-$(BZIP2).tar.gz -o $@
	@$(VERIFY_SHA256)

deps-$(TARGET)/bzip2-$(BZIP2)/.extracted: tarballs/bzip2-$(BZIP2).tar.gz
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	sed -i \
		-e 's|^CC=.*||' \
		-e 's|^AR=.*||' \
		-e 's|^RANLIB=.*||' \
		-e 's|^CFLAGS=.*||' \
		-e 's|^LDFLAGS=.*||' \
		-e 's|^PREFIX=.*|PREFIX=$(ROOT_DIR)build-$(TARGET)|' \
		deps-$(TARGET)/bzip2-$(BZIP2)/Makefile
	touch $@

build-$(TARGET)/lib/libbz2.a: deps-$(TARGET)/$(ARCH)-linux-$(MUSLABI)-$(TCTYPE)/.extracted deps-$(TARGET)/bzip2-$(BZIP2)/.extracted
	mkdir -p build-$(TARGET)
	cd deps-$(TARGET)/bzip2-$(BZIP2) && ../../configure-wrapper.sh make libbz2.a bzip2 bzip2recover -j$(JOBS)
	cd deps-$(TARGET)/bzip2-$(BZIP2) && ../../configure-wrapper.sh make install

libbz2: build-$(TARGET)/lib/libbz2.a
.PHONY: libbz2

# compile libuuid

tarballs/util-linux-$(UTILLINUX).tar.gz:
	mkdir -p tarballs
	curl -Lf https://github.com/util-linux/util-linux/archive/refs/tags/v$(UTILLINUX).tar.gz -o $@
	@$(VERIFY_SHA256)

deps-$(TARGET)/util-linux-$(UTILLINUX)/.extracted: tarballs/util-linux-$(UTILLINUX).tar.gz
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	touch $@

build-$(TARGET)/lib/libuuid.a: deps-$(TARGET)/util-linux-$(UTILLINUX)/.extracted deps-$(TARGET)/$(ARCH)-linux-$(MUSLABI)-$(TCTYPE)/.extracted
	cd deps-$(TARGET)/util-linux-$(UTILLINUX) && \
		printf "[properties]\nneeds_exe_wrapper = true\n" > ./cross.ini
	cd deps-$(TARGET)/util-linux-$(UTILLINUX) && \
		grep -E "option(.*),[[:space:]]*type[[:space:]]*:[[:space:]]*'feature'" meson_options.txt \
			| grep -vE 'build-libuuid' \
			| sed -E "s/.*option\(['\"]([a-zA-Z0-9_-]+)['\"].*/\1/" \
			| awk '{print "-D" $$1 "=disabled"}'\
			> ./build-args.txt &&\
		echo "--prefix=$(ROOT_DIR)build-$(TARGET) --default-library=static --prefer-static --buildtype=release --backend=ninja --cross-file cross.ini"\
			>> ./build-args.txt
	cd deps-$(TARGET)/util-linux-$(UTILLINUX) && \
		MESON=1\
		../../configure-wrapper.sh meson setup build $$(cat ./build-args.txt)
	ninja -C deps-$(TARGET)/util-linux-$(UTILLINUX)/build
	ninja -C deps-$(TARGET)/util-linux-$(UTILLINUX)/build install
	
libuuid: build-$(TARGET)/lib/libuuid.a
.PHONY: libuuid

# compile python3

tarballs/Python-$(PYTHON).tgz:
	mkdir -p tarballs
	curl -Lf https://www.python.org/ftp/python/$(PYTHON)/Python-$(PYTHON).tgz -o $@
	@$(VERIFY_SHA256)

deps-$(TARGET)/Python-$(PYTHON)/Modules/Setup.local: tarballs/Python-$(PYTHON).tgz
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	# monkey patched code for static symbols in ctypes
	cp -r ./python/staticapi deps-$(TARGET)/Python-$(PYTHON)/Modules/staticapi
	# Patch ctypes/__init__.py using content anchors (resilient to upstream
	# refactors of CDLL._load_library / line-number drift across patch releases).
	# 1. Inject StaticCDLL definitions before the CDLL class.
	# 2. Override `pythonapi = PyDLL(None)` with the StaticCDLL-backed proxy.
	# 3. Neuter the dlopen import (returns 0) so CDLL/PyDLL never call libdl.
	sed -i \
		-e "/^################################################################$$/r ./python/ctypes_patch_1.py"\
		-e "/^    pythonapi = PyDLL(None)$$/r ./python/ctypes_patch_2.py"\
		-e "s|^    from _ctypes import dlopen as _dlopen$$|    _dlopen = lambda *a, **kw: 0|"\
		./deps-$(TARGET)/Python-$(PYTHON)/Lib/ctypes/__init__.py
	cp -r ./python/Setup deps-$(TARGET)/Python-$(PYTHON)/Modules/Setup.local

# you must distinguish between cross and native compilation.
# apparently you need the native interpreter for cross compilation sob

PYTHON_DEPS = deps-$(TARGET)/$(ARCH)-linux-$(MUSLABI)-$(TCTYPE)/.extracted\
		openssl libffi libuuid libsqlite liblzma readline zlib libbz2 ncurses \
		deps-$(TARGET)/Python-$(PYTHON)/Modules/Setup.local

ifeq ($(TCTYPE),cross)
# cross-compiling case; use python-specific flags.
override NATIVE_PATH := python-static-$(NATIVE_TARGET)/bin/python$(PYTHONV)
.PHONY: check_native
check_native:
	test -f $(NATIVE_PATH) -a -d deps-$(NATIVE_TARGET)/Python-$(PYTHON)
python-static-$(TARGET)/bin/python$(PYTHONV): check_native $(PYTHON_DEPS)
	mkdir -p deps-$(TARGET)/Python-$(PYTHON)/build
	cp ./python/config.status deps-$(TARGET)/Python-$(PYTHON)/config.status\
		&& chmod +x deps-$(TARGET)/Python-$(PYTHON)/config.status
	sed\
		-e '/^CC=/d' \
		-e '/^AR=/d' \
		-e 's|$(NATIVE_ARCH)|$(ARCH)|g'\
		-e 's|^BUILDPYTHON=.*|BUILDPYTHON=build/python|g'\
		-e 's|^PYTHON_FOR_BUILD=.*|PYTHON_FOR_BUILD=$(ROOT_DIR)$(NATIVE_PATH) -E|g'\
		-e 's|^PYTHON_FOR_BUILD_DEPS=.*|PYTHON_FOR_BUILD_DEPS=|g'\
		-e 's|^PYTHON_FOR_FREEZE=.*|PYTHON_FOR_FREEZE=$(ROOT_DIR)$(NATIVE_PATH)|g'\
		-e 's|^FREEZE_MODULE_BOOTSTRAP=.*|FREEZE_MODULE_BOOTSTRAP=$(ROOT_DIR)deps-$(NATIVE_TARGET)/Python-$(PYTHON)/Programs/_freeze_module|g'\
		-e 's|^FREEZE_MODULE_BOOTSTRAP_DEPS=.*|FREEZE_MODULE_BOOTSTRAP_DEPS=|g'\
		-e '/^[[:space:]]*\$$(MAKE) -f Makefile\.pre.*Makefile/d'\
		deps-$(NATIVE_TARGET)/Python-$(PYTHON)/Makefile\
		> deps-$(TARGET)/Python-$(PYTHON)/Makefile
	sed\
		-e '/^CC=/d' \
		-e '/^AR=/d' \
		-e 's|$(NATIVE_ARCH)|$(ARCH)|g'\
		-e 's|^BUILDPYTHON=.*|BUILDPYTHON=build/python|g'\
		-e 's|^PYTHON_FOR_BUILD=.*|PYTHON_FOR_BUILD=$(ROOT_DIR)$(NATIVE_PATH) -E|g'\
		-e 's|^PYTHON_FOR_BUILD_DEPS=.*|PYTHON_FOR_BUILD_DEPS=|g'\
		-e 's|^PYTHON_FOR_FREEZE=.*|PYTHON_FOR_FREEZE=$(ROOT_DIR)$(NATIVE_PATH)|g'\
		-e 's|^FREEZE_MODULE_BOOTSTRAP=.*|FREEZE_MODULE_BOOTSTRAP=$(ROOT_DIR)deps-$(NATIVE_TARGET)/Python-$(PYTHON)/Programs/_freeze_module|g'\
		-e 's|^FREEZE_MODULE_BOOTSTRAP_DEPS=.*|FREEZE_MODULE_BOOTSTRAP_DEPS=|g'\
		-e '/^[[:space:]]*\$$(MAKE) -f Makefile\.pre.*Makefile/d'\
		deps-$(NATIVE_TARGET)/Python-$(PYTHON)/Makefile.pre\
		> deps-$(TARGET)/Python-$(PYTHON)/Makefile.pre

	# absolutely cursed patch to force atomics LMAO.
	if echo "$(ARCH)" | grep -E "i[3-6]86|arm[^6]?|mips[^6]?|microblaze|sh|m68k|or1k|riscv(32|64)"; then\
		sed -i\
			-e '/^SYSLIBS=.*/ s/$$/ -latomic/g'\
			deps-$(TARGET)/Python-$(PYTHON)/Makefile.pre ;\
		sed -i\
			-e '/^SYSLIBS=.*/ s/$$/ -latomic/g'\
			deps-$(TARGET)/Python-$(PYTHON)/Makefile ;\
	fi

	test -f deps-$(NATIVE_TARGET)/Python-$(PYTHON)/pyconfig.h
	cp -p deps-$(NATIVE_TARGET)/Python-$(PYTHON)/pyconfig.h\
		deps-$(TARGET)/Python-$(PYTHON)/pyconfig.h
	test -f ./python/pyconfig/$(TARGET)-patches.h
	cat ./python/pyconfig/$(TARGET)-patches.h >> deps-$(TARGET)/Python-$(PYTHON)/pyconfig.h;

	# monkey patch gcc128
	if grep '#undef HAVE_GCC_UINT128_T' ./python/pyconfig/$(TARGET)-patches.h; then\
		sed -i\
			-e 's|-DHAVE_UINT128_T=1||g'\
			deps-$(TARGET)/Python-$(PYTHON)/Makefile.pre ;\
		sed -i\
			-e 's|-DHAVE_UINT128_T=1||g'\
			deps-$(TARGET)/Python-$(PYTHON)/Makefile ;\
	fi
	# monkey patch 32-bit
	if grep '32-bit' ./python/pyconfig/$(TARGET)-patches.h; then\
		sed -i\
			-e 's|-DCONFIG_64=1|-DCONFIG_32=1|g'\
			deps-$(TARGET)/Python-$(PYTHON)/Makefile.pre ;\
		sed -i\
			-e 's|-DCONFIG_64=1|-DCONFIG_32=1|g'\
			deps-$(TARGET)/Python-$(PYTHON)/Makefile ;\
	fi

	touch deps-$(TARGET)/Python-$(PYTHON)/Makefile
	touch deps-$(TARGET)/Python-$(PYTHON)/Makefile.pre

	mkdir -p python-static-$(TARGET)/bin python-static-$(TARGET)/lib
	cp -r python-static-$(NATIVE_TARGET)/include python-static-$(TARGET)/include
	cp -r python-static-$(NATIVE_TARGET)/share python-static-$(TARGET)/share
	rsync -a --exclude='__pycache__/' \
		python-static-$(NATIVE_TARGET)/lib/python$(PYTHONV) \
		python-static-$(TARGET)/lib
	# sysconfig bullshit
	mv python-static-$(TARGET)/lib/python$(PYTHONV)/_sysconfigdata__linux_$(NATIVE_TARGET).py\
		python-static-$(TARGET)/lib/python$(PYTHONV)/_sysconfigdata__linux_$(TARGET).py
	sed -i \
		"s/$(NATIVE_TARGET)/$(TARGET)/g"\
		python-static-$(TARGET)/lib/python$(PYTHONV)/_sysconfigdata__linux_$(TARGET).py

	# build the actual binary
	cd deps-$(TARGET)/Python-$(PYTHON) && PYTHON_BUILD=1 ../../configure-wrapper.sh make -j$(JOBS) build/python
	cp -r deps-$(TARGET)/Python-$(PYTHON)/build/python python-static-$(TARGET)/bin/python$(PYTHONV)
	ln -sf python$(PYTHONV) python-static-$(TARGET)/bin/python3
else
# native case; basically just stub everyting.
python-static-$(TARGET)/bin/python$(PYTHONV): $(PYTHON_DEPS)
	cd deps-$(TARGET)/Python-$(PYTHON) &&\
		PYTHON="1"\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)python-static-$(TARGET)\
			--exec-prefix=$(ROOT_DIR)python-static-$(TARGET) --disable-shared\
			--with-openssl=$(ROOT_DIR)build-$(TARGET)\
			--build=$(ARCH)-linux-$(MUSLABI)\
			--disable-test-modules\
			$(if $(filter 1,$(USE_PGO)),--enable-optimizations,)\
			--with-ensurepip=no
	cd deps-$(TARGET)/Python-$(PYTHON) && PYTHON_BUILD=1 ../../configure-wrapper.sh make -j$(JOBS) PROFILE_TASK='$(PROFILE_TASK)'
	mkdir -p python-static-$(TARGET)
	cd deps-$(TARGET)/Python-$(PYTHON) && PYTHON_BUILD=1 ../../configure-wrapper.sh make bininstall
	rm -rf python-static-$(TARGET)/lib/libpython$(PYTHONV).a
	rm -rf python-static-$(TARGET)/lib/python$(PYTHONV)/config-$(PYTHONV)-$(TARGET)
endif
