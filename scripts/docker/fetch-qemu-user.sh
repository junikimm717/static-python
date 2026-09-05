#!/bin/sh
# Install qemu-user 11.1.1 into the Ubuntu image.
#
# Ubuntu 24.04's qemu-user is 8.2.2: too old for /proc/self/stat
# num_threads (>= 9.1) and still on the wrong side of the i386 SAHF/cc_op
# TCG bug (fixed in 11.0.4 / 11.1). Alpine's qemu-user packages are
# static-pie musl binaries with no shared-lib deps, so they run here.
# See .agents/skills/staticpy-traps/references/I386_QEMU_SAHF.md
set -eu

TARGETARCH=${1:-}
if [ -z "$TARGETARCH" ]; then
  TARGETARCH=$(uname -m)
fi
case "$TARGETARCH" in
  amd64|x86_64) HOST=x86_64 ;;
  arm64|aarch64) HOST=aarch64 ;;
  *)
    echo "fetch-qemu-user: unsupported TARGETARCH=$TARGETARCH" >&2
    exit 1
    ;;
esac

QEMU_VER=11.1.1-r0
BASE="https://dl-cdn.alpinelinux.org/alpine/edge/community/${HOST}"
ARCHES="x86_64 i386 aarch64 arm armeb ppc64 ppc64le s390x riscv64 riscv32 mips64"

sha256_for() {
  case "$1:$2" in
    x86_64:x86_64) echo 9ac8844d6f512305b9e9f10d8530728e9303a56b60904dae27e4de3565ad81a5 ;;
    x86_64:i386) echo 084b57e5b2341f617ba800f9d1f3223fdaf4706da1d23c9cffde8c75432f2184 ;;
    x86_64:aarch64) echo 2c1fa08187bcbec41e9e26d06fac5fc452f0c769d85b2e339d5642fed2fd95d0 ;;
    x86_64:arm) echo b94344f36f8052cccc66aac9d01b5a88f1cc63deb670b36bb4d488189bd975dd ;;
    x86_64:armeb) echo 68112678415813c7fce3dbbd7b757fa44cf292ec9467fbbed26c2a6bc4f71058 ;;
    x86_64:ppc64) echo 589788fa51bd046d0738c42a12c9525834ac8cf8ec1baed3655ddc883a0ed527 ;;
    x86_64:ppc64le) echo d359ff3c25a74377c7c9539d4bd9a5d6961b9f6b77427f6e28f12a686484d1d9 ;;
    x86_64:s390x) echo 191c170317a8c13b699530d01384e2e0c55164a585aa2d110cb44a2557aecaa4 ;;
    x86_64:riscv64) echo 15bc990366ff9829af54783f98f7ad193ecb88b0fb497534a02fb7ec6a1332e1 ;;
    x86_64:riscv32) echo b7e39f49f966847be9a7db7c6fe1b9b8c81815061cbbf6a01253c22c01767f67 ;;
    x86_64:mips64) echo c872fa0913ec3ad53d92378ea3f0fd29d64860d5b94205421646da89296c9ebc ;;
    aarch64:x86_64) echo 16c9e8c60eea294f65a50880e8a52ecb2dab9c9a397313fa6e97b53269721a19 ;;
    aarch64:i386) echo d8a92b40fb69028bb5eb8823103e5f1ff5d0c15d66660f1ede505078ffb34fe3 ;;
    aarch64:aarch64) echo cc79bdb3ad2318b58556c898db54357c5b7002dbf3a5d13032227eb23f15ad74 ;;
    aarch64:arm) echo 5bffbc8224d5da8547a5a620be791776417c06f1d48cddbae1d98337fe8d14ff ;;
    aarch64:armeb) echo ff5cb315ee5f60e782f6e0ade77e03af666cc8b0156f2f5c38a38c5e2d1c3dd1 ;;
    aarch64:ppc64) echo 0bae1e930dd8b263213a906315ae4bbee5b4c14655aa0cf9a7dfa8126fcf29b2 ;;
    aarch64:ppc64le) echo f187f3831fd9afb8ff6775e50f69f0f4ef057ef0b257ed0d197effbe502f0403 ;;
    aarch64:s390x) echo 146cd5f521415b0076f18c397ece93534677b3ae9458a06ffbe41e21feda3891 ;;
    aarch64:riscv64) echo dd6e0e9e11faa9bf7dd0f8d3c9203cf314223fe72712a46ef7b664571d6445e5 ;;
    aarch64:riscv32) echo 931aed3edd97b5496a161f02c0295b55e984da765b8f035fcfb568a8c009b56a ;;
    aarch64:mips64) echo 22ac397c3ee824ddd34d3886c45a1426cd24208233953d2b51d0aa3d46fca79d ;;
    *)
      echo "fetch-qemu-user: no sha256 for $1:$2" >&2
      exit 1
      ;;
  esac
}

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

for arch in $ARCHES; do
  apk="qemu-${arch}-${QEMU_VER}.apk"
  curl -fsSL -o "$workdir/$apk" "${BASE}/${apk}"
  echo "$(sha256_for "$HOST" "$arch")  $workdir/$apk" | sha256sum -c -
  tar -xzf "$workdir/$apk" -C "$workdir" usr/bin/qemu-"$arch"
  install -m 0755 "$workdir/usr/bin/qemu-$arch" /usr/bin/qemu-"$arch"
done

mkdir -p /usr/libexec/qemu-binfmt
for arch in $ARCHES; do
  ln -sfn /usr/bin/qemu-"$arch" /usr/libexec/qemu-binfmt/${arch}-binfmt-P
done
