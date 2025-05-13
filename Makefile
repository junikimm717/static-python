ROOT_DIR := $(dir $(realpath $(lastword $(MAKEFILE_LIST))))

CROSSMAKE = 0.9.10
OPENSSL = 3.5.0
LIBFFI = 3.4.8
LIBLZMA = 5.8.1
ZLIB = 1.3.1
READLINE = 8.2
NCURSES = 6.5
SQLITE = 3490200
BZIP2 = 1.0.8
UTILLINUX = 2.41
PYTHON = 3.13.3

SPLIT := $(subst ., ,$(PYTHON))
PYTHONV := $(word 1, $(SPLIT)).$(word 2, $(SPLIT))

ARCH := $(shell uname -m)
JOBS := $(shell nproc)

override SUPPORTED := x86_64 aarch64 mips64 powerpc64le s390x riscv64 powerpc64
override NATIVE_ARCH := $(shell uname -m)
USE_CROSSMAKE = 0

ifeq ($(filter $(ARCH),$(SUPPORTED)),)
$(error ARCH '$(ARCH)' is not one of the allowed values: $(SUPPORTED))
endif

# do a bunch of architecture fiddling.

ifneq ($(ARCH),$(NATIVE_ARCH))
override TCTYPE=cross
$(info Cross-Compiling to $(ARCH) from $(NATIVE_ARCH)...)
ifneq ($(NATIVE_ARCH),x86_64)
# you need musl crossmake if cross-compiling from non-x86 architecture.
$(info Not x86_64, musl-cross-make will be required)
endif
else
override TCTYPE=native
$(info Native Compiling in $(NATIVE_ARCH)...)
endif


export TCTYPE
export ARCH
export NATIVE_ARCH

DEPS_DIR := "$(ROOT_DIR)/deps-$(ARCH)"

# first target should be python3

.PHONY: python3 clean distclean

python3: python-static-$(ARCH)/bin/python$(PYTHONV)

clean:
	rm -rf deps-* build-* python-static-*

distclean: clean
	rm -rf tarballs

# build steps for musl toolchain.
# if not on an x86_64 machine, toolchains must get manually built.

tarballs/musl-cross-make-$(CROSSMAKE).tar.gz:
	mkdir -p tarballs
	curl -Lf https://github.com/richfelker/musl-cross-make/archive/refs/tags/v$(CROSSMAKE).tar.gz -o $@

deps-$(ARCH)/musl-cross-make-$(CROSSMAKE)/.extracted: tarballs/musl-cross-make-$(CROSSMAKE).tar.gz
	mkdir -p deps-$(ARCH)
	tar -xzf $< -C deps-$(ARCH)
	ln -sfn $(ROOT_DIR)/tarballs deps-$(ARCH)/musl-cross-make-$(CROSSMAKE)/sources
	touch $@

override FILENAME = $(ARCH)-linux-musl-$(TCTYPE).tgz
deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE).tgz:
	mkdir -p deps-$(ARCH)
	curl -Lf $(shell TCTYPE=$(TCTYPE) NATIVE_ARCH=$(NATIVE_ARCH) ARCH=$(ARCH) ./musl-source.sh)$(ARCH)-linux-musl-$(TCTYPE).tgz\
		-o deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE).tgz

.PHONY: crossmake
crossmake: deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)/.extracted

ifeq ($(USE_CROSSMAKE),1)
# manually compile the toolchain.
deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)/.extracted: deps-$(ARCH)/musl-cross-make-$(CROSSMAKE)/.extracted
	sed\
		-e 's|^TARGET=.*|TARGET=$(ARCH)-linux-musl|g'\
		-e 's|^OUTPUT=.*|OUTPUT=$(ROOT_DIR)deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)|g'\
		./cross-make/config.mak\
		> deps-$(ARCH)/musl-cross-make-$(CROSSMAKE)/config.mak
	sed -i\
		-e 's/\([jJz]x\)vf/\1f/g'\
		-e 's|^LINUX_VER =.*|LINUX_VER = 5.8.5|g'\
		deps-$(ARCH)/musl-cross-make-$(CROSSMAKE)/Makefile
	cd deps-$(ARCH)/musl-cross-make-$(CROSSMAKE) && make -j$(JOBS)
	cd deps-$(ARCH)/musl-cross-make-$(CROSSMAKE) && make install
	touch $@
