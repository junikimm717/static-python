# AGENTS.md (LLM generated)

Operational guide for LLM coding agents working on this repository. Skip the
project-narrative bits in `README.md` and treat this file as the procedural
reference -- what to run, where to write, what to leave behind.

## Repo at a glance

A from-source static Python toolchain. Version pins:

- `config/sources.toml` pins every upstream tarball -- version, URL and
  sha256 in one place. `staticpy print version:<name>` reads one back.
- The toolchain is **not** built here. gcc, binutils, musl, gmp, mpc, mpfr
  and the kernel headers are pinned in
  [gccfactory](https://github.com/junikimm717/gccfactory), which publishes
  one relocatable tarball per (host, target) cell to dev.mit.junic.kim.
  A compiler version bump is a gccfactory change plus a re-upload, not a
  change here.
- Supported targets: one row each in `config/targets.toml`;
  `staticpy print targets-all` lists them.

`src/staticpy` is the build system, driven through the `./staticpy` shim. It
owns the job in data rather than recipes -- `config/*.toml` holds the versions,
checksums, per-package configure args, per-target quirks and flag profiles --
and every job's key is hashed over its inputs and its dependencies' keys, so an
edit rebuilds exactly what depends on it and nothing else.

The interpreter is the artefact under
`dist/artifacts/pynative_<profile>_<triple>/bin/python3.14` for a native build
and `dist/artifacts/pycross_<profile>_<host>_<triple>/` for a cross one;
`--pack` also writes a relocatable tarball to
`dist/out/<profile>/<triple>/`. The dynamically-linked baseline used for
benchmarking is the `reference` profile's own artifact,
`dist/artifacts/pyref_reference_<triple>/rootfs/bin/python3.14`.

The dev container is still the easiest way to get a clean host, and its
`/workspace` is a bind mount of the repo. Job PATH is the fetched toolchain
plus `dirname(busybox)`; perl, make, and the few other host tools that have
to sit next to busybox are still load-bearing.
Use `tmux` *on the host* (the container image does not ship `tmux`) to keep
long-running build jobs alive.

## Container handle

There is one container. Static, cross, `reference*`, verify, pack, and
`kit` all run in it. Do not introduce a second image to "keep glibc and
musl apart" — the image is Ubuntu (glibc hostcc for `reference*`);
static/cross still use the gccfactory musl toolchains under
`dist/toolchains/`.

```sh
# one-shot exec
docker compose exec -T spython sh -c 'cd /workspace && ...'

# bring it up if it isn't running
docker compose up -d spython
```

`docker compose exec` requires the service to be up. The compose file holds
the container alive with a `while sleep` entrypoint, so it does not auto-exit
between commands. Starting it also registers the qemu interpreters in
binfmt_misc, which is why the service is privileged: that table belongs to the
host kernel, so the registration is machine-wide and not container-scoped.
staticpy still execs qemu by path and needs none of it -- what does is
CPython's own suite, which re-execs the target interpreter to spawn its
workers.

## Builds

- **Static, native arch** (the default with no `--target`):
  ```sh
  ./staticpy build --verify core --pack
  ```
  On a terminal, `build` with no `--target` opens a short wizard for the flags
  not given and prints the equivalent command line at the end. Non-interactive
  runs (CI, pipes, `TERM=dumb`, `STATICPY_NO_TUI=1`) never prompt: no
  `--target` keeps meaning the host triple with flag defaults, so scripted
  invocations are unchanged.
- **Static interpreter, all archs**:
  ```sh
  ./staticpy build --target all --verify core --pack --workers 2 -j 8
  ```
  `--workers` times `-j` is what actually loads the machine. The default is 4
  workers, which peaks around 11 GB of RSS per target during the LTO link --
  measure before trusting it on a machine with less than 48 GB. Plan for a
  multi-hour wall clock. Note that the build is fail-fast: one target's flake
  abandons the jobs still queued, so re-run rather than concluding anything
  from a partial sweep.
- **Dynamic baseline** (same container as the static build):
  ```sh
  ./staticpy build --profile reference
  ```
  Builds a stock `--enable-shared` Python of the same pinned version with the
  container's gcc, against shared copies of the same pinned dependencies.

Everything staticpy writes lives under `dist/` and is safe to delete; a
content-addressed rebuild recovers whatever you removed. Within it:
- `dist/artifacts/`: the published outputs, one directory per job key.
- `dist/work/`, `dist/srctrees/`: per-job intermediates and extracted sources.
- `dist/src/`: the sha256-verified upstream tarball cache.
- `dist/toolchains/`: toolchains the shim fetched from dev.mit.junic.kim.
  Deliberately not hashed here -- they are our own build output, verified in
  gccfactory.
- `dist/logs/jobs/<slug>/latest/`: every command's full output, always, whether
  or not `-v` was passed.

## Docker

PLEASE maximally use docker for all your builds above. You might occasionally
run into permission denied errors if you try to stream logs into
build-logs and that directory happens to be owned by root.

If you absolutely must use the host system, you need all the dependencies
specified in the Dockerfile. Furthermore, you must check that the entire
filesystem is owned by you before proceeding.

## Tarball hashes and preflight downloads

Every external tarball is sha256-pinned in `config/sources.toml`, in the same
edit as its version -- there is no second file to keep in step and no
`update-hashes` step. A tarball whose hash does not match the pin is deleted
rather than left for the next run to reuse. The checksum is part of every
dependent job's content key, so changing a pin invalidates exactly the
artifacts that depend on it.

`staticpy sources` is the handle:

```sh
./staticpy sources list      # the pins, and what is already in dist/src
./staticpy sources fetch     # download whatever is missing, verifying as it lands
./staticpy sources verify    # re-hash what is on disk, ignoring the .done markers
```

`fetch` is worth running on its own before a long build on a flaky link, and is
what makes `--offline` usable afterwards.

## Benchmarking

The `reference` profile builds the dynamic baseline: a stock `--enable-shared`
Python of the pinned version, compiled by this machine's own gcc against shared
copies of the same pinned dependencies. Same source at the same version, so
nothing but linkage and libc can explain a gap.

```sh
./staticpy build --profile reference
./staticpy bench --interp static --interp reference --baseline reference
```

Nothing is benchmarked unless it is named. `--interp` takes `static` (this
machine's pynative artifact), `reference` (the baseline above), `system`
(whatever `python3` resolves to) or an explicit `label=/path/to/python`;
`--baseline` fixes the denominator of every ratio, because with auto-discovery
adding an arm could silently change what everything was measured against.

`bench` runs pyperformance by default and installs it into a venv per arm.
`--with-ensurepip=no` does not remove pip's *source*: the ensurepip module and
its bundled wheel stay in the stdlib, so `-m venv` seeds a working pip. What a
static interpreter genuinely cannot do is dlopen a C extension, so a benchmark
whose requirement ships one fails at import and is named in `skipped.json`,
along with anything whose dependencies would not install -- a geometric mean
over a silently narrowed set is worse than no number at all. `--suite micro`
selects the built-in stdlib-only loops plus a spawn-latency probe, which is the
offline answer and a quick check, but reporting its geomean as though it
described a workload overstates whatever the interpreter is good at.

Results land in `dist/bench/<UTC-stamp>-<arch>/`, never overwritten: alongside
the report are the raw pyperf JSON, a manifest recording each binary's sha256
and linkage, and `timeline.jsonl` -- one record per measurement with its wall
time, load average and the busy fraction of the pinned core's SMT sibling,
which is what lets a suspicious number be audited months later.

Check the per-arm failure counts in `timeline.jsonl` before trusting a geomean.
A run once rendered a perfectly plausible report while 43 of 45 measurements on
one arm had failed.

Benchmark natively only. Under qemu you are measuring qemu, and the overhead
is not uniform across workloads, so the numbers are comparable to nothing --
not to native, and not to each other. `staticpy bench` refuses a `--target`
other than the build machine's own triple for exactly this reason.

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
  ./staticpy build --target all --verify core --pack --workers 2 -j 8 \
  2>&1 | tee $LOG; \
  echo EXIT_CODE=\${PIPESTATUS[0]} | tee -a $LOG'"
```

Then poll the log file rather than re-attaching, so you don't fight the
user for the terminal. When you think the job is done, look at the
tail; check for `EXIT_CODE=0` **and** grep `'Error [0-9]\|FAILED'` to
catch failure messages, since past `EXIT_CODE=0` lines on broken builds
have been observed when the launcher pattern wasn't pipestatus-aware.

`./staticpy status` answers what exists, what is stale and what is building
right now, from any shell, without touching the tmux session. Prefer it to
reading a log for progress.

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
  writing a paragraph, the explanation probably belongs in the
  `staticpy-traps` skill (see below) and the comment can just point there.

## Do not overfit a sweep

Agent investigation is cheap; a verify+pack across eleven targets is not.
Do not park a failure under `[expect.<this-triple>]` or a one-package
`[package.X.profile.<this>]` so the current run goes green. Reproduce on
a second arm, name the layer (recipe, emulator version, ABI), and hunt
until you can state the complete fix. An ignore is allowed only when that
fix is "unfixable" plus the experiment that proves it. The rule, with
the overnight counterexamples, is **Do not overfit the last failure** in
`.agents/skills/staticpy-traps/SKILL.md`.

## Findings go in the `staticpy-traps` skill

Anything you discover that is more than a one-line comment's worth of
explanation -- a real bug in an upstream component, a non-obvious
interaction between flags, the actual root cause of a failure mode that
took you more than fifteen minutes to corner -- belongs in
`.agents/skills/staticpy-traps/`, which is the one place a future agent
is guaranteed to read before debugging.

Small enough to state in a paragraph: add it to `SKILL.md` as an entry in
the matching section, phrased symptom-first so it is findable by what you
would have searched for.

Bigger than that -- it needs a reproducer, a disassembly, before/after
numbers, or layers of root cause -- it becomes a new file under
`.agents/skills/staticpy-traps/references/`, plus the short `SKILL.md`
entry that links to it at the point where the problem bites. The three
already there are the format: title, one-paragraph summary, minimal
reproducer (code or command), each layer of root cause as its own
section, and a "what we ended up doing" / "what we'd want upstream" at
the end.

- `references/MUSL_REPORT.md` -- musl `fma` losing negative zero on
  underflow, and the two safety nets that should have caught it.
- `references/MIPS64_FFI_REPORT.md` -- libffi closures returning the high
  half of the return slot for narrow integers on big-endian mips.

Either way, link it from the relevant code with a one-line comment, *not*
by inlining the explanation into the source. A long finding left in a
commit message or a scratch file is a finding nobody reads before hitting
the same wall.

## Commits

Don't commit unless the user explicitly asks. Even then:

- Read `git status` and `git diff` first, summarise what you'd commit, then
  ask for the green light before running `git add` / `git commit`.
- A version bump moves the version and the sha256 in the same edit of
  `config/sources.toml`. A pin without its matching checksum is not a
  half-done commit, it is a build that fetches an unverified tarball.
- `config/sources.toml` and `config/patches/` are deliberately excluded from
  the `--config` overlay -- a file lying around must not be able to redefine a
  pinned checksum -- so a build reads only the copy embedded in the binary.
  `config/` is a symlink to `src/staticpy/internal/config/defaults/`, the tree
  `go:embed` compiles in, so editing it and running `./staticpy` rebuilds
  around the change. Building with plain `go build` skips that rebuild and
  leaves you on whatever was embedded last.
- A diff that only one architecture needs goes in
  `[source.<pkg>.target_patches]` keyed by triple, not in `patches`. `patches`
  is hashed into the shared srctree, so a fix for one target there invalidates
  every target's deps and every interpreter; a target patch is applied to the
  staged copy and reaches that one target's key. Check it with
  `staticpy --json build --dry-run --target ...` before and after.

## What "done" looks like for common tasks

| task | done when |
|---|---|
| version bump | `config/sources.toml` edited with the new version and sha256, x86_64 static build green, `staticpy verify --level core` clean |
| toolchain change (re-publish from gccfactory) | the shim fetches it into `dist/toolchains/`, x86_64 static build green, sanity imports clean, and the banner from the new binary mentions the expected gcc version (`python3 -c 'import sys; print(sys.version)'`) |
| cross-arch interpreter fan-out | `staticpy status --target all` reports 0 stale and 0 missing, every target has a verify artifact with `failed=0`, every packed tarball's sha256 checks out, and at least one non-x86_64 arch benchmarked |
