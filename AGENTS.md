# AGENTS.md (LLM generated)

Operational guide for LLM coding agents working on this repository. Skip the
project-narrative bits in `README.md` and treat this file as the procedural
reference -- what to run, where to write, what to leave behind.

## Repo at a glance

A from-source static Python toolchain. Version pins:

- Top-level `Makefile` pins `OPENSSL`, `LIBFFI`, `LIBLZMA`, `ZLIB`,
  `READLINE`, `NCURSES`, `SQLITE`, `BZIP2`, `UTILLINUX`, and `PYTHON`.
- The toolchain is **not** built here. gcc, binutils, musl, gmp, mpc, mpfr
  and the kernel headers are pinned in
  [gccfactory](https://github.com/junikimm717/gccfactory), which publishes
  one relocatable tarball per (host, target) cell to dev.mit.junic.kim.
  A compiler version bump is a gccfactory change plus a re-upload, not a
  change here.
- Supported targets: `supported.txt`.

Two build systems are in the tree. The `Makefile` is the one that works and
the one to use. `src/staticpy` is a Go build system that owns the same job in
data rather than recipes -- `config/*.toml` holds the versions, checksums,
per-package configure args, per-target quirks and flag profiles -- and has not
yet built an interpreter. It compiles, its config validates, and
`staticpy doctor` reports what a build would need. Until it has produced a
working interpreter, treat it as the thing being built rather than the thing
you build with, and keep `config/*.toml` in step with the `Makefile` when you
bump a version, because both are live.

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
  docker compose exec -T spython sh -c 'cd /workspace && make python3 -j$(nproc)'
  ```
  The toolchain is fetched as a tarball and unpacked into
  `deps-$(TARGET)/$(TARGET)-$(TCTYPE)/`. `make toolchain` does just that
  step, which is the cheapest way to check a fresh publish before spending
  an hour on a python build.
- **Static interpreter, all archs**: `./parallel-pythons.pl` from inside
  the container, inside a tmux session. Builds the native interpreter serially
  first (cross targets need it), then supervises N concurrent cross builds
  (default 4 workers x -j8 each), prefixes `make download`, and writes
  per-platform logs to `build-logs/python-static-<platform>.log`. Default is
  fail-fast; pass `-k` for keep-going. Default skips any platform whose
  `python-static-<platform>/bin/python<PYTHONV>` already exists; pass
  `--force` to rebuild. Plan for a multi-hour wall clock.
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
- `tarballs/`: mixed cache. Contains (a) external dep sources pulled by
  the Makefile (openssl, libffi, ..., Python), each sha256-pinned in
  `hashes/`, and (b) per-platform toolchain tarballs
  (`<platform>-<tctype>.tgz`) downloaded from dev.mit.junic.kim. The (b)
  entries are deliberately not hashed here -- they are our own build
  output, verified in gccfactory.
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

A toolchain tarball has to drop onto **any** Linux rootfs -- glibc,
near-empty, whatever -- and just work, including the
`-flto -fuse-linker-plugin -fno-fat-lto-objects` path. Both halves of that
come from gccfactory: every binary is static-musl (nothing to dlopen, no
loader to find) and GCC's LTO plugin is compiled into `libbfd`, so `ld`
resolves a `-plugin liblto_plugin.so` argument to its built-in copy rather
than opening a file that does not exist. The end-to-end proof lives in
`test-portability/`:

```sh
./test-portability/proof.sh
# tee's full output to build-logs/portability-alien.log
```

That builds a `debian:stable-slim` "alien" image with no compiler in it
(only `make`/`file`/`binutils`), extracts the host-native toolchain tarball
(`<uname -m>-linux-musl-native.tgz`) into `/opt`, checks no driver binary
has a `PT_INTERP` and that the unprefixed tool names (`cc`, `ld`, `make`,
...) are all present, then compiles + runs three nontrivial programs (C,
C++ with libstdc++, and a two-TU LTO build through a static archive),
including a negative control that links the slim LTO objects *without* the
plugin to confirm the link actually fails. Re-run after a toolchain
re-publish. `proof.sh` packs the tarball from
`deps-<arch>-linux-musl/<arch>-linux-musl-native/` when missing; or
regenerate explicitly with:

```sh
docker compose exec -T spython sh -lc \
  'cd /workspace && H=$(uname -m) && \
   tar -czf test-portability/${H}-linux-musl-native.tgz \
     -C deps-${H}-linux-musl ${H}-linux-musl-native'
```

Full writeup, including expected output, falsification controls, and
"where this would break", is in `ai/PORTABILITY_PROOF.md` -- written
against the older wrapper-based toolchain, so read its mechanism sections
as history.

## Tarball hashes and preflight downloads

Every external tarball is sha256-pinned in `hashes/<basename>.sha256`.
When you bump a version in the Makefile:

```sh
# fetch fresh tarballs and rewrite hashes/*.sha256 (skips verification so the
# new download isn't rejected for not matching the old hash).
docker compose exec -T spython sh -c 'cd /workspace && make update-hashes'
```

Then commit the new `hashes/*.sha256` files alongside the Makefile change.

Before any parallel build, preflight the cache with `make download`. It
pulls every source tarball into `tarballs/` serially, so two workers can
never race the same curl. `parallel-pythons.pl` runs it automatically;
pass `--no-download` if you know the cache is already warm.

## Benchmarking

`benchmark/dynamic-build.sh` builds the dynamic baseline: a stock
`--enable-shared` Python of the pinned version, against the container's
apk-installed `*-dev` packages. It is the honest comparison target for a
static build, and nothing else produces one.

```sh
docker compose exec -T spython sh -c 'cd /workspace && ./benchmark/dynamic-build.sh'
```

It reads `make print-PYTHON` for the version, so it depends on the Makefile;
`staticpy print python-version` is the replacement once that goes.

pyperformance is the intended suite -- it is what speed.python.org publishes
against, so its numbers are comparable to the wider world. `staticpy bench`
will drive it, and two things have to land first:

- pyperformance builds a venv per run and pip-installs each benchmark's
  requirements. This interpreter is `--with-ensurepip=no`, so there is no pip.
- Several benchmark dependencies are C extensions, and no dlopen means no
  compiled wheel will ever import.

Both are answered by a `bundle.bench` of the pure-Python dependencies compiled
into the interpreter, which arrives with bundles.

Benchmark natively only. Under qemu you are measuring qemu, and the overhead
is not uniform across workloads, so the numbers are comparable to nothing --
not to native, and not to each other.

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
    sh -lc \"cd /workspace && make python3 -j\\\$(nproc)\" \
  2>&1 | tee $LOG; \
  echo EXIT_CODE=\${PIPESTATUS[0]} | tee -a $LOG'"
```

Then poll the log file rather than re-attaching, so you don't fight the
user for the terminal. When you think the job is done, look at the
tail; check for `EXIT_CODE=0` **and** grep `'Error [0-9]\|FAILED'` to
catch failure messages, since past `EXIT_CODE=0` lines on broken builds
have been observed when the launcher pattern wasn't pipestatus-aware.

For all-arch interpreter builds use `parallel-pythons.pl` rather than
hand-rolling parallel `make` calls; it owns the worker pool and
per-platform logging itself, and it expects to be invoked from inside a
single tmux session.

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
- `config/*.toml` is the version pin now. If you bump a version there, the
  sha256 in the same file must move with it.

## What "done" looks like for common tasks

| task | done when |
|---|---|
| version bump | `config/sources.toml` edited with the new version and sha256, x86_64 static build green, `staticpy verify --level core` clean |
| toolchain change (re-publish from gccfactory) | `make toolchain` fetches it, `./test-portability/proof.sh` is green, x86_64 static build green, sanity imports clean, and the banner from the new binary mentions the expected gcc version (`python3 -c 'import sys; print(sys.version)'`) |
| cross-arch interpreter fan-out | `parallel-pythons.pl` exits clean, every arch in `supported.txt` has `python-static-<platform>/bin/python<PYTHONV>`, and at least one non-x86_64 arch benchmarked |
