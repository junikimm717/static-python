ROOT_DIR := $(dir $(realpath $(lastword $(MAKEFILE_LIST))))

OPENSSL := 3.5.0
LIBFFI := 3.4.8
LIBLZMA := 5.8.1
ZLIB := 1.3.1
READLINE := 8.2
NCURSES := 6.5
SQLITE := 3490200
BZIP2 := 1.0.8
PYTHON := 3.13.3
PYTHONV := 3.13

ARCH := $(shell uname -m)
JOBS := $(shell nproc)

DEPS_DIR := "$(ROOT_DIR)/deps-$(ARCH)"

CC=$(DEPS_DIR)/$(ARCH)-linux-musl-native/bin/gcc
AR=$(DEPS_DIR)/$(ARCH)-linux-musl-native/bin/ar
RANLIB=$(DEPS_DIR)/$(ARCH)-linux-musl-native/bin/ranlib
LD=$(DEPS_DIR)/$(ARCH)-linux-musl-native/bin/ld

# build steps for musl toolchain.

deps-$(ARCH)/$(ARCH)-linux-musl-native.tgz:
	mkdir -p deps-$(ARCH)
	curl -Lf https://musl.cc/$(ARCH)-linux-musl-native.tgz -o deps-$(ARCH)/$(ARCH)-linux-musl-native.tgz

deps-$(ARCH)/$(ARCH)-linux-musl-native/.extracted: deps-$(ARCH)/$(ARCH)-linux-musl-native.tgz
	tar -xzf $< -C deps-$(ARCH)
	touch $@

# compile openssl

deps-$(ARCH)/openssl-$(OPENSSL).tar.gz:
	mkdir -p deps-$(ARCH)
	curl -Lf https://github.com/openssl/openssl/releases/download/openssl-$(OPENSSL)/openssl-$(OPENSSL).tar.gz -o deps-$(ARCH)/openssl-$(OPENSSL).tar.gz

deps-$(ARCH)/openssl-$(OPENSSL)/.extracted: deps-$(ARCH)/openssl-$(OPENSSL).tar.gz
	tar -xzf deps-$(ARCH)/openssl-$(OPENSSL).tar.gz -C deps-$(ARCH)
	cd deps-$(ARCH)/openssl-$(OPENSSL) && sed -i '1513d' ./Configure
	touch $@

build-$(ARCH)/lib64/libssl.a: deps-$(ARCH)/$(ARCH)-linux-musl-native/.extracted deps-$(ARCH)/openssl-$(OPENSSL)/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/openssl-$(OPENSSL) &&\
		ARCH="$(ARCH)"\
		../../configure-wrapper.sh \
		./Configure linux-$(ARCH) \
			no-shared no-dso no-engine no-tests no-ssl3 no-comp no-idea no-rc5\
			no-ec2m no-weak-ssl-ciphers no-apps\
			--prefix=$(ROOT_DIR)build-$(ARCH) --openssldir=$(ROOT_DIR)build-$(ARCH)
	cd deps-$(ARCH)/openssl-$(OPENSSL) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(ARCH)/openssl-$(OPENSSL) && ../../configure-wrapper.sh make install_sw

openssl: build-$(ARCH)/lib64/libssl.a
.PHONY: openssl

# compile libffi

deps-$(ARCH)/libffi-$(LIBFFI).tar.gz:
	mkdir -p deps-$(ARCH)
	curl -Lf https://github.com/libffi/libffi/releases/download/v$(LIBFFI)/libffi-$(LIBFFI).tar.gz -o deps-$(ARCH)/libffi-$(LIBFFI).tar.gz

deps-$(ARCH)/libffi-$(LIBFFI)/.extracted: deps-$(ARCH)/libffi-$(LIBFFI).tar.gz
	tar -xzf deps-$(ARCH)/libffi-$(LIBFFI).tar.gz -C deps-$(ARCH)
	touch $@

