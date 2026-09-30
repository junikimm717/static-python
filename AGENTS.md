# AGENTS.md

Procedural reference for coding agents. For the build system itself (DAG,
config layout, invariants, debug loop) load the `staticpy` skill; the other
`.agents/skills/` cover bumps, targets, kits, releases, traps and comments.

## Repo at a glance

- `src/staticpy` is the build system, driven through the `./staticpy` shim.
  `./staticpy help [<cmd>]` is authoritative for commands and flags.
- `config/sources.toml` pins every upstream tarball (version, URL, sha256).
  `staticpy print version:<name>` reads one back.
- Targets: one row each in `config/targets.toml`; `staticpy print targets-all`.
- The toolchain (gcc, binutils, musl, gmp, mpc, mpfr, kernel headers) is
  **not** built here. [gccfactory](https://github.com/junikimm717/gccfactory)
  publishes one relocatable tarball per (host, target) cell to
  dev.mit.junic.kim; the shim fetches it into `dist/toolchains/`. A compiler
  bump is a gccfactory change plus a re-upload. Job PATH is that toolchain,
  then the process PATH (perl and patch come from the host).

Everything generated is under `dist/`, safe to delete (content-addressed
rebuilds recover it):

| path | contents |
|---|---|
| `dist/artifacts/pynative_<profile>_<triple>/bin/python3.14` | native interpreter |
| `dist/artifacts/pycross_<profile>_<host>_<triple>/` | cross interpreter |
| `dist/artifacts/pyref_reference_<triple>/rootfs/bin/python3.14` | dynamic baseline (stock `--enable-shared`, same pins, container gcc) |
| `dist/out/<profile>/<triple>/` | `--pack` tarballs |
| `dist/src/`, `dist/srctrees/`, `dist/work/` | verified tarballs, extracted sources, job intermediates |
| `dist/toolchains/` | fetched toolchains (not hashed here; gccfactory verifies them) |
| `dist/logs/jobs/<slug>/latest/` | every command's full output, with or without `-v` |

## Docker first

Use the container for every build. One container (`spython`) runs static,
cross, `reference*`, verify, pack and `kit`. Do not add a second image to
"keep glibc and musl apart": the image is Ubuntu (glibc hostcc for
`reference*`); static/cross use the gccfactory musl toolchains.

```sh
docker compose up -d spython                                 # if not running
docker compose exec -T spython sh -c 'cd /workspace && ...'  # /workspace = repo
```

It stays up (`while sleep` entrypoint) and is privileged because it registers
qemu in the host kernel's binfmt_misc, machine-wide. staticpy execs qemu by
path; binfmt is for CPython's suite, which re-execs the interpreter.

On the host instead: install the Dockerfile's dependencies and make sure you
own the whole tree (root-owned dirs from the container cause permission
errors).

## Builds

```sh
./staticpy build --verify core --pack                                    # native (host triple)
./staticpy build --target all --verify core --pack --workers 2 -j 8     # every target
./staticpy build --profile reference                                     # dynamic baseline
```

- No `--target` on a terminal opens a wizard and prints the equivalent
  command. Non-interactive runs (CI, pipes, `TERM=dumb`, `STATICPY_NO_TUI=1`)
  never prompt.
- Load is `--workers` × `-j`. Default is 4 workers, peaking ~11 GB RSS per
  target during the LTO link; measure before trusting it under 48 GB. A full
  sweep takes hours.
- Builds are fail-fast: one flake abandons the queue. Re-run; don't draw
  conclusions from a partial sweep.

## Sources

Every tarball is sha256-pinned in `config/sources.toml` in the same edit as
its version; a mismatched download is deleted. The checksum is in every
dependent job's key.

```sh
./staticpy sources list     # pins, and what is in dist/src
./staticpy sources fetch    # download missing, verifying (then --offline works)
./staticpy sources verify   # re-hash on disk, ignoring .done markers
```

## Long jobs: tmux on the host

The container has no tmux. Detach a host session that runs `docker compose
exec`, tees to a log, and records the **inner** exit code. Plain `$?` after a
pipeline is `tee`'s (always 0), so a failed build would log `EXIT_CODE=0`;
use `set -o pipefail` and `${PIPESTATUS[0]}`.

```sh
LOG=$PWD/build.log
tmux new-session -d -s build "bash -c '\
  set -o pipefail; \
  ./staticpy build --target all --verify core --pack --workers 2 -j 8 \
  2>&1 | tee $LOG; \
  echo EXIT_CODE=\${PIPESTATUS[0]} | tee -a $LOG'"
```

Poll the log; don't attach (the terminal is the user's). When done, require
`EXIT_CODE=0` **and** no match for `grep 'Error [0-9]\|FAILED'`. For
progress, `./staticpy status` works from any shell. Monitoring pitfalls:
`staticpy` skill, **Watching a long build or benchmark**.