else
deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)/.extracted: deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE).tgz
	tar -xzf $< -C deps-$(ARCH)
	touch $@
endif

# compile openssl

tarballs/openssl-$(OPENSSL).tar.gz:
	mkdir -p tarballs
	curl -Lf https://github.com/openssl/openssl/releases/download/openssl-$(OPENSSL)/openssl-$(OPENSSL).tar.gz -o $@

deps-$(ARCH)/openssl-$(OPENSSL)/.extracted: tarballs/openssl-$(OPENSSL).tar.gz
	mkdir -p deps-$(ARCH)
	tar -xzf $< -C deps-$(ARCH)
	cd deps-$(ARCH)/openssl-$(OPENSSL) && sed -i '1513d' ./Configure
	touch $@

build-$(ARCH)/include/openssl/ssl.h: deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)/.extracted deps-$(ARCH)/openssl-$(OPENSSL)/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/openssl-$(OPENSSL) &&\
		../../configure-wrapper.sh \
		./Configure $(shell ./openssl-platform.sh $(ARCH))\
			no-shared no-dso no-asm no-engine no-tests no-ssl3 no-comp no-idea no-rc5\
			no-ec2m no-weak-ssl-ciphers no-apps\
			--prefix=$(ROOT_DIR)build-$(ARCH) --openssldir=$(ROOT_DIR)build-$(ARCH)
	cd deps-$(ARCH)/openssl-$(OPENSSL) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(ARCH)/openssl-$(OPENSSL) && ../../configure-wrapper.sh make install_sw

openssl: build-$(ARCH)/include/openssl/ssl.h
.PHONY: openssl

# compile libffi

tarballs/libffi-$(LIBFFI).tar.gz:
	mkdir -p tarballs
	curl -Lf https://github.com/libffi/libffi/releases/download/v$(LIBFFI)/libffi-$(LIBFFI).tar.gz -o $@

deps-$(ARCH)/libffi-$(LIBFFI)/.extracted: tarballs/libffi-$(LIBFFI).tar.gz
	mkdir -p deps-$(ARCH)
	tar -xzf $< -C deps-$(ARCH)
	touch $@

build-$(ARCH)/lib/libffi.a: deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)/.extracted deps-$(ARCH)/libffi-$(LIBFFI)/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/libffi-$(LIBFFI) &&\
		../../configure-wrapper.sh ./configure \
			--prefix=$(ROOT_DIR)build-$(ARCH) \
			--host=$(ARCH)-linux-musl
			--exec-prefix=$(ROOT_DIR)build-$(ARCH) \
			--enable-static --disable-shared
	cd deps-$(ARCH)/libffi-$(LIBFFI) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(ARCH)/libffi-$(LIBFFI) && ../../configure-wrapper.sh make install

libffi: build-$(ARCH)/lib/libffi.a
.PHONY: libffi

# compile libxz

tarballs/xz-$(LIBLZMA).tar.gz:
	mkdir -p tarballs
	curl -Lf https://github.com/tukaani-project/xz/releases/download/v$(LIBLZMA)/xz-$(LIBLZMA).tar.gz -o $@

deps-$(ARCH)/xz-$(LIBLZMA)/.extracted: tarballs/xz-$(LIBLZMA).tar.gz
	mkdir -p deps-$(ARCH)
	tar -xzf $< -C deps-$(ARCH)
	touch $@

build-$(ARCH)/lib/liblzma.a: deps-$(ARCH)/xz-$(LIBLZMA)/.extracted deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/xz-$(LIBLZMA) &&\
		ARCH="$(ARCH)"\
		TCTYPE="$(TCTYPE)"\
		../../configure-wrapper.sh ./configure \
			--prefix=$(ROOT_DIR)build-$(ARCH) \
			--host=$(ARCH)-linux-musl\
			--exec-prefix=$(ROOT_DIR)build-$(ARCH)\
			--enable-static --disable-shared
	cd deps-$(ARCH)/xz-$(LIBLZMA) && ../../configure-wrapper.sh make V=1 -j$(JOBS)
	cd deps-$(ARCH)/xz-$(LIBLZMA) && ../../configure-wrapper.sh make install