build-$(ARCH)/lib/libffi.a: deps-$(ARCH)/$(ARCH)-linux-musl-native/.extracted deps-$(ARCH)/libffi-$(LIBFFI)/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/libffi-$(LIBFFI) &&\
		ARCH="$(ARCH)"\
		../../configure-wrapper.sh ./configure \
			--prefix=$(ROOT_DIR)build-$(ARCH) \
			--exec-prefix=$(ROOT_DIR)build-$(ARCH) \
			--enable-static --disable-shared
	cd deps-$(ARCH)/libffi-$(LIBFFI) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(ARCH)/libffi-$(LIBFFI) && ../../configure-wrapper.sh make install

libffi: build-$(ARCH)/lib/libffi.a
.PHONY: libffi

# compile libxz

deps-$(ARCH)/xz-$(LIBLZMA).tar.gz:
	mkdir -p deps-$(ARCH)
	curl -Lf https://github.com/tukaani-project/xz/releases/download/v5.8.1/xz-5.8.1.tar.gz -o deps-$(ARCH)/xz-5.8.1.tar.gz

deps-$(ARCH)/xz-$(LIBLZMA)/.extracted: deps-$(ARCH)/xz-$(LIBLZMA).tar.gz
	tar -xzf deps-$(ARCH)/xz-5.8.1.tar.gz -C deps-$(ARCH)
	touch $@

build-$(ARCH)/lib/liblzma.a: deps-$(ARCH)/xz-$(LIBLZMA)/.extracted deps-$(ARCH)/$(ARCH)-linux-musl-native/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/xz-$(LIBLZMA) &&\
		ARCH="$(ARCH)"\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build-$(ARCH) --exec-prefix=$(ROOT_DIR)build-$(ARCH) --enable-static --disable-shared
	cd deps-$(ARCH)/xz-$(LIBLZMA) && ../../configure-wrapper.sh make V=1 -j$(JOBS)
	cd deps-$(ARCH)/xz-$(LIBLZMA) && ../../configure-wrapper.sh make install

liblzma: build-$(ARCH)/lib/liblzma.a
.PHONY: liblzma

# compile zlib

deps-$(ARCH)/zlib-$(ZLIB).tar.gz:
	mkdir -p deps-$(ARCH)
	curl -Lf http://zlib.net/zlib-1.3.1.tar.gz -o deps-$(ARCH)/zlib-1.3.1.tar.gz

deps-$(ARCH)/zlib-$(ZLIB)/.extracted: deps-$(ARCH)/zlib-$(ZLIB).tar.gz
	tar -xzf deps-$(ARCH)/zlib-1.3.1.tar.gz -C deps-$(ARCH)
	touch $@

build-$(ARCH)/lib/libz.a: deps-$(ARCH)/zlib-$(ZLIB)/.extracted deps-$(ARCH)/$(ARCH)-linux-musl-native/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/zlib-$(ZLIB) &&\
		ARCH="$(ARCH)"\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build-$(ARCH) --eprefix=$(ROOT_DIR)build-$(ARCH) --static
	cd deps-$(ARCH)/zlib-$(ZLIB) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(ARCH)/zlib-$(ZLIB) && ../../configure-wrapper.sh make install

zlib: build-$(ARCH)/lib/libz.a
.PHONY: zlib

# compile ncurses

deps-$(ARCH)/ncurses-$(NCURSES).tar.gz:
	mkdir -p deps-$(ARCH)
	curl -Lf https://invisible-mirror.net/archives/ncurses/ncurses-$(NCURSES).tar.gz -o deps-$(ARCH)/ncurses-$(NCURSES).tar.gz

deps-$(ARCH)/ncurses-$(NCURSES)/.extracted: deps-$(ARCH)/ncurses-$(NCURSES).tar.gz
	tar -xzf deps-$(ARCH)/ncurses-$(NCURSES).tar.gz -C deps-$(ARCH)
	touch $@

