---
name: staticpy-kit
description: >-
  Pack a benchmark kit (staticpy kit): several already-built interpreters plus
  a runner a quiet machine can unzip and measure. Use when the user asks for a
  kit, a bench tarball, staticpy kit, or to restamp kit.json / staticpy-bench.
---

# Benchmark kits

A kit is not one Python. It is the comparison set in `[kit.default]` (or
`--name smoke`): relocatable prefixes under `python/<profile>/`, vendored
pyperformance/pyperf/setuptools, `kit.json`, and `bin/staticpy-bench`.
`./run` on the quiet box is `staticpy-bench bench --kit .`.

```sh
docker compose exec -T spython sh -c 'cd /workspace && ./staticpy kit --name default --verify core'
```

Native only, same reason as `bench`. The tarball lands under
`dist/out/kit/<name>/`. `--verify` makes pack jobs match already-verified
artifacts; a broken interpreter is never kitted.

## Commit, then stamp

Whenever possible, commit the work and then stamp the kit at a non-dirty
version. Do not kit from a dirty tree if a commit is an option.

The `./staticpy` shim writes `HEAD` plus `-dirty` (when
`git status --porcelain` is non-empty) into `buildinfo.GitRevision`. That
string is a kit key input and is copied into `kit.json` and
`bin/staticpy-bench`. A kit packed during an uncommitted upgrade advertises
a dirty static-python revision forever, until you restamp.

1. Finish the pin/recipe/docs work.
2. Commit. Confirm `git status` is empty.
3. Rebuild `dist/.bin/staticpy` (or pass `STATICPY_GIT_REVISION` to the
   shim) so the stamp is the clean SHA, not an empty `.rev` leftover.
4. Then `staticpy kit`.

If you already packed while dirty, restamp: delete the published kit
directory, rebuild the binary from a clean tree, and run `kit` again.
`./staticpy print git-revision` and `kit.json`'s `git_revision` must be a
bare SHA, not `*-dirty`.
