# benchmarks/

Committed `./staticpy bench` sessions. CI never measures; GitHub Pages is
built from this tree by `./manage_benchmarks.py site`. Do not commit `_site`.
Only measured sessions belong here. The manager's tests keep a synthetic
session under `tests/fixtures/`.

## What we accept

One directory per run, named `<stamp>-<arch>/` (UTC `YYYYMMDDThhmmssZ`, then
the machine arch). Protocol **2**. These seven files, nothing else:

```
manifest.json   protocol, suite.name (pyperformance|micro), baseline, identities
env.json        kernel, cpu, memory, affinity, fingerprint
report.json     rows + geomean_vs_baseline (>1 is faster)
report.md
report.html
skipped.json    list, possibly empty
timeline.jsonl  one event per measurement
```

`import` copies only that set and refuses a directory missing `manifest.json`
or `report.json`. `.gitignore` is the same allowlist, so a whole-session dump
(venv, raw, logs, quiet.jsonl, …) will not be committed.

Required to compare later: `manifest.protocol == 2`, a host `fingerprint` /
`fingerprint_sha256`, `python_version`, `git_revision` of the packed
experiment (from `kit.json` on a kit run), and each interpreter as
`{label, binary_sha256, artifact_key, factors, packages}`. A kit session
also stores the full `kit.json` under `manifest.kit` so pins, the triple,
and pack-time arm hashes survive without the tarball. A profile name is
not a stable description of the binary.

## Paths in

Prefer `import`. It checks the protocol, copies the seven files, and refreshes
`index.json`. `list` / `verify` / `site` rescan directories that have a
manifest, so a dump still publishes if the seven files are present.

**Local (this repo, native machine)**

```sh
./staticpy bench --interp static --interp reference --baseline reference
./manage_benchmarks.py import dist/bench/<stamp>-<arch>
```

**Local dump** (same machine, skip the copy step)

```sh
cp -a dist/bench/<stamp>-<arch> benchmarks/
./manage_benchmarks.py verify
```

**Remote / quiet box (kit)** — unpack a `staticpy kit` tarball, run it, bring
the session back, import on a clone of this repo:

```sh
# quiet box
./run
# any machine with this repo
scp -r quiet:results/<stamp>-<arch> /tmp/
./manage_benchmarks.py import /tmp/<stamp>-<arch>
```

`scp`/`rsync` of the whole session into `benchmarks/<stamp>-<arch>/` is the
same as a local dump. Results on the kit live under `DIR/results/`, not
`dist/bench/`.

Then commit `benchmarks/` and push. Pages rebuilds from that tree on `master`.

```sh
./manage_benchmarks.py list
./manage_benchmarks.py show <stamp>-<arch>
./manage_benchmarks.py verify
./manage_benchmarks.py delete <stamp>-<arch> --yes
```

`--force` overwrites an existing id. `--allow-stale-protocol` imports a
session whose `protocol` is not 2; those stay in the tree and the site badges
them. `delete` refuses anything still under `fixtures/` unless `--fixtures`
is also passed.
