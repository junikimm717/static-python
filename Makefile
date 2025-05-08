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

build/.extracted:
	mkdir -p build
	touch $@

deps/.extracted:
	mkdir -p deps
	touch $@

# build steps for musl toolchain.

deps/x86_64-linux-musl-cross.tgz: deps/.extracted
	curl -Lf https://musl.cc/x86_64-linux-musl-cross.tgz -o deps/x86_64-linux-musl-cross.tgz

deps/x86_64-linux-musl-cross/.extracted: deps/x86_64-linux-musl-cross.tgz
	tar -xzf $< -C deps
	touch $@

# build steps for statically building openssl.
deps/openssl-$(OPENSSL).tar.gz: deps/.extracted
	curl -Lf https://github.com/openssl/openssl/releases/download/openssl-$(OPENSSL)/openssl-$(OPENSSL).tar.gz -o deps/openssl-$(OPENSSL).tar.gz

deps/openssl-$(OPENSSL)/.extracted: deps/openssl-$(OPENSSL).tar.gz
	tar -xzf deps/openssl-$(OPENSSL).tar.gz -C deps
	touch $@
	cd deps/openssl-$(OPENSSL) && sed -i '1513d' ./Configure

build/lib64/libssl.a: deps/x86_64-linux-musl-cross/.extracted deps/openssl-$(OPENSSL)/.extracted build/.extracted
	cd deps/openssl-$(OPENSSL) &&\
		CFLAGS="-static"\
		../../configure-wrapper.sh ./Configure linux-x86_64 --prefix=$(ROOT_DIR)build --openssldir=$(ROOT_DIR)build no-shared
	cd deps/openssl-$(OPENSSL) && ../../configure-wrapper.sh make -j4
	cd deps/openssl-$(OPENSSL) && ../../configure-wrapper.sh make install

openssl: build/lib64/libssl.a
.PHONY: openssl

# build steps for libffi

deps/libffi-$(LIBFFI).tar.gz: deps/.extracted
	curl -Lf https://github.com/libffi/libffi/releases/download/v$(LIBFFI)/libffi-$(LIBFFI).tar.gz -o deps/libffi-$(LIBFFI).tar.gz

deps/libffi-$(LIBFFI)/.extracted: deps/libffi-$(LIBFFI).tar.gz
	tar -xzf deps/libffi-$(LIBFFI).tar.gz -C deps
	touch $@

build/lib/libffi.a: deps/x86_64-linux-musl-cross/.extracted deps/libffi-$(LIBFFI)/.extracted build/.extracted
	cd deps/libffi-$(LIBFFI) &&\
		CFLAGS="-static"\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build --exec-prefix=$(ROOT_DIR)build --enable-static --disable-shared
	cd deps/libffi-$(LIBFFI) && ../../configure-wrapper.sh make -j4
	cd deps/libffi-$(LIBFFI) && ../../configure-wrapper.sh make install

libffi: build/lib/libffi.a
.PHONY: libffi

# build steps for libffi
deps/xz-$(LIBLZMA).tar.gz: deps/.extracted
	curl -Lf https://github.com/tukaani-project/xz/releases/download/v5.8.1/xz-5.8.1.tar.gz -o deps/xz-5.8.1.tar.gz

deps/xz-$(LIBLZMA)/.extracted: deps/xz-$(LIBLZMA).tar.gz
	tar -xzf deps/xz-5.8.1.tar.gz -C deps
	touch $@

build/lib/liblzma.a: deps/xz-$(LIBLZMA)/.extracted deps/x86_64-linux-musl-cross/.extracted build/.extracted
	cd deps/xz-$(LIBLZMA) &&\
		CFLAGS="-static"\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build --exec-prefix=$(ROOT_DIR)build --enable-static --disable-shared
	cd deps/xz-$(LIBLZMA) && ../../configure-wrapper.sh make V=1 -j4
	cd deps/xz-$(LIBLZMA) && ../../configure-wrapper.sh make install

liblzma: build/lib/liblzma.a
.PHONY: liblzma

# build steps for zlib
deps/zlib-$(ZLIB).tar.gz: deps/.extracted
	curl -Lf http://zlib.net/zlib-1.3.1.tar.gz -o deps/zlib-1.3.1.tar.gz

deps/zlib-$(ZLIB)/.extracted: deps/zlib-$(ZLIB).tar.gz
	tar -xzf deps/zlib-1.3.1.tar.gz -C deps
	touch $@

build/lib/libz.a: deps/zlib-$(ZLIB)/.extracted deps/x86_64-linux-musl-cross/.extracted build/.extracted
	cd deps/zlib-$(ZLIB) &&\
		CFLAGS="-static"\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build --eprefix=$(ROOT_DIR)build --static
	cd deps/zlib-$(ZLIB) && ../../configure-wrapper.sh make -j4
	cd deps/zlib-$(ZLIB) && ../../configure-wrapper.sh make install

zlib: build/lib/libz.a
.PHONY: zlib

# build steps for ncurses

deps/ncurses-$(NCURSES).tar.gz: deps/.extracted
	curl -Lf https://invisible-mirror.net/archives/ncurses/ncurses-$(NCURSES).tar.gz -o deps/ncurses-$(NCURSES).tar.gz

