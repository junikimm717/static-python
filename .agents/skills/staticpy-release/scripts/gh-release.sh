#!/usr/bin/env bash
# Stage, publish, or verify a GitHub release of kit.default packed tarballs.
# Asset list is derived from [kit.default] + `staticpy print`, not from ls.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: gh-release.sh <check|stage|publish|verify> [options]

  check     require every expected tarball; print the plan
  stage     write staging dir + SHA256SUMS + MANIFEST (implies check)
  publish   gh release create + upload (implies stage)
  verify    compare GitHub assets to MANIFEST

Options:
  --repo DIR          repository root (default: walk up from cwd)
  --staging DIR       staging directory
  --tag TAG           override tag (default: python-<cpython-version>)
  --target BRANCH     git ref for the release (default: master)
  --dry-run           print gh commands; do not create or upload
  --skip-binaries     do not retarget the binaries placeholder notes
  --clobber           allow replacing an existing release of the same tag
EOF
}

die() { echo "gh-release: $*" >&2; exit 1; }

cmd=
REPO=
STAGING=
TAG=
TARGET=master
DRY=0
SKIP_BINARIES=0
CLOBBER=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    check|stage|publish|verify) cmd=$1; shift ;;
    --repo) REPO=$2; shift 2 ;;
    --staging) STAGING=$2; shift 2 ;;
    --tag) TAG=$2; shift 2 ;;
    --target) TARGET=$2; shift 2 ;;
    --dry-run) DRY=1; shift ;;
    --skip-binaries) SKIP_BINARIES=1; shift ;;
    --clobber) CLOBBER=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done
[[ -n $cmd ]] || { usage >&2; exit 2; }

find_repo() {
  local d
  for d in "$PWD" "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"; do
    while [[ $d != / ]]; do
      [[ -x $d/staticpy && -f $d/src/staticpy/internal/config/defaults/bench.toml ]] && {
        echo "$d"
        return
      }
      d=$(dirname "$d")
    done
  done
  die "not inside a static-python repo (no ./staticpy)"
}

[[ -n $REPO ]] || REPO=$(find_repo)
REPO=$(cd "$REPO" && pwd)
cd "$REPO"

STATICPY=$REPO/staticpy
BENCH=$REPO/src/staticpy/internal/config/defaults/bench.toml
[[ -x $STATICPY ]] || die "missing $STATICPY"
[[ -f $BENCH ]] || die "missing $BENCH"

