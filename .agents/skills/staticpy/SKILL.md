---
name: staticpy
description: Work on this repo's Go build system (src/staticpy) — the job DAG, the content-addressed artifact store, where a version/flag/library/package is changed, where artifacts and per-command logs land, how to re-enter a failed job's exact environment, and which invariants must never be broken. Start here for ANY question about this repo — it has a lookup table that answers most of them without scanning the codebase. Use whenever touching src/staticpy or config/*.toml, running ./staticpy, adding a job family or a package, or working out why a build did or did not rebuild.
---

# staticpy

The build system owns the job in **data**: `config/*.toml` holds versions,
checksums, configure args, per-target quirks and flag profiles. Each job's key
hashes its inputs plus its dependencies' keys, so an edit rebuilds exactly
what depends on it.

`./staticpy help` and each command's `Long:` in `internal/cli/*.go` are the
authoritative user docs. Read them before writing docs; update them when
behaviour changes.

```
staticpy                 sh shim: provisions the environment, nothing else;
                         rebuilds the binary when src/staticpy changes
config/*.toml            sources, packages, targets, profiles, bundles, tests
src/staticpy/internal/
  config/                loader, layering, scope resolution, validation
  sources/               fetch + sha256 + extract + patch + content-anchored edits
  core/                  Job, Env, Merkle keys, flock leases, atomic publish, Runner
  recipe/                the job families; recipe.go holds the DAG
  gen/                   Setup.local, staticapi symbols
  ensure/                verification: ELF identity, imports, CPython's suite
  assets/files/          go:embed'd: per-target pyconfig fragments, Setup, probe
  cli/                   commands; the `Long:` fields are the real docs
dist/                    everything generated; gitignored, safe to delete
```

## Lookup table

| question | answer |
|---|---|
| commands and flags | `./staticpy help`, `help <cmd>`, `help layout`, `help targets` — authoritative |
| bump a pinned version | `config/sources.toml` (version + file + urls + topdir + **sha256 in the same edit**). CPython minor or matrix-wide sweep: `staticpy-bump` **Running a matrix upgrade** (fuzz first, hunt the class, full matrix `failed=0`, pack from a clean tree) |
| a package's configure flags | `config/packages.toml`. Only *decisions*; `--prefix`, `--exec-prefix`, `--host` are injected by the recipe (absolute or triple-derived) |
| a compiler/linker flag | `config/profiles.toml`. Profile-wide, then scoped: `deps`, `deps.<pkg>`, `python`, `pyhost`. A scoped change rebuilds only what it reaches |
| add a native library | `[source.X]` in `sources.toml` + `[package.X]` in `packages.toml` (`build` = autotools\|openssl\|make\|sources, `needs`, `provides`). `Deps` picks it up; `sysroot` composes it |
| add a Python package / bundle | `config/bundles.toml`: `[pkg.X]` with sdist + sha256 + `[[pkg.X.modules]]`, then a `[bundle.Y]` naming it. None defined yet. No dlopen, so a C module arrives at link time or not at all |
| add an architecture | a `config/targets.toml` row **plus** `internal/assets/files/pyconfig/<triple>-patches.h` — `staticpy-add-target` |
| where artifacts land | `dist/artifacts/<slug>/` (job outputs incl. interpreter prefixes); `dist/out/<profile>/<triple>/` (tarballs); `dist/src/` (verified tarballs); `dist/srctrees/` |
| logs for a failed job | `dist/logs/jobs/<slug>/latest/`: `NNN-<step>.log` per command plus `commands.sh`; `staticpy logs <slug> --failed` |
| shell in a job's exact env | `staticpy shell <slug>` (`--step NAME`, or `--print` for the env). Recovered from the recorded attempt, works long after the process is gone |
| what invalidates a rebuild | the Merkle key: `KeyInputs` (recipe version, source sha256s, decision flags, triples, resolved profile) plus every dep's key. Not timestamps. `staticpy status` calls the difference `stale` |
| what a build would do now | `staticpy status [--todo]` with the same `--verify`/`--pack` you'd pass to `build`, or it's a different plan |
| what this machine lacks | `staticpy doctor`. `perl` is the one irreducible host dep (OpenSSL's Configure); `patch` applies the pinned diffs |
| a resolved value for a script | `staticpy print <key>`: `python-version`, `python-abi`, `host`, `targets{,-all,-proven}`, `dist`, `recipe-version`, `version:<src>`, `sha256:<src>` |
| what flags resolve to | `staticpy config show [--profile N] [--scope S]` (names each layer's file) |
| pack a benchmark kit | `staticpy-kit` — commit first, stamp from a clean tree so `kit.json` isn't `*-dirty` |
| ship tarballs to GitHub | `staticpy-release` — `scripts/gh-release.sh check\|stage\|publish\|verify`; don't invent the asset list |
| a built interpreter misbehaves | `staticpy-traps`, symptom first. Read **Do not overfit the last failure** before adding an `[expect]` or one-package stanza |
| pyref advertised LTO/PGO, DSO has none | CPython 3.13 matches `*gcc*` against `$CC`. `hostcc.Find` prefers `gcc` over `cc`; after configure the Makefile must contain `-flto` / `PGO_PROF_USE_FLAG`. See staticpy-traps |
| a verify method failed on one triple | reproduce on native x86_64 static and a second qemu before writing `[expect.<triple>]` |
| what a bench session writes | always seven files, any `--suite`: `manifest.json`, `env.json`, `report.json`, `report.md`, `report.html`, `skipped.json`, `timeline.jsonl`. `suite.name` is `pyperformance` or `micro`. A kit session copies `kit.json` onto `manifest.kit` and promotes `python_version` / `git_revision` / `triple`. `venv/`, `raw/`, `logs/` stay local |
| bench ETA weights | `src/staticpy/internal/bench/eta_weights.json`: typical seconds per **benchmark name we actually time** (not all of pyperformance, not last night's kit). Restamp once a new name has been timed (**Bench ETA weights** below). A missing name must not break the ETA; it's a local guess kept out of the pace |

## The DAG

From `internal/recipe/recipe.go`'s package comment:

```
srctree:<pkg>-<ver>        extracted + patched source          (internal/sources)
probe:<T>                  target ABI sizes -> config.site
dep:<prof>:<T>:<pkg>       one native library, own prefix
sysroot:<prof>:<T>         the -I/-L view composed from dep artifacts
pyhost:<ver>               static-musl CPython that runs on the build machine
pynative:<prof>:<T>        the shipped interpreter, host == target
pycross:<prof>:<H>:<T>     the shipped interpreter, host != target
pack:<prof>:<T>            the distributable tarball
kit:<name>:<T>             several packed prefixes plus a bench runner
```

- **`pycross` depends on `pyhost`, never `pynative`.** A cross build only needs
  a same-version interpreter to freeze bytecode; gating on a full PGO host
  build once cost an hour per cross target. `pyhost` uses the fixed
  `bootstrap` profile and a minimal module set.
- **Every dep installs into its own prefix; `sysroot` is only the view.** A
  shared accumulator once let a stale `libz.a` survive a bump. Bumping one
  library rebuilds one library.
- `probe` is profile-free (ABI, not flags). It emits a `config.site`, so
  CPython's configure computes `pyconfig.h` instead of it being patched after.
- `Plan` in `recipe.go` alone decides the graph's shape; the CLI never
  constructs a job.

## Config layering

- `config/` is a symlink to `src/staticpy/internal/config/defaults/`, the
  `go:embed` tree (embed can't reach outside its package). One copy, nothing
  to sync.
- Resolution: **embedded defaults → `--config <dir>`**, later wins per
  top-level entry (a redefined profile replaces the embedded one; others
  survive).
- `sources.toml` and `patches/` are **outside** that stack: always the
  embedded copy unless `--sources <dir>` is passed. Otherwise any stray
  `config/` could redefine a sha256. `--sources` warns and is recorded in every
  artifact's `Manifest.Provenance`.
- The shim rebuilds when any file under `src/staticpy` is newer than the
  binary, not just `*.go`, because embeds (config, pyconfig fragments, `Setup`,
  `patcher.c`, staticapi) don't move a `.go` mtime. So a `config/` edit takes
  effect on the next `./staticpy`. Plain `go build` skips this.

## Invariants — do not break these

1. **An artifact directory is absent or complete.** Published by one rename,
   manifest (`.staticpy.json`) written last. Jobs write only into `work` and
   `stage`; core stamps the manifest, never a recipe.
2. **`KeyInputs` contain no absolute path and nothing per-run.** An absolute
   prefix makes the cache machine-specific; a timestamp or pid makes it
   useless. Don't re-hash what a dependency's key already covers.
3. **Two staticpy processes may share one `dist/`.** Content keys + `flock`
   leases + atomic rename. No process-global state assuming otherwise; never
   delete a file in `dist/locks/` (breaks flock identity for holders).
4. **Call `recipe.Bind(env)` before `recipe.Plan`.** `KeyInputs` gets no `Env`;
   without the bind the toolchain identity never reaches the key and stale
   artifacts survive a gccfactory re-publish.
5. **The shim provisions, the binary consumes.** staticpy never fetches a
   toolchain or qemu; it's handed paths and fails loudly if one is missing
   (so one binary works against a volume mount, a gccfactory checkout or a
   musl.cc unpack).
6. **Content-anchored edits assert their match count** (`Edit.MustMatch`, zero
   meaning exactly once). Prefer real diffs in `patches/`; use an Edit only
   where the fixup must survive an unreviewed version bump (the ctypes
   injection).
7. **Every command goes through the Runner** (`core.Cmd`), or it's missing from
   `dist/logs` and `commands.sh`.
8. **Bump `recipe.Version` by hand** when the *procedure* changes in a way flags
   don't capture (new step, reordering, install layout). It's in every key, so
   it rebuilds the world.

## Watching a long build or benchmark

Both fail *silently and partially*: the process lives, the counter climbs, the
output is wrong. Poll for failure, not liveness.

- **Count failures, not progress.** A run sat at "87 of 134" while 43 of the
  reference arm's 45 measurements had failed (`ModuleNotFoundError: pyperf`,
  installed into one arm's venv only).
  `grep -l '^# exit: [^0]' <logdir>/*.log | wc -l` catches it in 30 seconds.
- **Match terminal states** (`ERROR`, `error:`, `Traceback`, `FAILED`) and exit
  the watch loop when the process dies. A success-only grep looks like a hang.
- **Count per arm.** A failure on one arm only still renders a report, with
  ratios over whatever survived on both sides.
- **Verify the artifact, not the exit code.** `pyref` twice published without
  `_sqlite3` and `readline` at exit 0 ("necessary bits not found"). Prefer a
  postcondition inside the job.
- **Run a low-rate heartbeat** alongside the failure watch (e.g. every few
  minutes: `87/134 measured, failures static=0 reference=43`), so silence
  can't mean both "fine" and "dead an hour ago".
- **`pgrep -f` matches your own shell**, and misses argv with flags before the
  subcommand. Match something unique (session stamp, artifact path); distrust
  a count of 1.

## The debug loop

Each command a job runs has its own log, headed with cwd, overlaid env and
argv.

```sh
./staticpy doctor                  # host requirements, per target: buildable vs runnable
./staticpy status --todo           # what a build would do, before it does it
./staticpy logs <slug> --failed    # tail of the step the job died on
./staticpy logs <slug> --step configure
./staticpy logs <slug> --follow    # works while another process builds it
./staticpy shell <slug>            # that job's exact env and cwd
```

- `dist/logs/jobs/<slug>/latest/commands.sh` replays the run: copy out the
  failing command and iterate in `staticpy shell <slug>`.
- Attempt dirs are never reused, so a passing rebuild keeps the failure's
  evidence. The work tree is deleted on success; rebuild with `--keep-work` if
  `shell` needs it.
- Slugs are the names `staticpy status` prints, e.g.
  `dep:default:x86_64-linux-musl:openssl`.
- Don't close a red suite with `[expect.<failing-triple>]` or
  `[package.X.profile.<failing-profile>]`; hunt the class (recipe, qemu
  version, ABI, getpath). Rule: staticpy-traps **Do not overfit the last
  failure**.
- `staticpy verify` refuses to build by default. Levels: `smoke` (import
  probes, seconds, the gate every target must pass), `core` (language core plus
  every hand-linked extension module), `full` (CPython's suite, hours under
  qemu). Results are content-addressed, `report.json` in the verify artifact.

## State of play

- `x86_64-linux-musl` and `aarch64-linux-musl` are `proven` in
  `config/targets.toml`; the rest are `experimental`.
- **PGO is native-only in practice.** CPython runs `PROFILE_TASK` as
  `./python …` with no HOSTRUNNER, so `pgo = "on"` means `native-only` for a
  cross build.
- **Pyconfig fragments still cross-check the ABI probe.** The probe measures;
  fragments carry decisions (inline asm, atomics quirks). A target without a
  fragment file is a hard error.
- **Bundles are declared but empty** (`config/bundles.toml` has no `[pkg.*]`),
  so `--bundle` selects nothing. This never blocked pyperformance:
  `--with-ensurepip=no` leaves `ensurepip` and its wheel in the stdlib, so
  `-m venv` seeds pip even on the static build. `bench` defaults to
  pyperformance (installed per arm, plus each benchmark's requirements);
  `--suite micro` is the offline stdlib-only path. The real limit is per
  benchmark: no C extensions.

## Bench ETA weights

ETA = `this-run scale × leftover weight`. Weights live in
`src/staticpy/internal/bench/eta_weights.json`: mean `wall_s` of ok cells
(`wall_s >= 1`) from committed `benchmarks/*/timeline.jsonl`.

- Scale uses **only names already in the file**. A new name is guessed at the
  median until measured and must not enter `elapsed/weight`, or one surprise
  ten-minute benchmark rewrites every estimate.
- A kit runs once on a quiet box with no prior session and a different lineup;
  never look up last night's `(arm, benchmark)` cells.

Restamp (so new names join the shape) when:

- `config/bench.toml` / `DefaultPyperformance` moves and a session has timed
  the new suite
- a bench that used to skip or fail starts running (`skipped.json` shrinks)
- a committed `benchmarks/` timeline has names the JSON lacks

```sh
# from the repo root, after the new session is under benchmarks/
python3 -c '
import json, statistics
from collections import defaultdict
from pathlib import Path
d = defaultdict(list)
for p in Path("benchmarks").glob("*/timeline.jsonl"):
    for line in p.read_text().splitlines():
        if not line.strip():
            continue
        e = json.loads(line)
        if e.get("ok") and e.get("wall_s", 0) >= 1:
            d[e["benchmark"]].append(e["wall_s"])
out = {k: round(statistics.mean(v), 3) for k, v in sorted(d.items())}
Path("src/staticpy/internal/bench/eta_weights.json").write_text(
    json.dumps(out, indent=2) + "\n")
print(len(out), "weights")
'
```

Then `go test ./internal/bench/` in the spython container (Linux-only); the
replay test fails if the table regresses toward count-average.

## Related skills

- `staticpy-traps` — symptom-to-cause catalogue. Read before debugging
  anything that builds but misbehaves; write findings there.
- `staticpy-bump` — pin edits and matrix-upgrade rules.
- `staticpy-kit` — `staticpy kit`; commit first, stamp at a clean revision.
- `staticpy-add-target` — adding and proving a triple.
- `staticpy-release` — GitHub releases of packed tarballs.
- `comment-hygiene` — comment rules and sweeps.
