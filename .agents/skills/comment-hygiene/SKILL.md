---
name: comment-hygiene
description: How to run a comment sweep over src/staticpy without losing knowledge or silently changing code — the AST-equivalence harness that proves only comments moved, the delete/keep/trim taxonomy, this repo's two string-literal hazards, and the failure mode every sweep here has hit so far. Use when asked to strip, prune or clean up comments, when reviewing an AI-authored diff for narration, or before adding a comment to a fix.
---

# Comment hygiene

Two failure modes, opposite directions, both expensive. **Narration** — a
comment per line restating the line — is noise that goes stale and buries the
comments that matter. **Knowledge loss** — deleting `// musl has no
byte-level case folding` — costs the next person a day of rediscovery.

## Build the harness first

Never sweep without a way to prove you only moved comments. Reviewing a
200-line comment diff by eye does not catch a subagent that improved a variable
name along the way.

```sh
cp -r .agents/skills/comment-hygiene/stripcmt /tmp/sc
(cd /tmp/sc && go mod init stripcmt >/dev/null 2>&1; go build -o /tmp/stripcmt .)
/tmp/stripcmt ./src > /tmp/before.txt      # BEFORE any edit
# ... sweep ...
/tmp/stripcmt ./src > /tmp/after.txt
diff /tmp/before.txt /tmp/after.txt
```

It prints every Go file's AST with comments discarded, so a comment-only edit
produces byte-identical output. The only acceptable difference is **blank
lines**: `go/printer` preserves the vertical gap a comment occupied, so
deleting one that sat alone between two statements shows as one removed blank
line. Anything with a token in it means code moved — find it and revert it.

Run from the repo root; `stripcmt` takes a relative path and silently emits
nothing if the working directory is wrong, which looks like a total rewrite in
the diff.

Then `go build ./...` and `go vet ./...`. Cheap, and it catches a deleted
`//go:` directive.

## The three options

Most guidance treats this as keep-or-delete. It is not — there are three, and
the third is the one sweeps here keep skipping.

**Delete.** Narration restating the next line, step numbering, banner
dividers, tautological doc comments (`// Close closes.`), explanations of Go or
stdlib idioms, commented-out code, changelog narration (`// previously we did
Y`, `// NEW:`).

**Keep verbatim.** Anything stating *why*: a constraint, a workaround, an
upstream bug, an invariant. Especially the concurrency and correctness notes in
`internal/core` — lease and flock semantics, atomic-rename publish, what feeds a
cache key. Deleting one of those is how a race gets reintroduced.

**Trim the dead leading sentence.** The most common shape here is a doc comment
that opens by restating the signature and only then says something real. Delete
the opener, keep the rest, reword minimally:

    // LockPath is the flock file for a job slug. Lock files are never deleted:
    // removing one would break flock identity for anyone holding it open.
    ->
    // Lock files are never deleted: removing one would break flock identity for
    // anyone holding it open.

Where cutting the opener strands a pronoun or leaves a subjectless fragment,
reword the survivor into a standalone sentence. `// Built into its own prefix.`
is a trim that forgot this half.

Go's "every exported identifier gets a doc comment starting with its name"
convention is **explicitly overridden** in this repo's `internal/` packages.
"It's exported" is never a reason to keep, and never a tie. Do not cite golint
or godoc.

## The failure mode this repo has actually hit

Every sweep so far has come back near-zero on the first pass — two removals
across 1,970 lines, three across 2,150, four across 2,466 — because the agents
applied only the binary test and never the trim. The sibling repo has the same
history: a sweep on "when unsure, keep" alone removed one comment from 13k lines
and was rejected.

"When unsure, keep" is the tiebreaker for genuine ties. It is not a licence to
keep everything, and it is not a substitute for asking whether the first
sentence is doing any work.

The opposite failure is real too: agents inventing work delete WHY-comments and
rephrase things. Read the *category* of what came back, not the count. Removals
of accessor docs and dead openers are real work; removals of rationale are not.

## Two hazards specific to this tree

**`internal/cli` is ~3% comments because the help text is the documentation,
and it lives in string literals.** Every command has a `Long:` field; `help.go`
is almost entirely literals; error messages are written to be actionable. String
literals are code. A large number of removals from `cli` is a red flag, not a
win, and several of its files should come back at zero.

**`internal/gen/staticapi.go` emits C through string literals**, including
comments that end up in the generated `symbols.c`. Same rule — those are output,
not commentary.

## Running it with subagents

Partition by package, one agent per group, all spawned in a single message.
Roughly: `recipe`, `ensure`, `cli`, `core`+`logging`, `config`+`sources`,
`gen`+`assets`.

Each prompt must carry its exact file list and "edit only these"; the absolute
rule that only comments and the blank lines they leave may change, with the AST
check named so the agent knows it is verified; the delete/keep/trim taxonomy
**with concrete examples from that agent's own files**; "when unsure, keep";
"removing zero from a file is a perfectly good outcome — do not manufacture
removals"; `Edit` only, never `sed` or a python replace (a blind replace
silently no-ops on a bad anchor and reports success); and a report quoting every
change, before and after, so a trim is visible as a trim.

Naming the sacred comments per package is what stops the shredding. A `core`
agent needs to be told the flock note is load-bearing; a `sources` agent needs
to be told the sha256-before-`.done` note is.

## Traps

**Account for every changed file afterwards, not just the expected ones.**
`git status` — the harness only covers `.go` files, so a changed `go.mod` or a
touched asset is invisible to it.

**Actively-wrong comments are the real find.** A comment describing code that no
longer exists should be *fixed*, not deleted, and reported separately. Today's
sweep turned up a duplicated verb in `writeExterns`' doc left over from an
earlier edit. An agent that flags one rather than drive-by fixing it is doing
the right thing — a sweep's remit is comments, and rewriting prose mid-sweep
hides real changes in a large diff.

## Writing comments in the first place

Repo rule, from `AGENTS.md`: when you have found a fix, **one line of comment
max**. The impulse after a hard debugging session is to write a paragraph
explaining the journey. Don't. One line at the fix; the full story goes in
`staticpy-traps`, where it is searchable by symptom -- as an entry in its
`SKILL.md`, or as a write-up under its `references/` if it needs a reproducer.