VERSION=$("$STATICPY" print python-version)
HOST=$("$STATICPY" print host)
DIST=$("$STATICPY" print dist)
# targets-all is space-separated
# shellcheck disable=SC2207
TARGETS=($("$STATICPY" print targets-all))
[[ ${#TARGETS[@]} -gt 0 ]] || die "staticpy print targets-all was empty"
[[ -n $VERSION && -n $HOST && -n $DIST ]] || die "staticpy print returned empty fields"

readarray -t ARMS < <(python3 - "$BENCH" <<'PY'
import sys, tomllib
from pathlib import Path
data = tomllib.loads(Path(sys.argv[1]).read_text())
arms = data["kit"]["default"]["arms"]
for a in arms:
    print(a)
PY
)
[[ ${#ARMS[@]} -gt 0 ]] || die "kit.default.arms is empty"

STATIC_ARMS=()
REF_ARMS=()
for a in "${ARMS[@]}"; do
  if [[ $a == reference || $a == reference-* ]]; then
    REF_ARMS+=("$a")
  else
    STATIC_ARMS+=("$a")
  fi
done

[[ -n $TAG ]] || TAG=python-$VERSION
[[ -n $STAGING ]] || STAGING=/tmp/staticpy-release-$VERSION

# Collect expected (logical_name -> real_path). Fail closed.
declare -A FILES=()
MISSING=()
AMBIGUOUS=()

expect_file() {
  local name=$1 path=$2
  if [[ ! -f $path ]]; then
    MISSING+=("$name ($path)")
    return
  fi
  local sz
  sz=$(stat -c%s "$path")
  if [[ $sz -lt 1048576 ]]; then
    MISSING+=("$name too small (${sz}B) at $path")
    return
  fi
  FILES[$name]=$path
}

pack_name() {
  local profile=$1 triple=$2
  echo "python-${VERSION}-${triple}-${profile}.tar.gz"
}

for profile in "${STATIC_ARMS[@]}"; do
  for triple in "${TARGETS[@]}"; do
    name=$(pack_name "$profile" "$triple")
    expect_file "$name" "$DIST/out/$profile/$triple/$name"
  done
done

for profile in "${REF_ARMS[@]}"; do
  name=$(pack_name "$profile" "$HOST")
  shopt -s nullglob
  local_dirs=("$DIST/out/$profile/${HOST}_"[0-9a-f][0-9a-f]*)
  shopt -u nullglob
  # keep only dirs whose suffix is exactly _ + 12 hex
  hits=()
  for d in "${local_dirs[@]+"${local_dirs[@]}"}"; do
    base=$(basename "$d")
    suf=${base#"${HOST}_"}
    if [[ $suf =~ ^[0-9a-f]{12}$ && -d $d ]]; then
      hits+=("$d")
    fi
  done
  if [[ ${#hits[@]} -eq 0 ]]; then
    leftover=$DIST/out/$profile/$HOST/$name
    MISSING+=("$name (need ${HOST}_<12 hex>/; leftover unsuffixed is not used: $leftover)")
    continue
  fi
  if [[ ${#hits[@]} -gt 1 ]]; then
    AMBIGUOUS+=("$profile: ${hits[*]}")
    continue
  fi
  expect_file "$name" "${hits[0]}/$name"
done

expected=$(( ${#STATIC_ARMS[@]} * ${#TARGETS[@]} + ${#REF_ARMS[@]} ))
found=${#FILES[@]}

print_plan() {
  echo "repo     $REPO"
  echo "dist     $DIST"
  echo "version  $VERSION"
  echo "host     $HOST"
  echo "tag      $TAG"
  echo "target   $TARGET"
  echo "static   ${#STATIC_ARMS[@]} arms × ${#TARGETS[@]} triples"
  echo "ref      ${#REF_ARMS[@]} arms (hostcc-suffixed $HOST only)"
  echo "expect   $expected tarballs"
  echo "found    $found"
  if [[ ${#MISSING[@]} -gt 0 ]]; then
    echo "MISSING"
    printf '  %s\n' "${MISSING[@]}"
  fi
  if [[ ${#AMBIGUOUS[@]} -gt 0 ]]; then
    echo "AMBIGUOUS (two hostcc prefixes; do not guess)"
    printf '  %s\n' "${AMBIGUOUS[@]}"
  fi
}

if [[ $cmd == check ]]; then
  print_plan
  [[ ${#MISSING[@]} -eq 0 && ${#AMBIGUOUS[@]} -eq 0 && $found -eq $expected ]] \
    || die "matrix incomplete"
  echo "OK"
  exit 0
fi

[[ ${#MISSING[@]} -eq 0 && ${#AMBIGUOUS[@]} -eq 0 && $found -eq $expected ]] \
  || { print_plan; die "matrix incomplete"; }

stage() {
  rm -rf "$STAGING"
  mkdir -p "$STAGING"
  local name
  for name in "${!FILES[@]}"; do
    ln -s "${FILES[$name]}" "$STAGING/$name"
  done
  (
    cd "$STAGING"
    sha256sum python-*.tar.gz | sort -k2 > SHA256SUMS
    {
      echo "tag $TAG"
      echo "version $VERSION"
      echo "host $HOST"
      echo "commit $(git -C "$REPO" rev-parse HEAD)"
      echo "static ${STATIC_ARMS[*]}"
      echo "targets ${TARGETS[*]}"
      echo "reference ${REF_ARMS[*]}"
      echo "count $expected"
    } > MANIFEST
  )
  echo "staged $STAGING ($expected tarballs + SHA256SUMS)"
}

write_notes() {
  local notes=$1
  local sha
  sha=$(git -C "$REPO" rev-parse --short HEAD)
  cat >"$notes" <<EOF
CPython **${VERSION}** from \`${sha}\` on \`${TARGET}\`.

**${expected} tarballs:** ${#TARGETS[@]} triples × ${#STATIC_ARMS[@]} static profiles, plus ${#REF_ARMS[@]} host-built reference interpreters (${HOST}). \`SHA256SUMS\` is attached.

Filename: \`python-${VERSION}-<triple>-<profile>.tar.gz\`.

## Static (fully static musl)

$(printf '%s\n' "${STATIC_ARMS[@]}" | sed 's/^/- `/;s/$/`/')

Triples: $(printf '%s ' "${TARGETS[@]}")

These are relocatable prefixes. A static interpreter cannot \`dlopen\` a C extension.

## Reference (host gcc, dynamically linked)

$(printf '%s\n' "${REF_ARMS[@]}" | sed 's/^/- `/;s/$/`/')

Host-built prefixes are keyed on hostcc. The filename still uses the staticpy target slug.
EOF
}

if [[ $cmd == stage ]]; then
  print_plan
  stage
  exit 0
fi

if [[ $cmd == publish ]]; then
  print_plan
  stage
  notes=$STAGING/NOTES.md
  write_notes "$notes"
  origin=$(git -C "$REPO" remote get-url origin)
  gh_repo=${origin#git@github.com:}
  gh_repo=${gh_repo#https://github.com/}
  gh_repo=${gh_repo%.git}
  create=(gh release create "$TAG" --repo "$gh_repo" --target "$TARGET" --title "CPython ${VERSION} — static musl + host reference" --notes-file "$notes")
  if [[ $DRY -eq 1 ]]; then
    printf '%q ' "${create[@]}"; echo
    echo "would upload $expected tarballs + SHA256SUMS from $STAGING"
    exit 0
  fi
  if gh release view "$TAG" --repo "$gh_repo" >/dev/null 2>&1; then
    [[ $CLOBBER -eq 1 ]] || die "release $TAG already exists (pass --clobber to replace assets)"
  else
    "${create[@]}"
  fi
  (
    cd "$STAGING"
    gh release upload "$TAG" SHA256SUMS --repo "$gh_repo" --clobber
    batch=()
    n=0
    for f in python-*.tar.gz; do
      batch+=("$f")
      n=$((n + 1))
      if [[ ${#batch[@]} -eq 10 ]]; then
        echo "uploading $n / $expected"
        gh release upload "$TAG" "${batch[@]}" --repo "$gh_repo" --clobber
        batch=()
      fi
    done
    if [[ ${#batch[@]} -gt 0 ]]; then
      echo "uploading $n / $expected"
      gh release upload "$TAG" "${batch[@]}" --repo "$gh_repo" --clobber
    fi
  )
  if [[ $SKIP_BINARIES -eq 0 ]]; then
    gh release edit binaries --repo "$gh_repo" --notes "Superseded by [${TAG}](https://github.com/${gh_repo}/releases/tag/${TAG}): ${expected} interpreter tarballs plus SHA256SUMS." \
      || echo "gh-release: warning: could not update binaries placeholder" >&2
  fi
  echo "https://github.com/${gh_repo}/releases/tag/${TAG}"
  exit 0
fi

if [[ $cmd == verify ]]; then
  [[ -f $STAGING/MANIFEST ]] || die "no $STAGING/MANIFEST (run stage/publish first)"
  origin=$(git -C "$REPO" remote get-url origin)
  gh_repo=${origin#git@github.com:}
  gh_repo=${gh_repo#https://github.com/}
  gh_repo=${gh_repo%.git}
  mapfile -t remote < <(gh api "repos/${gh_repo}/releases/tags/${TAG}" --jq '[.assets[].name] | sort | .[]')
  mapfile -t local < <(cd "$STAGING" && ls SHA256SUMS python-*.tar.gz | sort)
  echo "remote ${#remote[@]}  local ${#local[@]}"
  extra=$(comm -13 <(printf '%s\n' "${local[@]}") <(printf '%s\n' "${remote[@]}") || true)
  miss=$(comm -23 <(printf '%s\n' "${local[@]}") <(printf '%s\n' "${remote[@]}") || true)
  [[ -z $miss ]] || { echo "LOCAL_ONLY"; echo "$miss"; }
  [[ -z $extra ]] || { echo "REMOTE_ONLY"; echo "$extra"; }
  extra_n=$(printf '%s\n' "$extra" | grep -c . || true)
  miss_n=$(printf '%s\n' "$miss" | grep -c . || true)
  sizes=$(gh api "repos/${gh_repo}/releases/tags/${TAG}" --jq '{n: (.assets|length), min: ([.assets[].size]|min), max: ([.assets[].size]|max), sum: ([.assets[].size]|add)}')
  echo "sizes $sizes"
  [[ ${extra_n:-0} -eq 0 && ${miss_n:-0} -eq 0 ]] || die "asset set mismatch"
  echo "OK $TAG"
  exit 0
fi

die "unreachable"