build-$(ARCH)/lib/libncursesw.a: deps-$(ARCH)/ncurses-$(NCURSES)/.extracted deps-$(ARCH)/$(ARCH)-linux-musl-native/.extracted
	cd deps-$(ARCH)/ncurses-$(NCURSES) &&\
		ARCH="$(ARCH)"\
		../../configure-wrapper.sh ./configure --without-cxx --without-cxx-binding\
		--without-shared --prefix=$(ROOT_DIR)build-$(ARCH)\
		--exec-prefix=$(ROOT_DIR)build-$(ARCH) --enable-static\
		--without-ada \
		--without-manpages \
		--without-tests \
		--without-progs\
		--enable-termcap\
		--disable-shared
	cd deps-$(ARCH)/ncurses-$(NCURSES) && make -j$(JOBS)
	cd deps-$(ARCH)/ncurses-$(NCURSES) && make install

ncurses: build-$(ARCH)/lib/libncursesw.a
.PHONY: ncurses

# compile readline

deps-$(ARCH)/readline-$(READLINE).tar.gz:
	mkdir -p deps-$(ARCH)
	curl -Lf https://ftp.gnu.org/gnu/readline/readline-$(READLINE).tar.gz -o deps-$(ARCH)/readline-$(READLINE).tar.gz

deps-$(ARCH)/readline-$(READLINE)/.extracted: deps-$(ARCH)/readline-$(READLINE).tar.gz 
	tar -xzf deps-$(ARCH)/readline-$(READLINE).tar.gz -C deps-$(ARCH)
	touch $@

build-$(ARCH)/lib/libreadline.a: deps-$(ARCH)/$(ARCH)-linux-musl-native/.extracted deps-$(ARCH)/readline-$(READLINE)/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/readline-$(READLINE) &&\
		ARCH="$(ARCH)"\
		../../configure-wrapper.sh ./configure \
			--prefix=$(ROOT_DIR)build-$(ARCH)\
			--exec-prefix=$(ROOT_DIR)build-$(ARCH)\
			--with-curses\
			--disable-install-examples\
			--enable-static\
			--disable-shared
	cd deps-$(ARCH)/readline-$(READLINE) && ../../configure-wrapper.sh make -j1
	cd deps-$(ARCH)/readline-$(READLINE) && ../../configure-wrapper.sh make install

readline: build-$(ARCH)/lib/libreadline.a
.PHONY: readline

# compile steps for libsqlite

deps-$(ARCH)/sqlite-src-$(SQLITE).zip:
	mkdir -p deps-$(ARCH)
	curl -Lf https://www.sqlite.org/2025/sqlite-src-$(SQLITE).zip -o deps-$(ARCH)/sqlite-src-$(SQLITE).zip

deps-$(ARCH)/sqlite-src-$(SQLITE)/.extracted: deps-$(ARCH)/sqlite-src-$(SQLITE).zip
	cd deps-$(ARCH) && unzip -o sqlite-src-$(SQLITE).zip
	touch $@

build-$(ARCH)/lib/libsqlite3.a: deps-$(ARCH)/$(ARCH)-linux-musl-native/.extracted build-$(ARCH)/lib/libreadline.a deps-$(ARCH)/sqlite-src-$(SQLITE)/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/sqlite-src-$(SQLITE) &&\
		ARCH="$(ARCH)"\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build-$(ARCH) --exec-prefix=$(ROOT_DIR)build-$(ARCH) --enable-static --disable-shared
	cd deps-$(ARCH)/sqlite-src-$(SQLITE) && ../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(ARCH)/sqlite-src-$(SQLITE) && ../../configure-wrapper.sh make install

libsqlite: build-$(ARCH)/lib/libsqlite3.a
.PHONY: libsqlite

# compile bzip2