deps/ncurses-$(NCURSES)/.extracted: deps/ncurses-$(NCURSES).tar.gz
	tar -xzf deps/ncurses-$(NCURSES).tar.gz -C deps
	touch $@

build/lib/libncursesw.a: deps/ncurses-$(NCURSES)/.extracted
	cd deps/ncurses-$(NCURSES) &&\
		../../configure-wrapper.sh ./configure --without-cxx --without-cxx-binding\
		--without-shared --prefix=$(ROOT_DIR)build\
		--exec-prefix=$(ROOT_DIR)build --enable-static\
		--disable-shared
	cd deps/ncurses-$(NCURSES) && make -j4
	cd deps/ncurses-$(NCURSES) && make install

ncurses: build/lib/libncursesw.a
.PHONY: ncurses

# build steps for gnu readline

deps/readline-$(READLINE).tar.gz: deps/.extracted
	curl -Lf https://ftp.gnu.org/gnu/readline/readline-$(READLINE).tar.gz -o deps/readline-$(READLINE).tar.gz

deps/readline-$(READLINE)/.extracted: deps/readline-$(READLINE).tar.gz
	tar -xzf deps/readline-$(READLINE).tar.gz -C deps
	touch $@

build/lib/libreadline.a: deps/x86_64-linux-musl-cross/.extracted deps/readline-$(READLINE)/.extracted build/.extracted
	cd deps/readline-$(READLINE) &&\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build --exec-prefix=$(ROOT_DIR)build --enable-static --disable-shared
	cd deps/readline-$(READLINE) && ../../configure-wrapper.sh make -j1
	cd deps/readline-$(READLINE) && ../../configure-wrapper.sh make install

readline: build/lib/libreadline.a
.PHONY: readline

# build steps for libsqlite

deps/sqlite-src-$(SQLITE).zip: deps/.extracted
	curl -Lf https://www.sqlite.org/2025/sqlite-src-$(SQLITE).zip -o deps/sqlite-src-$(SQLITE).zip

deps/sqlite-src-$(SQLITE)/.extracted: deps/sqlite-src-$(SQLITE).zip
	cd deps && unzip -o sqlite-src-$(SQLITE).zip
	touch $@

build/lib/libsqlite3.a: deps/x86_64-linux-musl-cross/.extracted deps/sqlite-src-$(SQLITE)/.extracted build/.extracted
	cd deps/sqlite-src-$(SQLITE) &&\
		CFLAGS="-static"\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build --exec-prefix=$(ROOT_DIR)build --enable-static --disable-shared
	cd deps/sqlite-src-$(SQLITE) && ../../configure-wrapper.sh make -j4
	cd deps/sqlite-src-$(SQLITE) && ../../configure-wrapper.sh make install

libsqlite: build/lib/libsqlite3.a
.PHONY: libsqlite

# Bzip2
deps/bzip2-$(BZIP2).tar.gz: deps/.extracted
	curl -Lf https://sourceware.org/pub/bzip2/bzip2-$(BZIP2).tar.gz -o deps/bzip2-$(BZIP2).tar.gz

deps/bzip2-$(BZIP2)/.extracted: deps/bzip2-$(BZIP2).tar.gz
	tar -xzf deps/bzip2-$(BZIP2).tar.gz -C deps
	touch $@

build/lib/libbz2.a: deps/x86_64-linux-musl-cross/.extracted deps/bzip2-$(BZIP2)/.extracted
	cd deps/bzip2-$(BZIP2) &&\
		../../configure-wrapper.sh make -j4 "CFLAGS=-I$(ROOT_DIR)build/include -g0 -O2 -fno-align-functions -fno-align-jumps -fno-align-loops -fno-align-labels -Wno-error -fPIC"
	cd deps/bzip2-$(BZIP2) &&\
		../../configure-wrapper.sh make PREFIX=$(ROOT_DIR)build install

libbz2: build/lib/libbz2.a
.PHONY: libbz2

# Python3 building steps

deps/Python-$(PYTHON).tgz: deps/.extracted
	curl -Lf https://www.python.org/ftp/python/$(PYTHON)/Python-$(PYTHON).tgz -o deps/Python-$(PYTHON).tgz

deps/Python-$(PYTHON)/.extracted: deps/Python-$(PYTHON).tgz
	tar -xzf deps/Python-$(PYTHON).tgz -C deps
	touch $@

build/bin/python3: openssl libffi libsqlite liblzma readline zlib libbz2 ncurses deps/Python-$(PYTHON)/.extracted
	cp ./Setup deps/Python-$(PYTHON)/Modules/Setup.local
	cd deps/Python-$(PYTHON) &&\
		../../configure-wrapper.sh ./configure --prefix=$(ROOT_DIR)build\
			--exec-prefix=$(ROOT_DIR)build --enable-static --disable-shared\
  		--with-openssl=$(ROOT_DIR)build\
      --disable-test-modules\
      --with-ensurepip=install
	# ctypes has a dl opener that seems completely unnecessary
	sed -i "390s/.*/            pass/" ./deps/Python-$(PYTHON)/Lib/ctypes/__init__.py
	cd deps/Python-$(PYTHON) && ../../configure-wrapper.sh make -j8
	cd deps/Python-$(PYTHON) && ../../configure-wrapper.sh make install

python3: build/bin/python3
.PHONY: python3
