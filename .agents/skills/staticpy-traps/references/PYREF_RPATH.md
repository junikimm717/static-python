# pyref: two hostccs, one prefix — and rpath to the leftover

## Summary

A pyref rebuild on Alpine dropped readline:
`checking for readline/readline.h... yes`, `checking for readline in
-lreadline... no`. `_curses` / `initscr` were fine. `config.log` had

```
ld: .../pyref_reference_x86_64-linux-musl/rootfs/lib/libncursesw.so.6:
    undefined reference to `__memset_chk@GLIBC_2.3.4'
```

That undefined ref is a **foreign leftover**, not a missing `-L` and not
"rpath must never name a published prefix." Alpine's `-lreadline` opened a
**glibc** `libncursesw.so.6` sitting at the musl-named slug from an earlier
Ubuntu hostcc. Same-toolchain leftover at that path would have been musl
`libncursesw.so.6` and the probe would have linked.

Two bugs, not one. Mixing two host compilers onto one prefix is what made
this fail. Baking `-rpath` at that prefix is what made `ld` look there.
Either fix alone stops *this* crash. Both stay: they are different
invariants.

## Minimal reproducer

This-build (Alpine `cc`) `libreadline.so`, found via `-L` (shadow):

```
NEEDED  libncursesw.so.6
NEEDED  libc.musl-x86_64.so.1
```

Published leftover at the unsuffixed slug (Ubuntu hostcc, still there
because the job key said stale and the directory did not move):

```
NEEDED  libc.so.6
```

CPython's check is `-lreadline` only. `ld` does not use `-L` for that
`.so`'s own `NEEDED`. Order is the input's `DT_RUNPATH` / `DT_RPATH`, then
command-line `-rpath`, then `-rpath-link`. Host-built flags used to rpath
the published prefix, so the walk hit the glibc file first.

`AC_SEARCH_LIBS(initscr, ncursesw)` is a direct `-lncursesw` and uses
`-L` (shadow, musl). Only the outer-library-only check fails.

Same leftover, same-toolchain: replace `NEEDED libc.so.6` with
`libc.musl-x86_64.so.1`. The probe links. After `rewriteRootfsRpaths` the
installed module records a SONAME, not a path, and runtime is `$ORIGIN`
in the new rootfs. The glibc symbol error cannot happen without a
foreign `.so` at the rpath.

## Layers

1. **Two host compilers shared a publish path.** The Merkle key already
   folded in the hostcc fingerprint, so Alpine treated the Ubuntu tree as
   stale and rebuilt. `ArtifactDir` was still
   `pyref_reference_x86_64-linux-musl`. The previous libc's `.so` files
   sat at `--prefix` and at `-rpath` for the whole rebuild. This is the
   layer that produced `@GLIBC_2.3.4`.

2. **`ld` NEEDED search ignores `-L`.** A rebuild's `-lreadline` therefore
   walks whatever `-rpath` names before the shadow `-rpath-link`. First
   build: published prefix is missing, `-rpath-link` wins. Rebuild:
   prefix is the previous artifact. Mixing made that leftover a different
   libc. Same hostcc, same SONAME: the walk still hits the *previous
   generation*, but it usually links. It becomes a configure miss only
   when that generation does not satisfy this-build `libreadline` (SONAME
   stayed, symbol set did not) — a dep bump, not a toolchain swap.

3. **`-rpath` existed so `rewriteRootfsRpaths` had a string longer than
   `$ORIGIN` to shrink.** The published path is not required for that.
   The shadow/view is also longer, exists during the build, and holds
   this generation. `--prefix` stays the published path (libtool /
   OPENSSLDIR); rpath is a different string.

4. **`Runner.Output` discarded stdout on failure**, so `assertModules`
   printed `could not run at all` instead of `readline`.

## What is and is not a valid fix

**Hostcc-keyed publish paths** (`_<12 hex>` of the hostcc key on pyref,
pack, verify, kit) plus `rejectForeignHostPrefix`: different host
compilers never write the same prefix. A leftover glibc tree at an
unsuffixed slug is not this job's `--prefix` or `-rpath`. This is the
fix for the observed crash. It does not make a same-toolchain rebuild
link against this generation rather than the last one.

**`-rpath` / `LDFLAGS_NODIST` on the shadow/view**: first build and
rebuild use the same search path, and a same-toolchain ncurses bump
cannot fail `-lreadline` against yesterday's `.so`. Valid independently
of mixing. Not what produced `__memset_chk@GLIBC_2.3.4`. Keep
`-rpath-link` on the same directories.

Keyed paths alone would have stopped this crash (Alpine would not rpath
Ubuntu's tree). Shadow rpath alone would also have stopped it (`ld`
would not open the leftover). Neither substitutes for the other.

Do not bump `recipe.Version`: the path is absolute and not in the key;
host toolchain identity already invalidates a glibc artifact when
`hostcc` changes. Do not skip readline. Do not `[expect]` it. Do not
treat "delete the stale artifact" as the fix. Do not rebuild
`--profile reference` inside Alpine if the baseline you want is glibc.
