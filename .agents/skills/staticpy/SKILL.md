---
name: staticpy
description: Work on this repo's Go build system (src/staticpy) — the job DAG, the content-addressed artifact store, where a version/flag/library/package is changed, where artifacts and per-command logs land, how to re-enter a failed job's exact environment, and which invariants must never be broken. Start here for ANY question about this repo — it has a lookup table that answers most of them without scanning the codebase. Use whenever touching src/staticpy or config/*.toml, running ./staticpy, adding a job family or a package, or working out why a build did or did not rebuild.
---

# staticpy

## Orientation

`src/staticpy` is the build system. It owns the job in **data** rather than
recipes — `config/*.toml` holds versions, checksums, per-package configure args,
per-target quirks and flag profiles — and every job's key is hashed over its
inputs and its dependencies' keys, so an edit rebuilds exactly what depends on
it. `config/sources.toml` is the single place a version and its sha256 live.

`./staticpy help` and each command's `Long:` in `internal/cli/*.go` are the
user-facing documentation and are kept authoritative. Read them before writing
new docs, and update them when behaviour changes.

```
staticpy                 sh shim; provisions the execution environment and
                         nothing else. Rebuilds the binary from an mtime check
                         over *.go only (see the trap below).
config/*.toml            the data: sources, packages, targets, profiles,
                         bundles, test expectations. Overlaid onto an embedded
                         copy of the same files.
src/staticpy/internal/
  config/                loader, layering, scope resolution, validation
  sources/               fetch + sha256 + extract + patch + content-anchored edits
  core/                  Job, Env, Merkle keys, flock leases, atomic publish, Runner
  recipe/                the job families; recipe.go holds the DAG
  gen/                   Setup.local, staticapi symbols, dotted-module shims
  ensure/                verification: ELF identity, imports, CPython's suite
  assets/files/          go:embed'd: per-target pyconfig fragments, Setup, probe
  cli/                   commands; the `Long:` fields are the real docs
dist/                    everything generated; gitignored, safe to delete
```

## Answer questions from here, not from a scan

| question | answer |
|---|---|
| what commands and flags exist | `./staticpy help`, `./staticpy help <cmd>`, `help layout`, `help targets` — authoritative |
| bump a pinned version | `config/sources.toml` (version + file + urls + topdir + **sha256 in the same edit**) |
| change a package's configure flags | `config/packages.toml`. Only *decisions* live there: `--prefix`, `--exec-prefix`, `--host` are injected by the recipe because they are absolute or triple-derived |
| change a compiler/linker flag | `config/profiles.toml`. Profile-wide, then scoped: `deps`, `deps.<pkg>`, `python`, `pyhost`. A scoped change rebuilds only what that scope reaches |
| add a native library | a `[source.X]` in `config/sources.toml` + a `[package.X]` in `config/packages.toml` (`build` = autotools\|openssl\|make\|sources, `needs`, `provides`). `Deps` picks up every package automatically; `sysroot` composes them |
| add a Python package / bundle | `config/bundles.toml`: a `[pkg.X]` with sdist + sha256 + `[[pkg.X.modules]]`, then a `[bundle.Y]` naming it. Nothing is defined yet. A static interpreter cannot dlopen, so a C module arrives at link time or not at all |
| add an architecture | a row in `config/targets.toml` **plus** `internal/assets/files/pyconfig/<triple>-patches.h` — see the `staticpy-add-target` skill |
| where do artifacts land | `dist/artifacts/<slug>/` (published job outputs, incl. each interpreter prefix); `dist/out/<profile>/<triple>/` (tarballs); `dist/src/` (verified tarballs); `dist/srctrees/` |
| logs for a failed job | `dist/logs/jobs/<slug>/latest/` — `NNN-<step>.log` per command plus `commands.sh`; read them with `staticpy logs <slug> --failed` |
| a shell in a job's exact environment | `staticpy shell <slug>` (add `--step NAME`, or `--print` to just see the env). Recovered from the recorded attempt, so it works long after the process is gone |
| what invalidates a rebuild | a job's Merkle key: its `KeyInputs` (recipe version, source sha256s, decision flags, triples, resolved profile) plus every dependency's key. Not timestamps. `staticpy status` calls the difference `stale` |
| what would a build do right now | `staticpy status [--todo]` — pass the same `--verify`/`--pack` you would pass to `build`, or you are not looking at the same plan |
| what this machine is missing | `staticpy doctor`. `perl` is the one irreducible host dependency (OpenSSL's Configure); busybox covers `sh`/`awk`/`sed`/`patch` |
| a resolved value, for a script | `staticpy print <key>` — `python-version`, `python-abi`, `host`, `targets{,-all,-proven}`, `dist`, `recipe-version`, `version:<src>`, `sha256:<src>` |
| what the flags actually resolve to | `staticpy config show [--profile N] [--scope S]`, which also names the file each layer came from |
| why a built interpreter misbehaves | the `staticpy-traps` skill — symptom first |

## The DAG

From `internal/recipe/recipe.go`'s package comment, which is the spine:

```
srctree:<pkg>-<ver>        extracted + patched source          (internal/sources)
probe:<T>                  target ABI sizes -> config.site
dep:<prof>:<T>:<pkg>       one native library, own prefix
sysroot:<prof>:<T>         the -I/-L view composed from dep artifacts
pyhost:<ver>               static-musl CPython that runs on the build machine
pynative:<prof>:<T>        the shipped interpreter, host == target
pycross:<prof>:<H>:<T>     the shipped interpreter, host != target
pack:<prof>:<T>            the distributable tarball
```

Two edges carry the design:

**`pycross` depends on `pyhost`, never on `pynative`.** A cross build needs a
runnable same-version interpreter to freeze bytecode, and that is all it needs;
gating it on a full PGO release build of the host is what used to make one cross
target cost an hour of unrelated work. `pyhost` is built with the fixed
`bootstrap` profile and a minimal module set, because it is a means and not an
output.

**Every dep installs into its own prefix, and `sysroot` composes them.** Nothing
installs into a shared accumulator — that is how a stale `libz.a` from an older
version survived a bump and linked into everything after it. `sysroot` is only
the *view*, so bumping one library rebuilds one library.

`probe` is profile-free: sizes and alignments are ABI properties, not flag
properties. Its output is a `config.site`, so CPython's own configure computes
`pyconfig.h` rather than being bypassed and the header patched afterwards.

`Plan` in `recipe.go` is the only place the graph's shape is decided; the CLI
never constructs a job.

## Data, code, generated

Changeable with no Go compiler in the loop: `config/*.toml` at the repo root.
Resolution is **embedded defaults → `<repo>/config` → `--config <dir>`**, later
layers winning per top-level entry, so a profile redefined on disk replaces the
embedded one of the same name and profiles only the embedded set knows about
survive untouched.

`sources.toml` and `patches/` are deliberately **outside** that stack. They come
from the copy embedded in the binary unless `--sources <dir>` is passed
explicitly: if any `config/` next to the binary could redefine a sha256, pinning
would document what was downloaded rather than constrain it. `--sources` warns,
and is recorded in `Manifest.Provenance` of every artifact built with it — a
build that took a weaker path must never look identical to one that did not.

**The rebuild trap.** The shim rebuilds the binary when any `*.go` is newer than
it — and only `*.go`. Edits to `internal/config/defaults/*.toml`,
`internal/config/defaults/patches/`, or `internal/assets/files/` (pyconfig
fragments, `Setup`, `patcher.c`, staticapi) are `go:embed` inputs that the mtime
check does not see, so they silently do not take effect. Touch a `.go` file or
delete `dist/.bin/staticpy` after changing one. Note that a repo-root
`config/sources.toml` edit does nothing on its own for the same reason plus the
overlay exclusion above: pass `--sources config`.

## Invariants — do not break these

1. **An artifact directory is either absent or complete.** It is published by a
   single rename with the manifest (`.staticpy.json`) written last, so a crash,
   a `SIGKILL` or a full disk can never leave a half-built directory that the
   next run mistakes for a finished one. Jobs write only into `work` and
   `stage`; core stamps the manifest itself and a recipe must never write it.
2. **A job rebuilds only when its Merkle key changes, so `KeyInputs` must
   contain no absolute path and nothing that varies per run.** An absolute
   prefix makes the cache machine-specific; a timestamp or a pid makes it
   useless. Everything behind a dependency is already covered by that
   dependency's key, so do not re-hash it.
3. **Two staticpy processes may share one `dist/` safely.** Content keys +
   `flock` leases + atomic rename. Do not add process-global state that assumes
   otherwise, and never delete a file in `dist/locks/` — removing one breaks
   flock identity for anyone holding it open.
4. **`recipe.Bind(env)` must be called before `recipe.Plan`.** `KeyInputs` gets
   no `Env`, so without the bind the toolchain's identity never reaches the key,
   and artifacts built by a compiler nobody can name any more keep being served
   across a gccfactory re-publish.
5. **The shim provisions, the binary consumes.** staticpy never fetches a
   toolchain, a busybox or a qemu; it is handed paths and fails loudly when one
   is missing. That is what lets the same binary run against a volume mount, a
   gccfactory checkout or a musl.cc unpack — and busybox specifically is never
   downloaded, because an unpinned binary on the build PATH would undercut the
   checksums everything else depends on.
6. **Content-anchored edits assert their match count.** `Edit.MustMatch` (zero
   meaning exactly once) makes a moved anchor a loud failure instead of a silent
   no-op or a double application. Edits are the exception; `patches/` holds real
   diffs and `patch` already fails when its context moves. Reach for an Edit only
   where the fixup must survive an unreviewed version bump — the ctypes
   injection is the case that earns it.
7. **Every command goes through the Runner** (`core.Cmd`). Anything exec'd
   around it is invisible in `dist/logs` and absent from `commands.sh`.
8. **Bump `recipe.Version` by hand** when the *procedure* changes in a way the
   flags do not capture — a new step, a different ordering, a changed install
   layout. It is in every key, so it rebuilds the world.

## The debug loop

You should never be grepping a multi-megabyte undifferentiated log. Every
command a job runs is its own file, headed with cwd, the overlaid environment
and the exact argv.

```sh
./staticpy doctor                  # host requirements, per target: buildable vs runnable
./staticpy status --todo           # what a build would actually do, before it does it
./staticpy logs <slug> --failed    # the tail of the step the job died on
./staticpy logs <slug> --step configure
./staticpy logs <slug> --follow    # works while another process is building it
./staticpy shell <slug>            # that job's exact env and cwd
```

`dist/logs/jobs/<slug>/latest/commands.sh` is the whole run as a replayable
script: copy the one failing command out of it and iterate inside
`staticpy shell <slug>`. Attempt directories are never reused, so a passing
rebuild does not erase the evidence from the failure before it. The work tree
is deleted when a job succeeds — rebuild with `--keep-work` if `shell` needs
somewhere to land. Slugs are exactly the names `staticpy status` prints, e.g.
`dep:default:x86_64-linux-musl:openssl`.

`staticpy verify` refuses to build by default: it is for asking whether what is
on disk is good, not for starting an hour of work. Levels are `smoke` (import
probes, seconds, the gate every target must pass), `core` (language core plus
every hand-linked extension module) and `full` (CPython's suite, hours under
qemu). Results are content-addressed and kept as `report.json` in the verify
job's artifact.

## State of play

Honest inventory, because the code reads more finished than it is:

- **No interpreter has ever been built with staticpy.** It compiles, its config
  validates, and `doctor` reports what a build would need. Treat it as the thing
  being built, not the thing you build with.
- **The cross path is unproven.** `pycross` configures with `--build`/`--host`/
  `--with-build-python` pointed at `pyhost`; nothing has run it end to end.
  `--with-build-python`'s version check is where a mismatched `pyhost` would
  first surface.
- **PGO is native-only in practice.** CPython runs `PROFILE_TASK` as
  `./python …` with no HOSTRUNNER, so `pgo = "on"` means `native-only` for a
  cross build.
- **The per-target pyconfig fragments are still the cross-check for the ABI
  probe.** `probe` measures what is measurable; the fragments carry what is a
  decision (inline asm availability, atomics quirks). Adding a target without
  one is a hard error by design.
- **Bundles are declared but empty.** `config/bundles.toml` defines no `[pkg.*]`,
  so `--bundle` has nothing to select yet — and pyperformance benchmarking waits
  on it, because there is no pip in a `--with-ensurepip=no` interpreter.

## Related

- `staticpy-traps` — symptom-to-cause catalogue, with the long bug write-ups in
  its `references/`. **Read it before debugging anything that builds but
  misbehaves, and write what you find there.**
- `staticpy-add-target` — adding and proving a new triple.
- `comment-hygiene` — this repo's comment rule: say *why*, not *what*; no stale
  framing; no baked-in numbers unless the number is the point. Anything longer
  than a few lines goes in `staticpy-traps`, linked from the code by one line.