## Benchmarking

```sh
./staticpy build --profile reference
./staticpy bench --interp static --interp reference --baseline reference
```

- **Natively only.** Under qemu you measure qemu, non-uniformly; `bench`
  refuses a foreign `--target`.
- **Check per-arm failure counts in `timeline.jsonl` before trusting a
  geomean.** One run rendered a plausible report with 43 of 45 measurements
  failed on one arm.
- Only named arms run. `--interp` takes `static` (this machine's pynative),
  `reference`, `system` (whatever `python3` is) or `label=/path/to/python`.
  `--baseline` fixes every ratio's denominator so adding an arm can't change it.
- Default suite is pyperformance, installed into a venv per arm
  (`--with-ensurepip=no` still leaves ensurepip in the stdlib, so venvs get
  pip). Benchmarks needing a C extension (no dlopen) or failing to install are
  listed in `skipped.json`. `--suite micro` is stdlib-only and offline; don't
  present its geomean as a workload number.
- Sessions land in `dist/bench/<UTC-stamp>-<arch>/`, never overwritten: report,
  raw pyperf JSON, a manifest with each binary's sha256 and linkage, and
  `timeline.jsonl` (per measurement: wall time, load average, SMT-sibling
  busy fraction). Committing sessions: `benchmarks/README.md`.

## Rules

- **Don't commit unless asked.** Then: read `git status` and `git diff`,
  summarise, and get a green light before `git add` / `git commit`.
- **Version and sha256 move in the same edit** of `config/sources.toml`. A pin
  without its checksum fetches an unverified tarball. Take the digest from
  upstream (`staticpy-bump`).
- **`config/sources.toml` and `config/patches/` are excluded from the
  `--config` overlay** so no stray file can redefine a checksum; builds read
  the embedded copy. `config/` is a symlink to
  `src/staticpy/internal/config/defaults/` (the `go:embed` tree). `./staticpy`
  rebuilds around an edit; plain `go build` does not and leaves you on the
  last embedded copy.
- **One-arch diffs go in `[source.<pkg>.target_patches]`** keyed by triple,
  not `patches`. `patches` is hashed into the shared srctree, so it
  invalidates every target; a target patch reaches one target's key. Check
  with `staticpy --json build --dry-run --target ...` before and after.
- **Don't overfit a sweep.** Never park a failure under
  `[expect.<this-triple>]` or a one-package `[package.X.profile.<this>]` to go
  green. Reproduce on a second arm, name the layer (recipe, emulator version,
  ABI), and find the complete fix; an ignore needs "unfixable" plus the
  experiment proving it. See `staticpy-traps`, **Do not overfit the last
  failure**.
- **Findings go in `staticpy-traps`.** Anything beyond a one-line comment (an
  upstream bug, a flag interaction, a root cause that took >15 min) goes in
  `.agents/skills/staticpy-traps/SKILL.md` as a symptom-first entry, or, if it
  needs a reproducer / disassembly / numbers / layered root cause, as a new
  `references/` file (format: title, one-paragraph summary, minimal
  reproducer, one section per root-cause layer, "what we ended up doing" /
  "what we'd want upstream") plus a linking entry. Not in commit messages or
  scratch files.
- **Comments: one line max at a fix**, pointing to the traps entry. Say *why*,
  not *what*; no stale framing ("now", "previously", "step N of the plan");
  no baked-in numbers unless the number is the point. Details:
  `comment-hygiene` skill.

## What "done" looks like

| task | done when |
|---|---|
| version bump | `config/sources.toml` edited with the new version and sha256, x86_64 static build green, `staticpy verify --level core` clean |
| toolchain change (re-publish from gccfactory) | the shim fetches it into `dist/toolchains/`, x86_64 static build green, sanity imports clean, and the banner from the new binary mentions the expected gcc version (`python3 -c 'import sys; print(sys.version)'`) |
| cross-arch interpreter fan-out | `staticpy status --target all` reports 0 stale and 0 missing, every target has a verify artifact with `failed=0`, every packed tarball's sha256 checks out, and at least one non-x86_64 arch benchmarked |
