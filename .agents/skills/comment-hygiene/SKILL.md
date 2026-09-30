---
name: comment-hygiene
description: How to run a comment sweep over src/staticpy without losing knowledge or silently changing code — the AST-equivalence harness that proves only comments moved, the delete/keep/trim taxonomy, this repo's two string-literal hazards, and the failure mode every sweep here has hit so far. Use when asked to strip, prune or clean up comments, when reviewing an AI-authored diff for narration, or before adding a comment to a fix.
---

# Comment hygiene

Two opposite failures: **narration** (comments restating the line, which go
stale and bury the useful ones) and **knowledge loss** (deleting
`// musl has no byte-level case folding` costs someone a day).

## Build the harness first

Never sweep without proof that only comments moved; eyeballing a 200-line
diff misses a subagent "improving" a variable name.

```sh
cp -r .agents/skills/comment-hygiene/stripcmt /tmp/sc
(cd /tmp/sc && go mod init stripcmt >/dev/null 2>&1; go build -o /tmp/stripcmt .)
/tmp/stripcmt ./src > /tmp/before.txt      # BEFORE any edit
# ... sweep ...
/tmp/stripcmt ./src > /tmp/after.txt
diff /tmp/before.txt /tmp/after.txt
```

- It prints every Go file's AST without comments. The only acceptable diff is
  **blank lines** (`go/printer` keeps the gap a lone comment occupied). Any
  token in the diff means code moved: revert it.
- Run from the repo root. With a wrong cwd it silently emits nothing, which
  looks like a total rewrite.
- Then `go build ./...` and `go vet ./...` (catches a deleted `//go:`
  directive).

## Delete, keep, or trim

**Delete:** narration of the next line, step numbering, banner dividers,
tautological docs (`// Close closes.`), explanations of Go/stdlib idioms,
commented-out code, changelog narration (`// previously we did Y`, `// NEW:`).

**Keep verbatim:** anything stating *why* — a constraint, workaround,
upstream bug, invariant. Especially `internal/core`'s concurrency and
correctness notes (lease/flock semantics, atomic-rename publish, what feeds a
cache key); deleting those reintroduces races.

**Trim the dead leading sentence** — the option sweeps here keep skipping. A
doc comment that restates the signature, then says something real, loses the
opener:

    // LockPath is the flock file for a job slug. Lock files are never deleted:
    // removing one would break flock identity for anyone holding it open.
    ->
    // Lock files are never deleted: removing one would break flock identity for
    // anyone holding it open.

If cutting strands a pronoun or leaves a fragment, reword into a standalone
sentence (`// Built into its own prefix.` is a trim that forgot this).

Go's "every exported identifier gets a doc comment" convention is
**explicitly overridden** in `internal/`. "It's exported" is never a reason
to keep, nor a tie-breaker. Don't cite golint or godoc.

## The failure this repo keeps hitting

Every first-pass sweep came back near zero — 2 removals in 1,970 lines, 3 in
2,150, 4 in 2,466 — because agents applied keep/delete and never trim. The
sibling repo's "when unsure, keep" sweep removed one comment from 13k lines and
was rejected. "When unsure, keep" is for genuine ties, not a licence to keep
everything.

The opposite also happens: agents inventing work delete WHY-comments and
rephrase. Judge the *category* of removals, not the count: accessor docs and
dead openers are real work; rationale is not.

## Two hazards in this tree

- **`internal/cli` is ~3% comments** because its docs are string literals
  (`Long:` fields, `help.go`, actionable error messages). String literals are
  code. Many removals from `cli` is a red flag; several files should come back
  at zero.
- **`internal/gen/staticapi.go` emits C via string literals**, including
  comments that land in generated `symbols.c`. Those are output.

## Running it with subagents

One agent per package group, all spawned in one message: `recipe`, `ensure`,
`cli`, `core`+`logging`, `config`+`sources`, `gen`+`assets`. Each prompt
carries:

- its exact file list and "edit only these"
- the absolute rule: only comments and the blank lines they leave change,
  verified by the AST check (name it)
- the delete/keep/trim taxonomy **with examples from that agent's own files**,
  and the sacred comments for its package (the flock note for `core`, the
  sha256-before-`.done` note for `sources`) — naming them stops the shredding
- "when unsure, keep"; "zero removals from a file is fine — don't manufacture
  them"
- `Edit` only, never `sed` or a python replace (a blind replace no-ops on a bad
  anchor and reports success)
- a report quoting every change before and after, so trims are visible

## Traps

- **Account for every changed file** via `git status`: the harness only sees
  `.go`, not `go.mod` or assets.
- **Actively-wrong comments are the real find.** A comment describing code
  that no longer exists should be *fixed*, not deleted, and reported
  separately (one sweep found a duplicated verb in `writeExterns`' doc). Flag
  rather than drive-by rewrite — prose rewrites mid-sweep hide real changes.

## Writing comments on a fix

Repo rule (`AGENTS.md`): **one line of comment max** at a fix. The full story
goes in `staticpy-traps`, searchable by symptom — a `SKILL.md` entry, or a
`references/` write-up if it needs a reproducer.
