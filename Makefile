ROOT_DIR := $(dir $(realpath $(lastword $(MAKEFILE_LIST))))

CROSSMAKE := master
OPENSSL := 3.5.0
LIBFFI := 3.4.8
LIBLZMA := 5.8.1
ZLIB := 1.3.1
READLINE := 8.2
NCURSES := 6.5
SQLITE := 3490200
BZIP2 := 1.0.8
UTILLINUX := 2.41
PYTHON := 3.13.3
LINUX_VER := 5.15.184

SPLIT := $(subst ., ,$(PYTHON))
PYTHONV := $(word 1, $(SPLIT)).$(word 2, $(SPLIT))

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


export TCTYPE
export ARCH
export NATIVE_ARCH
export MUSLABI

# first target should be python3

.PHONY: python3 clean distclean

python3: python-static-$(TARGET)/bin/python$(PYTHONV)

clean:
	rm -rf deps-* build-* python-static-*

distclean: clean
	rm -rf tarballs

# build steps for musl toolchain.

tarballs/musl-cross-make-master.tar.gz:
	mkdir -p tarballs
	curl -Lf https://github.com/richfelker/musl-cross-make/archive/$(CROSSMAKE).tar.gz -o $@

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
	sed -i\
		-e 's/--enable-languages=c,c++/--enable-languages=c/g'\
		-e 's|--enable-libstdcxx-time=rt||g'\
		deps-$(TARGET)/musl-cross-make-$(CROSSMAKE)/litecross/Makefile
	cd deps-$(TARGET)/musl-cross-make-$(CROSSMAKE) && make -j$(JOBS)
	cd deps-$(TARGET)/musl-cross-make-$(CROSSMAKE) && make install
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

deps-$(TARGET)/openssl-$(OPENSSL)/.extracted: tarballs/openssl-$(OPENSSL).tar.gz
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	cd deps-$(TARGET)/openssl-$(OPENSSL) && sed -i '1513d' ./Configure
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

deps-$(TARGET)/zlib-$(ZLIB)/.extracted: tarballs/zlib-$(ZLIB).tar.gz
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	touch $@

build-$(TARGET)/lib/libz.a: deps-$(TARGET)/zlib-$(ZLIB)/.extracted deps-$(TARGET)/$(ARCH)-linux-$(MUSLABI)-$(TCTYPE)/.extracted
	mkdir -p build-$(TARGET)
	cd deps-$(TARGET)/zlib-$(ZLIB) &&\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build-$(TARGET) --eprefix=$(ROOT_DIR)build-$(TARGET) --static
	cd deps-$(TARGET)/zlib-$(ZLIB) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(TARGET)/zlib-$(ZLIB) && ../../configure-wrapper.sh make install

zlib: build-$(TARGET)/lib/libz.a
.PHONY: zlib

# compile ncurses

tarballs/ncurses-$(NCURSES).tar.gz:
	mkdir -p tarballs
	curl -Lf https://ftp.gnu.org/gnu/ncurses/ncurses-$(NCURSES).tar.gz -o $@

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
	curl -Lf https://www.sqlite.org/2025/sqlite-src-$(SQLITE).zip -o $@

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

deps-$(TARGET)/Python-$(PYTHON)/Modules/Setup.local: tarballs/Python-$(PYTHON).tgz
	mkdir -p deps-$(TARGET)
	tar -xzf $< -C deps-$(TARGET)
	# monkey patched code for static symbols in ctypes
	cp -r ./python/staticapi deps-$(TARGET)/Python-$(PYTHON)/Modules/staticapi
	# This seems EXTRMEMELY fragile, should patch later probably.
	sed -i \
		-e "319r ./python/ctypes_patch_1.py"\
		-e "486r ./python/ctypes_patch_2.py"\
		-e "390s/.*/            pass/"\
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
			--with-ensurepip=no
	cd deps-$(TARGET)/Python-$(PYTHON) && PYTHON_BUILD=1 ../../configure-wrapper.sh make -j$(JOBS)
	mkdir -p python-static-$(TARGET)
	cd deps-$(TARGET)/Python-$(PYTHON) && PYTHON_BUILD=1 ../../configure-wrapper.sh make bininstall
	rm -rf python-static-$(TARGET)/lib/libpython$(PYTHONV).a
	rm -rf python-static-$(TARGET)/lib/python$(PYTHONV)/config-$(PYTHONV)-$(TARGET)
endif
