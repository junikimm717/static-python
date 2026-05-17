# AGENTS.md (LLM generated)

Operational guide for LLM coding agents working on this repository. Skip the
project-narrative bits in `README.md` and treat this file as the procedural
reference -- what to run, where to write, what to leave behind.

## Repo at a glance

A from-source static Python toolchain. The single source of truth for *every*
version pin is the top-level `Makefile`:

- toolchain (`musl-cross-make`, `binutils`, `musl`, `gcc`) -- gcc version lives
  in `cross-make/config.mak`, the rest are inherited from `musl-cross-make`
  master defaults.
- third-party libs (`openssl`, `sqlite`, `libffi`, `xz/lzma`, `zlib`,
  `ncurses`, `readline`, `bzip2`, `util-linux`, `python`) -- pinned at the top
  of `Makefile` as `OPENSSL := 3.5.6`, etc.
- supported targets: `supported.txt`.

Builds run **inside the Alpine dev container**, never on the host. The
container's `/workspace` is a bind mount of the repo, so anything you create
on disk shows up on the host immediately. Use `tmux` *on the host* (the
container image does not ship `tmux`) to keep long-running build jobs alive.

The static interpreter is the artefact under
`python-static-${HOST_ARCH}-linux-musl/bin/python3.13`. The stock
dynamically-linked baseline used for benchmarking sits at
`python-dynamic-${HOST_ARCH}-linux-musl/bin/python3.13`.

## Container handle

```sh
# one-shot exec
docker compose exec -T spython sh -c 'cd /workspace && ...'

# bring it up if it isn't running
docker compose up -d spython
```

`docker compose exec` requires the service to be up. The compose file holds
the container alive with a `while sleep` entrypoint, so it does not auto-exit
between commands.

## Builds