liblzma: build-$(ARCH)/lib/liblzma.a
.PHONY: liblzma

# compile zlib

tarballs/zlib-$(ZLIB).tar.gz:
	mkdir -p tarballs
	curl -Lf http://zlib.net/zlib-$(ZLIB).tar.gz -o $@

deps-$(ARCH)/zlib-$(ZLIB)/.extracted: tarballs/zlib-$(ZLIB).tar.gz
	mkdir -p deps-$(ARCH)
	tar -xzf $< -C deps-$(ARCH)
	touch $@

build-$(ARCH)/lib/libz.a: deps-$(ARCH)/zlib-$(ZLIB)/.extracted deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/zlib-$(ZLIB) &&\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build-$(ARCH) --eprefix=$(ROOT_DIR)build-$(ARCH) --static
	cd deps-$(ARCH)/zlib-$(ZLIB) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(ARCH)/zlib-$(ZLIB) && ../../configure-wrapper.sh make install

zlib: build-$(ARCH)/lib/libz.a
.PHONY: zlib

# compile ncurses

tarballs/ncurses-$(NCURSES).tar.gz:
	mkdir -p tarballs
	curl -Lf https://ftp.gnu.org/gnu/ncurses/ncurses-$(NCURSES).tar.gz -o $@

deps-$(ARCH)/ncurses-$(NCURSES)/.extracted: tarballs/ncurses-$(NCURSES).tar.gz
	mkdir -p deps-$(ARCH)
	tar -xzf $< -C deps-$(ARCH)
	touch $@

build-$(ARCH)/lib/libncursesw.a: deps-$(ARCH)/ncurses-$(NCURSES)/.extracted deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)/.extracted
	cd deps-$(ARCH)/ncurses-$(NCURSES) &&\
		../../configure-wrapper.sh ./configure --without-cxx --without-cxx-binding\
			--without-shared --prefix=$(ROOT_DIR)build-$(ARCH)\
			--exec-prefix=$(ROOT_DIR)build-$(ARCH) --enable-static\
			--host=$(ARCH)-linux-musl\
			--without-ada \
			--without-manpages \
			--without-tests \
			--without-progs\
			--enable-termcap\
			--disable-shared
	cd deps-$(ARCH)/ncurses-$(NCURSES) && make -j$(JOBS)
	cd deps-$(ARCH)/ncurses-$(NCURSES) &&\
		TIC_PATH=$(shell command -v tic) make install

ncurses: build-$(ARCH)/lib/libncursesw.a
.PHONY: ncurses

# compile readline

tarballs/readline-$(READLINE).tar.gz:
	mkdir -p tarballs
	curl -Lf https://ftp.gnu.org/gnu/readline/readline-$(READLINE).tar.gz -o $@

deps-$(ARCH)/readline-$(READLINE)/.extracted: tarballs/readline-$(READLINE).tar.gz 
	mkdir -p deps-$(ARCH)
	tar -xzf $< -C deps-$(ARCH)
	touch $@

build-$(ARCH)/lib/libreadline.a: deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)/.extracted deps-$(ARCH)/readline-$(READLINE)/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/readline-$(READLINE) &&\
		../../configure-wrapper.sh ./configure \
			--prefix=$(ROOT_DIR)build-$(ARCH)\
			--exec-prefix=$(ROOT_DIR)build-$(ARCH)\
			--with-curses\
			--host=$(ARCH)-linux-musl\
			--disable-install-examples\
			--enable-static\
			--disable-shared
	cd deps-$(ARCH)/readline-$(READLINE) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(ARCH)/readline-$(READLINE) && ../../configure-wrapper.sh make install

readline: build-$(ARCH)/lib/libreadline.a
.PHONY: readline

# compile steps for libsqlite

tarballs/sqlite-src-$(SQLITE).zip:
	mkdir -p tarballs
	curl -Lf https://www.sqlite.org/2025/sqlite-src-$(SQLITE).zip -o $@

