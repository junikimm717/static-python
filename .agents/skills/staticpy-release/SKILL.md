---
name: staticpy-release
description: >-
  Publish a GitHub release of the packed static and reference interpreter
  tarballs from dist/out. Use when the user asks to create or refresh a GitHub
  release, attach the 70 (or current matrix) tarballs, update the binaries
  placeholder, or ship python-X.Y.Z artifacts.
---

# GitHub release of packed interpreters

Do not invent the asset list. Run the scripts in `scripts/` of this skill.
They read `[kit.default]` and `staticpy print`, then fail if a cell is missing
or ambiguous.

## When this is done

A GitHub release tagged `python-<cpython-version>` on `master` whose assets
are exactly:

- one tarball per static kit arm × every `targets-all` triple
- one tarball per `reference*` kit arm, from the **hostcc-suffixed** prefix
  only (`<triple>_<12 hex>`)
- `SHA256SUMS`

No kit tarball. No unsuffixed `dist/out/reference*/<triple>/` leftover. Asset
count matches the script's `MANIFEST`. Every `.tar.gz` is a real gzip
(>1 MiB), not a dangling symlink.

## Procedure

1. Confirm the matrix is on disk (do not start a sweep from this skill):

   ```sh
   .agents/skills/staticpy-release/scripts/gh-release.sh check
   ```

   If it prints `MISSING` or `AMBIGUOUS`, stop. Those cells are not a release
   problem; they are a build problem.

2. Stage (deterministic names + checksums):

   ```sh
   .agents/skills/staticpy-release/scripts/gh-release.sh stage
   ```

   Staging is `/tmp/staticpy-release-<version>/` unless `--staging` is set.

3. Create the release and upload. Tag **must** be `python-<version>` (plain
   `3.13.13` is a GitHub 422). Target `master` (this repo has no `main`):

   ```sh
   .agents/skills/staticpy-release/scripts/gh-release.sh publish
   ```

   `--dry-run` prints the `gh` commands and does not create anything.

4. Verify:

   ```sh
   .agents/skills/staticpy-release/scripts/gh-release.sh verify
   ```

5. Point the empty-or-stale `binaries` tag at the new release (the README
   still links there). The publish step does this unless `--skip-binaries`.

Do not commit `dist/`. Do not upload twice (same filename on two releases).
Do not rewrite an existing `python-<version>` tag unless the user explicitly
asks to replace that release.

## Which tarball is "the" reference

Host-built prefixes are keyed on hostcc (`pack.go` / `hostPublishSuffix`).
Two machines that share `dist/` still must not write the same prefix.

| path | use |
|---|---|
| `dist/out/reference/<triple>_xxxxxxxxxxxx/*.tar.gz` | yes (exactly one such dir) |
| `dist/out/reference/<triple>/*.tar.gz` | no — leftover unsuffixed slug |
| two `_<hex>` dirs for one profile | stop — toolchain mix |

## Notes body

`publish` writes a notes file from the same lists as the assets. Edit only
if the user asked for different prose. Keep: CPython version, commit SHA,
static arms × triples, reference arms, `failed=0` only if you actually
checked verify reports this run.

## Pitfalls (already paid for)

- `gh release create 3.13.13` → `tag_name is not a valid tag`. Use
  `python-3.13.13`.
- `gh pr edit` / some GraphQL calls die on deprecated Projects; release
  create/upload via `gh release` is fine.
- `gh release upload` from a staging dir of **symlinks** is OK (`open`
  follows). Check remote `size` anyway: min should be the SHA256SUMS
  (~8 KiB), max a reference tarball (~60 MiB), not 17 bytes.
- `gh release view` without `--repo` fails if cwd is `/tmp/...`.
- Older `gh` has no `--latest`; omit it.
- A leftover `dist/out/reference*/<triple>/` (no `_<hex>`) is not an
  asset. The check script refuses it; do not "fix" that by uploading it.

## After

Return the release URL. Mention the `binaries` redirect if it was updated.
