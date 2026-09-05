FROM ubuntu:24.04

# One image for every profile: gccfactory musl static/cross *and* host-built
# glibc reference*. A second libc container was how agents kept measuring
# the wrong baseline.
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    curl \
    git \
    xz-utils \
    unzip \
    tar \
    perl \
    tcl \
    jimsh \
    busybox \
    pkg-config \
    python3 \
    patch \
    rsync \
    flex \
    bison \
    meson \
    ninja-build \
    patchelf \
    make \
  && rm -rf /var/lib/apt/lists/*

# Ubuntu 24.04's golang is older than go.mod (1.23).
ARG GO_VERSION=1.23.8
ARG TARGETARCH=amd64
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64)  goarch=amd64; sha=45b87381172a58d62c977f27c4683c8681ef36580abecd14fd124d24ca306d3f ;; \
      arm64)  goarch=arm64; sha=9d6d938422724a954832d6f806d397cf85ccfde8c581c201673e50e634fdc992 ;; \
      *) echo "unsupported TARGETARCH=$TARGETARCH" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${goarch}.tar.gz" -o /tmp/go.tgz; \
    echo "$sha  /tmp/go.tgz" | sha256sum -c -; \
    tar -C /usr/local -xzf /tmp/go.tgz; \
    rm /tmp/go.tgz
ENV PATH=/usr/local/go/bin:$PATH

# qemu-user 11.1.1 (Alpine edge static-pie). Ubuntu 24.04 ships 8.2.2.
COPY scripts/docker/fetch-qemu-user.sh /tmp/fetch-qemu-user.sh
RUN chmod +x /tmp/fetch-qemu-user.sh \
 && /tmp/fetch-qemu-user.sh "$TARGETARCH" \
 && rm /tmp/fetch-qemu-user.sh

COPY scripts/docker/binfmt /usr/local/bin/binfmt
RUN chmod +x /usr/local/bin/binfmt

# Hermetic PATH is dirname(busybox). Put the applets configure looks up
# next to it so sqlite/openssl find tclsh, jimsh, perl, make, and GNU patch.
RUN busybox --install -s /bin \
 && for p in perl jimsh tclsh make patch; do \
      [ -e "/bin/$p" ] || ln -sf "/usr/bin/$p" "/bin/$p"; \
    done

# Bind-mounted repo is owned by the host user; this image runs as root.
# Without this, `git` and `go build`'s VCS probe die with exit 128.
RUN git config --global --add safe.directory '*'

WORKDIR /workspace