deps-$(ARCH)/sqlite-src-$(SQLITE)/.extracted: tarballs/sqlite-src-$(SQLITE).zip
	mkdir -p deps-$(ARCH)
	cd deps-$(ARCH) && unzip -o ../tarballs/sqlite-src-$(SQLITE).zip
	touch $@

build-$(ARCH)/lib/libsqlite3.a: deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)/.extracted build-$(ARCH)/lib/libreadline.a deps-$(ARCH)/sqlite-src-$(SQLITE)/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/sqlite-src-$(SQLITE) &&\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build-$(ARCH)\
			--exec-prefix=$(ROOT_DIR)build-$(ARCH)\
			--host=$(ARCH)-linux-musl\
			--enable-static --disable-shared
	cd deps-$(ARCH)/sqlite-src-$(SQLITE) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(ARCH)/sqlite-src-$(SQLITE) && ../../configure-wrapper.sh make install

libsqlite: build-$(ARCH)/lib/libsqlite3.a
.PHONY: libsqlite

# compile bzip2

tarballs/bzip2-$(BZIP2).tar.gz:
	mkdir -p tarballs
	curl -Lf https://sourceware.org/pub/bzip2/bzip2-$(BZIP2).tar.gz -o $@

deps-$(ARCH)/bzip2-$(BZIP2)/.extracted: tarballs/bzip2-$(BZIP2).tar.gz
	mkdir -p deps-$(ARCH)
	tar -xzf $< -C deps-$(ARCH)
	sed -i \
		-e 's|^CC=.*||' \
		-e 's|^AR=.*||' \
		-e 's|^RANLIB=.*||' \
		-e 's|^CFLAGS=.*||' \
		-e 's|^LDFLAGS=.*||' \
		-e 's|^PREFIX=.*|PREFIX=$(ROOT_DIR)build-$(ARCH)|' \
		deps-$(ARCH)/bzip2-$(BZIP2)/Makefile
	touch $@

build-$(ARCH)/lib/libbz2.a: deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)/.extracted deps-$(ARCH)/bzip2-$(BZIP2)/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/bzip2-$(BZIP2) && ../../configure-wrapper.sh make libbz2.a bzip2 bzip2recover -j$(JOBS)
	cd deps-$(ARCH)/bzip2-$(BZIP2) && ../../configure-wrapper.sh make install

libbz2: build-$(ARCH)/lib/libbz2.a
.PHONY: libbz2

# compile libuuid

tarballs/util-linux-$(UTILLINUX).tar.gz:
	mkdir -p tarballs
	curl -Lf https://github.com/util-linux/util-linux/archive/refs/tags/v$(UTILLINUX).tar.gz -o $@

deps-$(ARCH)/util-linux-$(UTILLINUX)/.extracted: tarballs/util-linux-$(UTILLINUX).tar.gz
	mkdir -p deps-$(ARCH)
	tar -xzf $< -C deps-$(ARCH)
	touch $@

build-$(ARCH)/lib/libuuid.a: deps-$(ARCH)/util-linux-$(UTILLINUX)/.extracted deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)/.extracted
	cd deps-$(ARCH)/util-linux-$(UTILLINUX) && \
		printf "[properties]\nneeds_exe_wrapper = true\n" > ./cross.ini
	cd deps-$(ARCH)/util-linux-$(UTILLINUX) && \
		grep -E "option(.*),[[:space:]]*type[[:space:]]*:[[:space:]]*'feature'" meson_options.txt \
			| grep -vE 'build-libuuid' \
			| sed -E "s/.*option\(['\"]([a-zA-Z0-9_-]+)['\"].*/\1/" \
			| awk '{print "-D" $$1 "=disabled"}'\
			> ./build-args.txt &&\
		echo "--prefix=$(ROOT_DIR)build-$(ARCH) --default-library=static --prefer-static --buildtype=release --backend=ninja --cross-file cross.ini"\
			>> ./build-args.txt
	cd deps-$(ARCH)/util-linux-$(UTILLINUX) && \
		../../configure-wrapper.sh meson setup build $$(cat ./build-args.txt)
	ninja -C deps-$(ARCH)/util-linux-$(UTILLINUX)/build
	ninja -C deps-$(ARCH)/util-linux-$(UTILLINUX)/build install
	
