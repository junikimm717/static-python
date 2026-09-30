# benchmarks/

Committed `./staticpy bench` sessions, measured only (the manager's tests keep
a synthetic one under `tests/fixtures/`). CI never measures. GitHub Pages is
built from this tree by `./manage_benchmarks.py site`; do not commit `_site`.

## What we accept

One directory per run, `<stamp>-<arch>/` (UTC `YYYYMMDDThhmmssZ`, then machine
arch), protocol **2**, exactly these seven files:

```
manifest.json   protocol, suite.name (pyperformance|micro), baseline, identities
env.json        kernel, cpu, memory, affinity, fingerprint
report.json     rows + geomean_vs_baseline (>1 is faster)
report.md
report.html
skipped.json    list, possibly empty
timeline.jsonl  one event per measurement
```

`import` copies only these and refuses a directory without `manifest.json` or
`report.json`. `.gitignore` is the same allowlist, so venv/raw/logs/quiet.jsonl
in a whole-session dump never get committed.

A session must carry, to be comparable later: `manifest.protocol == 2`, host
`fingerprint` / `fingerprint_sha256`, `python_version`, the experiment's
`git_revision` (from `kit.json` on a kit run), and each interpreter as
`{label, binary_sha256, artifact_key, factors, packages}` (a profile name does
not identify a binary). Kit sessions also keep the full `kit.json` under
`manifest.kit`, so pins, triple and pack-time arm hashes survive without the
tarball.

## Adding a session

Prefer `import`: it checks the protocol, copies the seven files and refreshes
`index.json`. `list` / `verify` / `site` rescan any directory with a manifest,
so a raw copy also publishes if the seven files are present.

```sh
# local, native machine
./staticpy bench --interp static --interp reference --baseline reference
./manage_benchmarks.py import dist/bench/<stamp>-<arch>

# local dump instead of import
cp -a dist/bench/<stamp>-<arch> benchmarks/
./manage_benchmarks.py verify

# kit on a quiet box: results land in DIR/results/, not dist/bench/
./run
scp -r quiet:results/<stamp>-<arch> /tmp/          # then, on a repo clone:
./manage_benchmarks.py import /tmp/<stamp>-<arch>
```

Copying a whole session into `benchmarks/<stamp>-<arch>/` is the same as a
local dump. Then commit `benchmarks/` and push; Pages rebuilds on `master`.

```sh
./manage_benchmarks.py list
./manage_benchmarks.py show <stamp>-<arch>
./manage_benchmarks.py verify
./manage_benchmarks.py delete <stamp>-<arch> --yes
```

- `--force` overwrites an existing id.
- `--allow-stale-protocol` imports a non-2 protocol; it stays and the site
  badges it.
- `delete` refuses anything under `fixtures/` without `--fixtures`.
