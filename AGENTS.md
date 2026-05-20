# AGENTS.md (LLM generated)

Operational guide for LLM coding agents working on this repository. Skip the
project-narrative bits in `README.md` and treat this file as the procedural
reference -- what to run, where to write, what to leave behind.

## Repo at a glance

A from-source static Python toolchain. Version pins:

- Top-level `Makefile` pins `CROSSMAKE`, `OPENSSL`, `LIBFFI`, `LIBLZMA`,
  `ZLIB`, `READLINE`, `NCURSES`, `SQLITE`, `BZIP2`, `UTILLINUX`, `PYTHON`,
  `LINUX_VER`, and `GCC_VER`.
- `cross-make/config.mak` pins `GCC_VER` (must match the top-level
  Makefile's `GCC_VER`; the post-install hook reads the top-level one).
- `binutils`, `musl`, `gmp`, `mpc`, `mpfr`, `config.{sub,guess}` come from
  `musl-cross-make` master's defaults; no local override.
- Supported targets: `supported.txt`.

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
- **Static, all archs (toolchains)**: `./parallel-toolchains.pl` from inside
  the container, inside a tmux session. Supervises N concurrent
  toolchain builds (default 4 workers x -j8 each), prefixes a `make
  download` to populate `tarballs/` serially, and writes per-platform
  logs to `build-logs/toolchain-<platform>.log`. Default is fail-fast;
  pass `-k` for keep-going. Default skips any platform whose
  `tarballs/<platform>-<tctype>.tgz` already exists; pass `--force` to
  rebuild. Plan for a multi-hour wall clock with gcc 15.
- **Static interpreter, all archs**: `./parallel-pythons.pl` from inside
  the container, inside a tmux session. Builds the native interpreter serially
  first (cross targets need it), then supervises N concurrent cross builds
  (default 4 workers x -j8 each), prefixes `make download`, and writes
  per-platform logs to `build-logs/python-static-<platform>.log`. Default is
  fail-fast; pass `-k` for keep-going. Default skips any platform whose
  `python-static-<platform>/bin/python<PYTHONV>` already exists; pass
  `--force` to rebuild. Expects prebuilt toolchain tarballs (`USE_CROSSMAKE=0`,
  the default); pass `--use-crossmake` to build toolchains inline. Plan for a
  multi-hour wall clock.
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
- `tarballs/`: mixed source cache. Contains (a) external dep sources
  pulled by the Makefile (openssl, libffi, ..., Python), (b)
  `musl-cross-make-master.tar.gz`, (c) musl-cross-make sources populated
  by `make download` (gcc, binutils, musl, ...), and (d) per-platform
  toolchain tarballs we built (`<platform>-<tctype>.tgz`). All entries
  in (a)/(b)/(c) are sha256/sha1-pinned. The (d) tarballs match what
  the `USE_CROSSMAKE=0` recipe would otherwise download from
  `dev.mit.junic.kim`.
- `deps-download-prime/`: sentinel tree for `make download`. Holds the
  extracted source dirs as a benign side effect (~1.5 GB). `make clean`
  intentionally preserves it; `make distclean` removes it.
- *Never* nuke `hashes/` -- those are the trusted checksums for every
  externally fetched tarball.

## Docker

PLEASE maximally use docker for all your builds above. You might occasionally
run into permission denied errors if you try to stream logs into
build-logs and that directory happens to be owned by root.

If you absolutely must use the host system, you need all the dependencies
specified in the Dockerfile. Furthermore, you must check that the entire
filesystem is owned by you before proceeding.

## Portability check

The whole point of the `cross-make/wrapper.c` + `cross-make/post-install.sh`
trick is that a toolchain tarball built here drops onto **any** Linux
rootfs (glibc, near-empty, whatever) and just works -- including the
`-flto -fuse-linker-plugin -fno-fat-lto-objects` path where `ld.bfd`
has to dlopen `liblto_plugin.so`. The end-to-end proof lives in
`cross-make/test-portability/`:

```sh
./cross-make/test-portability/proof.sh
# tee's full output to build-logs/portability-alien.log
```

That builds a `debian:stable-slim` "alien" image with no compiler in
it (only `make`/`file`/`binutils`), extracts the toolchain tarball at
`cross-make/test-portability/x86_64-linux-musl-native.tgz` into `/opt`,
then compiles + runs three nontrivial programs (C, C++ with libstdc++,
and a two-TU LTO build via `-flto -fuse-linker-plugin
-fno-fat-lto-objects`), including a negative control that links the
slim LTO objects *without* the plugin to confirm the link actually
fails. Re-run after touching `wrapper.c`, `post-install.sh`, or after
a `GCC_VER` / `BINUTILS` bump. The bundled toolchain tarball is
regenerated from a current `deps-x86_64-linux-musl/` tree with:

```sh
docker compose exec -T spython sh -lc \
  'cd /workspace && tar -czf cross-make/test-portability/x86_64-linux-musl-native.tgz \
     -C deps-x86_64-linux-musl x86_64-linux-musl-native'
```

Full writeup, including expected output, falsification controls, and
"where this would break", is in `ai/PORTABILITY_PROOF.md`.

## Tarball hashes and preflight downloads

Every external tarball is sha256-pinned in `hashes/<basename>.sha256`.
When you bump a version in the Makefile:

```sh
# fetch fresh tarballs and rewrite hashes/*.sha256 (skips verification so the
# new download isn't rejected for not matching the old hash).
docker compose exec -T spython sh -c 'cd /workspace && make update-hashes'
```

Then commit the new `hashes/*.sha256` files alongside the Makefile change.

Before any parallel toolchain run, preflight the cache with `make
download`. It pulls every tarball any toolchain or python3 build will
need (top-level deps + musl-cross-make sources) into `tarballs/`
serially. This eliminates the only real concurrency race: multiple
`make crossmake` workers symlink `sources/` back to the shared
`tarballs/`, and musl-cross-make's `sources/%` wget rule has no locking,
so two workers fetching the same gcc tarball corrupt one tmp file.
`parallel-toolchains.pl` runs `make download` automatically; pass
`--no-download` if you know the cache is already warm.

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

### Also update `benchmark/reports/README.md`

`benchmark/reports/README.md` is the short-attention-span cliffs-notes
across **all** reports -- TL;DR + "what wins / what's mixed" ranges +
reports index + open experiments. **It goes stale the moment a new
report lands.** After writing your per-report `## Analysis`, do this:

- Bump the "Last updated" date and the run-count line at the top.
- If your new run pushes either bound of a range column under
  "What wins" or "What is mixed or wrong", widen the range.
- Add a row to the "Reports index" table (host + 1-line distinguishing
  fact).
- If a TL;DR bullet became more or less true, edit it. If an item in
  "What is missing" got answered, strike it and turn the answer into a
  TL;DR bullet.
- If you found a new cross-cutting pattern (a row that's consistently
  weird, a knob that consistently moves the geomean), add it.

The cliffs-notes README has its own checklist at the bottom mirroring
this; both should agree.

### When to re-run

- After bumping any version in the Makefile or `cross-make/config.mak`.
- After touching `configure-wrapper.sh`, `python/Setup`, or any other thing
  in `python/` that the runtime build picks up.
- After rebuilding the dynamic baseline (`benchmark/dynamic-build.sh`).
- *Not* in normal source edits that don't reach the binary.

## Tmux discipline for long jobs

The host has tmux; the container does not. Use the host wrapper pattern:
spawn a detached tmux session that runs the command via `docker compose
exec`, tees stdout+stderr to a log file, and writes a final
`EXIT_CODE=...` line that reflects the **inner** command, not `tee`.

Bash's `${PIPESTATUS[0]}` (or `set -o pipefail` + `$?`) is the right way
to capture the upstream exit; plain `$?` after a pipeline reads the
exit of the rightmost stage (`tee`), which is always 0 -- a build can
fail loudly and the log will still claim `EXIT_CODE=0`.

```sh
LOG=$PWD/build.log
tmux new-session -d -s build "bash -c '\
  set -o pipefail; \
  docker compose exec -T spython \
    sh -lc \"cd /workspace && make USE_CROSSMAKE=1 python3 -j\\\$(nproc)\" \
  2>&1 | tee $LOG; \
  echo EXIT_CODE=\${PIPESTATUS[0]} | tee -a $LOG'"
```

Then poll the log file rather than re-attaching, so you don't fight the
user for the terminal. When you think the job is done, look at the
tail; check for `EXIT_CODE=0` **and** grep `'Error [0-9]\|FAILED'` to
catch failure messages, since past `EXIT_CODE=0` lines on broken builds
have been observed when the launcher pattern wasn't pipestatus-aware.

For all-arch toolchain builds use `parallel-toolchains.pl` rather than
hand-rolling parallel `make crossmake` calls; it owns the worker pool
and per-platform logging itself, and it expects to be invoked from
inside a single tmux session.

## Comment style

Don't write overly verbose comments.

- A comment exists to explain *why* the code looks the way it does -- a
  trade-off, a workaround, a thing the reader can't infer from the code.
- If the code is self-evident (`# build the actual binary`), drop the
  comment.
- Avoid stale framing: don't write "now we do X", "previously we did Y",
  or "item N of the rebuild plan". The "now" goes stale on the next
  refactor; the rebuild plan is gone after a few commits.
- Avoid baking specific numbers (gcc 9.4, 32 cores, -j32) into prose
  unless the number is the *point* of the comment.
- Three to five lines is usually the right length. If you find yourself
  writing a paragraph, the explanation probably belongs in `ai/` (see
  below) and the comment can just point there.

## Reports under `ai/`

Anything you discover that is more than a one-line comment's worth of
explanation -- a real bug in an upstream component, a non-obvious
interaction between flags, the actual root cause of a failure mode that
took you more than fifteen minutes to corner -- writes up as a markdown
report under `ai/`. Examples already there:

- `ai/MUSL_REPORT.md` -- musl `fma` losing negative zero on underflow,
  and the two safety nets that should have caught it.

Format follows that example: title, one-paragraph summary, minimal
reproducer (code or command), each layer of root cause as its own
section, and a "what we ended up doing" / "what we'd want upstream" at
the end.

When you write a report, link it from the relevant code with a one-line
comment like `# See ai/<NAME>.md.`, *not* by inlining the explanation
into the source.

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
| version bump | Makefile edited, `make update-hashes` run, `hashes/` refreshed, x86_64 static build green, sanity imports clean (`ssl, zlib, sqlite3, ctypes, _lzma, _hashlib`), benchmark re-run with analysis written, `benchmark/reports/README.md` updated |
| toolchain change | as above, plus banner from the new binary mentions the right gcc version (`python3 -c 'import sys; print(sys.version)'`); if `GCC_VER` moved, both the top-level `Makefile` and `cross-make/config.mak` were bumped together |
| benchmark code change | report renders, analysis explains what the new metric is measuring and why the baseline numbers stayed put (or didn't), `benchmark/reports/README.md` updated |
| cross-arch toolchain fan-out | `parallel-toolchains.pl` exits clean, every arch in `supported.txt` has a `tarballs/<arch>-<tctype>.tgz`, and `build-logs/toolchain-<arch>.log` ends in `EXIT_CODE=0` |
| cross-arch interpreter fan-out | `parallel-pythons.pl` exits clean, every arch in `supported.txt` has `python-static-<platform>/bin/python<PYTHONV>`, and at least one non-x86_64 arch benchmarked |