libuuid: build-$(ARCH)/lib/libuuid.a
.PHONY: libuuid

# compile python3

tarballs/Python-$(PYTHON).tgz:
	mkdir -p tarballs
	curl -Lf https://www.python.org/ftp/python/$(PYTHON)/Python-$(PYTHON).tgz -o $@

deps-$(ARCH)/Python-$(PYTHON)/Modules/Setup.local: tarballs/Python-$(PYTHON).tgz
	mkdir -p deps-$(ARCH)
	tar -xzf $< -C deps-$(ARCH)
	# monkey patched code for static symbols in ctypes
	cp -r ./python/staticapi deps-$(ARCH)/Python-$(PYTHON)/Modules/staticapi
	# This seems EXTRMEMELY fragile, should patch later probably.
	sed -i \
		-e "319r ./python/ctypes_patch_1.py"\
		-e "486r ./python/ctypes_patch_2.py"\
		-e "390s/.*/            pass/"\
		./deps-$(ARCH)/Python-$(PYTHON)/Lib/ctypes/__init__.py
	cp -r ./python/Setup deps-$(ARCH)/Python-$(PYTHON)/Modules/Setup.local

# you must distinguish between cross and native compilation.
# apparently you need the native interpreter for cross compilation sob

PYTHON_DEPS = deps-$(ARCH)/$(ARCH)-linux-musl-$(TCTYPE)/.extracted\
		openssl libffi libuuid libsqlite liblzma readline zlib libbz2 ncurses \
		deps-$(ARCH)/Python-$(PYTHON)/Modules/Setup.local

ifeq ($(TCTYPE),cross)
# cross-compiling case; use python-specific flags.
override NATIVE_PATH := python-static-$(NATIVE_ARCH)/bin/python$(PYTHONV)
.PHONY: check_native
check_native:
	test -f $(NATIVE_PATH) -a -d deps-$(NATIVE_ARCH)/Python-$(PYTHON)