deps-$(ARCH)/bzip2-$(BZIP2).tar.gz:
	mkdir -p deps-$(ARCH)
	curl -Lf https://sourceware.org/pub/bzip2/bzip2-$(BZIP2).tar.gz -o deps-$(ARCH)/bzip2-$(BZIP2).tar.gz

deps-$(ARCH)/bzip2-$(BZIP2)/.extracted: deps-$(ARCH)/bzip2-$(BZIP2).tar.gz
	tar -xzf deps-$(ARCH)/bzip2-$(BZIP2).tar.gz -C deps-$(ARCH)
	sed -i \
		-e 's|^CC=.*||' \
		-e 's|^AR=.*||' \
		-e 's|^RANLIB=.*||' \
		-e 's|^CFLAGS=.*||' \
		-e 's|^LDFLAGS=.*||' \
		-e 's|^PREFIX=.*|PREFIX=$(ROOT_DIR)build-$(ARCH)|' \
		deps-$(ARCH)/bzip2-$(BZIP2)/Makefile
	touch $@

build-$(ARCH)/lib/libbz2.a: deps-$(ARCH)/$(ARCH)-linux-musl-native/.extracted deps-$(ARCH)/bzip2-$(BZIP2)/.extracted
	mkdir -p build-$(ARCH)
	cd deps-$(ARCH)/bzip2-$(BZIP2) &&\
		ARCH="$(ARCH)"\
		../../configure-wrapper.sh make -j$(JOBS)
	cd deps-$(ARCH)/bzip2-$(BZIP2) &&\
		ARCH="$(ARCH)"\
		../../configure-wrapper.sh make install

libbz2: build-$(ARCH)/lib/libbz2.a
.PHONY: libbz2

# compile python3

deps-$(ARCH)/Python-$(PYTHON).tgz:
	mkdir -p deps-$(ARCH)
	curl -Lf https://www.python.org/ftp/python/$(PYTHON)/Python-$(PYTHON).tgz -o deps-$(ARCH)/Python-$(PYTHON).tgz

deps-$(ARCH)/Python-$(PYTHON)/Modules/Setup.local: deps-$(ARCH)/Python-$(PYTHON).tgz
	tar -xzf deps-$(ARCH)/Python-$(PYTHON).tgz -C deps-$(ARCH)
	# monkey patched code for static symbols in ctypes
	cp -r ./staticapi deps-$(ARCH)/Python-$(PYTHON)/Modules/staticapi
	sed -i \
		-e "319r ./staticapi/ctypes_patch_1.py"\
		-e "486r ./staticapi/ctypes_patch_2.py"\
		-e "390s/.*/            pass/"\
		./deps-$(ARCH)/Python-$(PYTHON)/Lib/ctypes/__init__.py
	cp -r ./Setup deps-$(ARCH)/Python-$(PYTHON)/Modules/Setup.local

python-static-$(ARCH)/bin/python$(PYTHONV): openssl libffi libsqlite liblzma readline zlib libbz2 ncurses deps-$(ARCH)/Python-$(PYTHON)/Modules/Setup.local deps-$(ARCH)/$(ARCH)-linux-musl-native/.extracted
	cd deps-$(ARCH)/Python-$(PYTHON) &&\
		ARCH="$(ARCH)"\
		PYTHON="1"\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)python-static-$(ARCH)\
			--exec-prefix=$(ROOT_DIR)python-static-$(ARCH) --enable-static --disable-shared\
			--with-openssl=$(ROOT_DIR)build-$(ARCH)\
			--disable-test-modules\
			--with-ensurepip=install
	cd deps-$(ARCH)/Python-$(PYTHON) && PYTHON=1 ../../configure-wrapper.sh make -j8
	mkdir -p python-static-$(ARCH)
	cd deps-$(ARCH)/Python-$(PYTHON) && PYTHON=1 ../../configure-wrapper.sh make install

python3: python-static-$(ARCH)/bin/python$(PYTHONV)
.PHONY: python3
