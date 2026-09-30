---
name: staticpy-kit
description: >-
  Pack a benchmark kit (staticpy kit): several already-built interpreters plus
  a runner a quiet machine can unzip and measure. Use when the user asks for a
  kit, a bench tarball, staticpy kit, or to restamp kit.json / staticpy-bench.
---

# Benchmark kits

A kit is the comparison set in `[kit.default]` (or `--name smoke`):
relocatable prefixes under `python/<profile>/`, vendored
pyperformance/pyperf/setuptools, `kit.json`, and `bin/staticpy-bench`. On the
quiet box (no checkout, no git probe), `./run` is
`staticpy-bench bench --kit .`. The session copies `kit.json` onto
`manifest.kit` and promotes `python_version`, `kit_version`, `triple` and
`git_revision`, so the run can be retraced without the tarball.

```sh
docker compose exec -T spython sh -c 'cd /workspace && ./staticpy kit --name default --verify core'
```

Native only, as with `bench`. Output: `dist/out/kit/<name>/`. `--verify` makes
pack jobs match already-verified artifacts, so a broken interpreter is never
kitted.

## Commit, then stamp

The shim writes `HEAD`, plus `-dirty` if `git status --porcelain` is non-empty,
into `buildinfo.GitRevision`. That string is a kit key input and is copied into
`kit.json` and `bin/staticpy-bench`, so a kit packed mid-change says dirty
forever. Don't kit from a dirty tree if a commit is an option.

1. Finish the pin/recipe/docs work.
2. Commit; confirm `git status` is empty.
3. Rebuild `dist/.bin/staticpy` (or pass `STATICPY_GIT_REVISION` to the shim)
   so the stamp is the clean SHA, not a stale `.rev`.
4. `staticpy kit`.

Already packed dirty? Delete the published kit directory, rebuild the binary
from a clean tree, re-run `kit`. `./staticpy print git-revision` and
`kit.json`'s `git_revision` must be a bare SHA.

## After the quiet box measures

The runner's ETA uses the binary's embedded `eta_weights.json` (benches we
actually time), not previous `(arm, benchmark)` cells. When committing the
session under `benchmarks/`, check `skipped.json` and the timeline for names
missing from `eta_weights.json`; if a bench that used to skip now runs,
restamp (`staticpy` skill, **Bench ETA weights**). Skipping this doesn't break
the ETA — the new name is a local guess kept out of the pace — but the shape
stays stale.