python-static-$(ARCH)/bin/python$(PYTHONV): check_native $(PYTHON_DEPS)
	mkdir -p deps-$(ARCH)/Python-$(PYTHON)/build
	cp ./python/config.status deps-$(ARCH)/Python-$(PYTHON)/config.status\
		&& chmod +x deps-$(ARCH)/Python-$(PYTHON)/config.status
	sed\
		-e '/^CC=/d' \
		-e '/^AR=/d' \
		-e 's|$(NATIVE_ARCH)|$(ARCH)|g'\
		-e 's|^BUILDPYTHON=.*|BUILDPYTHON=build/python|g'\
		-e 's|^PYTHON_FOR_BUILD=.*|PYTHON_FOR_BUILD=$(ROOT_DIR)$(NATIVE_PATH) -E|g'\
		-e 's|^PYTHON_FOR_BUILD_DEPS=.*|PYTHON_FOR_BUILD_DEPS=|g'\
		-e 's|^PYTHON_FOR_FREEZE=.*|PYTHON_FOR_FREEZE=$(ROOT_DIR)$(NATIVE_PATH)|g'\
		-e 's|^FREEZE_MODULE_BOOTSTRAP=.*|FREEZE_MODULE_BOOTSTRAP=$(ROOT_DIR)deps-$(NATIVE_ARCH)/Python-$(PYTHON)/Programs/_freeze_module|g'\
		-e 's|^FREEZE_MODULE_BOOTSTRAP_DEPS=.*|FREEZE_MODULE_BOOTSTRAP_DEPS=|g'\
		-e '/^[[:space:]]*\$$(MAKE) -f Makefile\.pre.*Makefile/d'\
		deps-$(NATIVE_ARCH)/Python-$(PYTHON)/Makefile\
		> deps-$(ARCH)/Python-$(PYTHON)/Makefile
	sed\
		-e '/^CC=/d' \
		-e '/^AR=/d' \
		-e 's|$(NATIVE_ARCH)|$(ARCH)|g'\
		-e 's|^BUILDPYTHON=.*|BUILDPYTHON=build/python|g'\
		-e 's|^PYTHON_FOR_BUILD=.*|PYTHON_FOR_BUILD=$(ROOT_DIR)$(NATIVE_PATH) -E|g'\
		-e 's|^PYTHON_FOR_BUILD_DEPS=.*|PYTHON_FOR_BUILD_DEPS=|g'\
		-e 's|^PYTHON_FOR_FREEZE=.*|PYTHON_FOR_FREEZE=$(ROOT_DIR)$(NATIVE_PATH)|g'\
		-e 's|^FREEZE_MODULE_BOOTSTRAP=.*|FREEZE_MODULE_BOOTSTRAP=$(ROOT_DIR)deps-$(NATIVE_ARCH)/Python-$(PYTHON)/Programs/_freeze_module|g'\
		-e 's|^FREEZE_MODULE_BOOTSTRAP_DEPS=.*|FREEZE_MODULE_BOOTSTRAP_DEPS=|g'\
		-e '/^[[:space:]]*\$$(MAKE) -f Makefile\.pre.*Makefile/d'\
		deps-$(NATIVE_ARCH)/Python-$(PYTHON)/Makefile.pre\
		> deps-$(ARCH)/Python-$(PYTHON)/Makefile.pre
	# absolutely cursed patch
	if test "$(ARCH)" = "riscv64"; then\
		sed -i\
			-e 's|^LIBS=.*|LIBS=-latomic|g'\
			deps-$(ARCH)/Python-$(PYTHON)/Makefile.pre ;\
		sed -i\
			-e 's|^LIBS=.*|LIBS=-latomic|g'\
			deps-$(ARCH)/Python-$(PYTHON)/Makefile ;\
	fi

	test -f deps-$(NATIVE_ARCH)/Python-$(PYTHON)/pyconfig.h
	cp -p deps-$(NATIVE_ARCH)/Python-$(PYTHON)/pyconfig.h\
		deps-$(ARCH)/Python-$(PYTHON)/pyconfig.h
	if test -f ./python/pyconfig/$(ARCH)-patches.h; then \
		cat ./python/pyconfig/$(ARCH)-patches.h >> deps-$(ARCH)/Python-$(PYTHON)/pyconfig.h; \
	else \
		cat ./python/pyconfig-patches.h >> deps-$(ARCH)/Python-$(PYTHON)/pyconfig.h; \
	fi
	touch deps-$(ARCH)/Python-$(PYTHON)/Makefile
	touch deps-$(ARCH)/Python-$(PYTHON)/Makefile.pre

	cd deps-$(ARCH)/Python-$(PYTHON) && PYTHON=1 ../../configure-wrapper.sh make -j$(JOBS) build/python

	mkdir -p python-static-$(ARCH)/bin python-static-$(ARCH)/lib
	cp -r python-static-$(NATIVE_ARCH)/include python-static-$(ARCH)/include
	cp -r python-static-$(NATIVE_ARCH)/share python-static-$(ARCH)/share
	rsync -a --exclude='__pycache__/' \
		python-static-$(NATIVE_ARCH)/lib/python$(PYTHONV) \
		python-static-$(ARCH)/lib

	cp -r deps-$(ARCH)/Python-$(PYTHON)/build/python python-static-$(ARCH)/bin/python$(PYTHONV)
	ln -sf python$(PYTHONV) python-static-$(ARCH)/bin/python3
else
# native case; basically just stub everyting.
python-static-$(ARCH)/bin/python$(PYTHONV): $(PYTHON_DEPS)
	cd deps-$(ARCH)/Python-$(PYTHON) &&\
		PYTHON="1"\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)python-static-$(ARCH)\
			--exec-prefix=$(ROOT_DIR)python-static-$(ARCH) --disable-shared\
			--with-openssl=$(ROOT_DIR)build-$(ARCH)\
			--build=$(ARCH)-linux-musl\
			--disable-test-modules\
			--with-ensurepip=no
	cd deps-$(ARCH)/Python-$(PYTHON) && PYTHON=1 ../../configure-wrapper.sh make -j$(JOBS)
	mkdir -p python-static-$(ARCH)
	cd deps-$(ARCH)/Python-$(PYTHON) && PYTHON=1 ../../configure-wrapper.sh make bininstall
endif

.PHONY: native-interpreter python-configure
