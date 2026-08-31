# benchmarks/

Committed copies of `./staticpy bench` sessions. Numbers in this tree were
copied from `dist/bench/` on a real machine. Fixtures under `fixtures/` are
synthetic demo data, not published scores.

CI never runs `./staticpy bench` or `./staticpy build`. GitHub Pages is
generated from these files by `./manage_benchmarks.py site`.

## How a run gets here

```sh
./staticpy bench --interp reference --interp default
./manage_benchmarks.py import dist/bench/<stamp>-<arch>
```

Import copies `manifest.json`, `env.json`, `report.json`, `report.md`,
`report.html`, `skipped.json`, and `timeline.jsonl` if present. The
manifest includes a host `fingerprint` (cpu, microcode, smt, kernel
cmdline, vulnerabilities, memory, …) and `fingerprint_sha256`. It does not
copy `venv/`, `raw/`, or `logs/`. The destination name is the session
directory's basename (`<stamp>-<arch>/`).

`--force` overwrites an existing id. Import refuses a `protocol` other than
the current contract unless `--allow-stale-protocol` is passed.

```sh
./manage_benchmarks.py list
./manage_benchmarks.py show <stamp>-<arch>
./manage_benchmarks.py verify
./manage_benchmarks.py site --out _site
```

## Protocol

`protocol` is the session schema and measurement contract, defined as
`CURRENT_PROTOCOL` in `manage_benchmarks.py` and `bench.Protocol` in
`src/staticpy/internal/bench/protocol.go`. It is currently **1**. A bump
means previously recorded numbers cannot be compared to new ones (wrong
reduction, silently dropped arms, a contaminated pin). A pyperformance pin
bump is a suite change, not a protocol bump.

Stale-protocol runs stay in the tree. `list` and the site badge them.
They are not deleted automatically.

## Purge

```sh
./manage_benchmarks.py delete <stamp>-<arch> --yes
```

That removes `benchmarks/<name>/` and rewrites `index.json`. Anything under
`fixtures/` is refused unless `--fixtures` is also passed: those directories
exist so tests and an empty Pages site have something to render.

Do not commit `venv/`, `raw/`, or `logs/` even if a session still has them
on disk; `.gitignore` already drops those paths under `benchmarks/`.

## Fixtures

`fixtures/20000101T000000Z-x86_64/` is a tiny synthetic session. Its stamp
is `20000101T…`, `env.json`'s `cpu_model` contains `FIXTURE`, and the Pages
index puts it under a **Fixture / demo** heading. Treat it as layout glue,
not a score.