- **Static, native arch** (this is `python3` -- the repo's default target):
  ```sh
  docker compose exec -T spython sh -c 'cd /workspace && make USE_CROSSMAKE=1 python3 -j$(nproc)'
  ```
  `USE_CROSSMAKE=1` forces a from-source toolchain via `musl-cross-make`
  rather than the prebuilt `*.tgz` mirror referenced for `USE_CROSSMAKE=0`.
  Both paths install into the same `deps-$(TARGET)/$(TARGET)-$(TCTYPE)/`
  prefix, so a downstream Python build is indifferent.
- **Static, all archs**: `./build-all.sh` (serial). Bootstraps x86_64 first to
  prime the source cache, then walks `supported.txt`. Plan for a multi-hour
  wall clock with gcc 15.
- **Dynamic baseline (x86_64 / aarch64 / whichever host you're on)**:
  ```sh
  docker compose exec -T spython sh -c 'cd /workspace && ./benchmark/dynamic-build.sh'
  ```
  Builds a stock `--enable-shared` Python of the same version pinned in the
  Makefile, against the container's apk-installed `*-dev` packages.

Build artefacts you can safely nuke if you need to re-do work:
- `deps-${TARGET}/` and `build-${TARGET}/`: per-target intermediates. The
  Makefile is idempotent so a partial removal triggers a partial rebuild.
- `python-static-${TARGET}/`, `python-dynamic-${TARGET}/`: the installed
  interpreters.
- `tarballs/*.tgz` from `dev.mit.junic.kim`: cached prebuilt toolchains for
  `USE_CROSSMAKE=0`. Keep them unless you are explicitly switching to
  `USE_CROSSMAKE=1`.
- *Never* nuke `hashes/` -- those are the trusted checksums for every
  externally fetched tarball.

## Tarball hashes

Every external tarball is sha256-pinned in `hashes/<basename>.sha256`. When
you bump a version in the Makefile:

```sh
# fetch fresh tarballs and rewrite hashes/*.sha256 (skips verification so the
# new download isn't rejected for not matching the old hash).
docker compose exec -T spython sh -c 'cd /workspace && make update-hashes'
```

Then commit the new `hashes/*.sha256` files alongside the Makefile change.

## Benchmarking workflow

The benchmark harness in `benchmark/` exists to put numbers on the "is `-O3
-flto` + static linking actually worth anything?" question and to catch
regressions when libraries or the compiler get bumped. Three pieces:

| file | purpose |
|---|---|
| `benchmark/microbench.py` | CPU-bound interpreter loops, ns/op timing |
| `benchmark/measure_startup.py` | external cold-start / first-import probe |
| `benchmark/run.sh` | orchestrator -- runs both, emits a markdown report |

### Required state before you run

1. Container is up (`docker compose up -d spython`).
2. Static build exists at
   `python-static-${HOST_ARCH}-linux-musl/bin/python3.13`. If not, build it
   (see above).
3. (Optional but recommended) dynamic baseline exists at
   `python-dynamic-${HOST_ARCH}-linux-musl/bin/python3.13`. If not, run
   `./benchmark/dynamic-build.sh`. Without it, you only get static vs
   system-python, which is a confounded comparison (different Python
   versions).

### Running

```sh
docker compose exec -T spython sh -c 'cd /workspace && ./benchmark/run.sh'
```

What happens:

- Architecture is detected from `uname -m`; Python version from `make
  print-PYTHONV`. Nothing is hard-coded.
- The script writes a fresh report to
  `benchmark/reports/<YYYY-MM-DDThhmmZ>_<arch>.md`, owned by the host UID
  (the script `chown`s back so you can edit it).
- The report includes (a) an Environment block (CPU model, core count,
  cache hierarchy, kernel), (b) interpreter metadata (path, version,
  linkage, on-disk size), (c) CPU micro-benchmark table with per-row and
  geomean `X / static` ratios, (d) startup-probe table with the same shape,
  and (e) an empty `## Analysis` placeholder at the bottom.
- The full report is also echoed to stdout for ad-hoc inspection.

### Writing the analysis (this is your job)

**Always** open the new file under `benchmark/reports/` and replace the
italic placeholder under `## Analysis` with prose. The previous report in
that directory is the natural diff target. A good analysis:

1. **Names the change.** "First run after `OPENSSL` 3.5.0 -> 3.5.6 and
   `gcc` 9.4 -> 15.1.0". If you didn't change anything and it's a re-run,
   say so and call out any drift bigger than ~3% as suspicious.
2. **Calls out what moved meaningfully** -- per-row deltas of more than a
   few percent vs the previous report. Don't editorialize 1.17x -> 1.18x;
   *do* editorialize 0.92x -> 0.99x or 1.10x -> 1.13x.
3. **Tests hypotheses against the numbers.** If the README claims
   "regressions come from older libraries", and you just matched libraries
   and the regressions are still there, *say that the hypothesis was
   falsified* and propose the next one. Don't preserve a comfortable story.
4. **Leaves a concrete next step.** A one-line `perf stat` invocation or
   an extra benchmark you'd run beats a vague "should investigate".

See `benchmark/reports/2026-05-17T1818Z_x86_64.md` for a worked example.

### When to re-run

- After bumping any version in the Makefile or `cross-make/config.mak`.
- After touching `configure-wrapper.sh`, `python/Setup`, or any other thing
  in `python/` that the runtime build picks up.
- After rebuilding the dynamic baseline (`benchmark/dynamic-build.sh`).
- *Not* in normal source edits that don't reach the binary.

## Tmux discipline for long jobs

The host has tmux; the container does not. Use the host wrapper pattern:

```sh
tmux new-session -d -s build "\
  docker compose -f /home/junikim/staticpy/docker-compose.yml exec -T spython \
    sh -lc 'cd /workspace && make USE_CROSSMAKE=1 python3 -j32' \
  2>&1 | tee /home/junikim/staticpy/build.log; \
  echo EXIT_CODE=\$? | tee -a /home/junikim/staticpy/build.log"
```

Then poll the log file rather than re-attaching, so you don't fight the user
for the terminal. When you think the job is done, check `tail -n 20
build.log` and `grep EXIT_CODE build.log`.

## Commits

Don't commit unless the user explicitly asks. Even then:

- Read `git status` and `git diff` first, summarise what you'd commit, then
  ask for the green light before running `git add` / `git commit`.
- The `hashes/` files matter -- if you bumped a version, the matching
  `hashes/*.sha256` must be in the same commit. The Makefile will refuse to
  build otherwise.
- New benchmark reports under `benchmark/reports/` are tracked; commit them
  alongside the source change that motivated re-running.

## What "done" looks like for common tasks

| task | done when |
|---|---|
| version bump | Makefile edited, `make update-hashes` run, `hashes/` refreshed, x86_64 static build green, sanity imports clean (`ssl, zlib, sqlite3, ctypes, _lzma, _hashlib`), benchmark re-run with analysis written |
| toolchain change | as above, plus banner from the new binary mentions the right gcc version (`python3 -c 'import sys; print(sys.version)'`) |
| benchmark code change | report renders, analysis explains what the new metric is measuring and why the baseline numbers stayed put (or didn't) |
| cross-arch fan-out | each arch in `supported.txt` produces a static interpreter; at least one non-x86_64 arch gets benchmarked |
