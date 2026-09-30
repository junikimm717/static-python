---
name: staticpy-release
description: >-
  Publish a GitHub release of the packed static and reference interpreter
  tarballs from dist/out. Use when the user asks to create or refresh a GitHub
  release, attach the 70 (or current matrix) tarballs, update the binaries
  placeholder, or ship python-X.Y.Z artifacts.
---

# GitHub release of packed interpreters

Don't invent the asset list: use `scripts/gh-release.sh` in this skill. It
reads `[kit.default]` and `staticpy print` and fails on a missing or ambiguous
cell.

## Done when

A release tagged `python-<cpython-version>` on `master` with exactly:

- one tarball per static kit arm × every `targets-all` triple
- one tarball per `reference*` kit arm, from the **hostcc-suffixed** prefix
  only (`<triple>_<12 hex>`)
- `SHA256SUMS`

No kit tarball, no unsuffixed `dist/out/reference*/<triple>/` leftover. Asset
count matches the script's `MANIFEST`. Every `.tar.gz` is a real gzip
(>1 MiB), not a dangling symlink.

## Procedure

```sh
S=.agents/skills/staticpy-release/scripts/gh-release.sh
$S check     # MISSING or AMBIGUOUS: stop — that's a build problem; don't start a sweep here
$S stage     # deterministic names + checksums in /tmp/staticpy-release-<version>/ (or --staging)
$S publish   # --dry-run prints the gh commands only
$S verify
```

- Tag **must** be `python-<version>` (plain `3.13.13` is a GitHub 422), target
  `master` (there is no `main`).
- `publish` also points the `binaries` tag (linked from README) at the new
  release unless `--skip-binaries`.
- Don't commit `dist/`. Don't upload the same filename to two releases. Don't
  rewrite an existing `python-<version>` tag unless the user asks to replace
  it.

## Which tarball is "the" reference

Host-built prefixes are keyed on hostcc (`pack.go` / `hostPublishSuffix`) so
two machines sharing `dist/` never write the same prefix.

| path | use |
|---|---|
| `dist/out/reference/<triple>_xxxxxxxxxxxx/*.tar.gz` | yes (exactly one such dir) |
| `dist/out/reference/<triple>/*.tar.gz` | no — leftover unsuffixed slug |
| two `_<hex>` dirs for one profile | stop — toolchain mix |

## Notes body

`publish` generates notes from the same lists as the assets; edit only if the
user wants different prose. Keep: CPython version, commit SHA, static arms ×
triples, reference arms, and `failed=0` only if you checked verify reports
this run.

## Pitfalls

- `gh release create 3.13.13` → `tag_name is not a valid tag`; use
  `python-3.13.13`.
- `gh pr edit` / some GraphQL calls die on deprecated Projects; `gh release`
  create/upload is fine.
- Uploading from a staging dir of **symlinks** works, but check remote sizes:
  min ~8 KiB (SHA256SUMS), max ~60 MiB (a reference tarball), never 17 bytes.
- `gh release view` without `--repo` fails when cwd is `/tmp/...`.
- Older `gh` lacks `--latest`; omit it.
- An unsuffixed `dist/out/reference*/<triple>/` is not an asset; the check
  refuses it — don't "fix" that by uploading it.

Finish by returning the release URL, and mention the `binaries` redirect if it
moved.
