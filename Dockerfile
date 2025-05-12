FROM alpine

# Extremely basic dockerfile for dev purposes.

RUN apk add --no-cache\
  git make curl tar perl meson ninja unzip\
  xz build-base flex bison ncurses rsync

WORKDIR /workspace
