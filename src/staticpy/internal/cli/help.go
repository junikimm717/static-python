package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

const overview = `staticpy builds a fully static CPython against musl: one relocatable binary
with no loader, no libpython, and every extension module linked in - because a
static interpreter cannot dlopen one, so a C module either arrives at link time
or not at all. It cross-compiles the same interpreter for every triple in
targets.toml.

Everything is content-addressed. Each job has a key hashed over its sources,
patches, flags, triples and its dependencies' keys, so rebuilding with identical
inputs is a no-op, changing one configure flag rebuilds exactly what depends on
it, and two staticpy processes may share one dist/ safely.

staticpy never fetches a compiler. Toolchains, busybox and qemu are handed to it
by the ./staticpy shim (or by you, with flags); it fails loudly when one is
missing rather than falling back to whatever the host happens to have. Upstream
sources it does fetch, and only over a matching sha256.`

const globalHelp = `GLOBAL FLAGS (accepted before or after the command)
  --dist DIR          artifact + build root. Default ./dist, or <repo>/dist from
                      a checkout; the ./staticpy shim always sets it. Everything
                      staticpy writes lives under here and is safe to delete.
  --config DIR        extra configuration layer, overlaid last. See
                      ` + "`staticpy help config`" + `.
  --sources DIR       read sources.toml and patches/ from here instead of from
                      this binary. This is a supply-chain override - it can
                      redefine a pinned checksum - so it is never picked up
                      implicitly, it warns when used, and it is recorded in the
                      provenance of every artifact built with it.
  --toolchains DIR    directory of provisioned toolchains, one <triple>-cross or
                      <triple>-native subdirectory each.
  --toolchain T=PATH  use this tree for triple T. Repeatable. Wins over
                      --toolchains, for testing one target against a hand-built
                      compiler.
  --busybox PATH      busybox binary supplying sh/awk/sed to hermetic builds.
                      Defaults to whatever is on PATH; the shim passes the one
                      it found. It is deliberately never downloaded: fetching an
                      unpinned binary onto a build PATH would undercut the
                      checksums the rest of the system depends on.
  --qemu T=PATH       qemu-user binary for running triple T's binaries.
                      Repeatable. Only verification needs it. Without one, the
                      target's qemu name from targets.toml is looked up on PATH.
  --profile NAME      flag profile from profiles.toml (default "default").
  --host TRIPLE       the build machine's triple. Inferred from this machine's
                      architecture; pass it only when that inference is
                      ambiguous, as it is between arm hard- and soft-float.
  --target TRIPLE     what to build for. Repeatable, comma-separated accepted,
                      and "all" or "proven" expand to those sets. With no
                      --target the build is for this machine's own triple.
  --workers N         how many jobs to build at once (default: up to 4).
  -j N                -j handed to each job's make (default: CPUs / workers).
                      Workers times -j is what actually loads the machine, so
                      the two defaults multiply out to about one job per CPU.
  --offline           never touch the network; only sources already verified in
                      dist/src may be used. Fails immediately instead of hanging
                      on a mirror that is not there.
  --hermetic          compose PATH from busybox and the selected toolchain only.
                      On by default when a busybox is available, because that is
                      what makes an artifact reproducible on another machine.
  --no-hermetic       let the host PATH through: friendlier on a dev box,
                      reproducible nowhere.
  --keep-work         keep dist/work/<job>/ after a job succeeds, so
                      ` + "`staticpy shell <slug>`" + ` has a build tree to land in.
  -v, --verbose       mirror every command's output to the terminal as it runs.
                      Without it the output still goes to dist/logs in full.
  --json              machine-readable output, where the command has a JSON form.
  --color WHEN        auto|always|never (default auto: colour only on a terminal).
  --git-revision SHA  commit this executable was built from. The ./staticpy shim
                      stamps it with -X and passes the same value here.`

const layoutHelp = `WHERE THINGS LAND under dist/
  artifacts/<slug>/                 published job outputs. Present means complete:
                                    a directory is renamed into place with its
                                    manifest written last, so a crash leaves no
                                    half-built artifact behind.
  out/                              the interpreters and tarballs you came for
  src/                              verified upstream tarballs (keep these)
  srctrees/                         extracted + patched source trees
  logs/jobs/<slug>/<attempt>/       every command a job ran, with cwd and env
  logs/jobs/<slug>/latest           symlink to the newest attempt
  logs/runs/<stamp>-<pid>/run.jsonl one invocation's structured event stream
  locks/, state/heartbeats/         cross-process coordination
  work/, .staging/, .trash/         scratch; safe to delete
  toolchains/                       what the shim provisioned
  .bin/staticpy                     the binary the shim builds and runs`

func targetsHelp() string {
	cfg, err := config.Load(config.Options{})
	if err != nil {
		return "TARGETS\n  (targets.toml could not be read: " + err.Error() + ")\n"
	}
	var b strings.Builder
	b.WriteString("TARGETS from targets.toml\n")
	for _, name := range sortedKeys(cfg.Targets) {
		t := cfg.Targets[name]
		mark := "  "
		if t.Status == "proven" {
			mark = " *"
		}
		fmt.Fprintf(&b, "  %s %-24s %-12s %d-bit  qemu %s\n", mark, t.Triple, t.Arch, t.Bits, t.Qemu)
	}
	b.WriteString("  (* = proven: built and tested regularly. The rest are experimental\n")
	b.WriteString("   and do not gate CI, which is a statement about coverage, not about\n")
	b.WriteString("   whether they work.)\n")
	return b.String()
}

func printHelp(w io.Writer, topic string) {
	switch topic {
	case "":
		fmt.Fprintln(w, bold("staticpy")+" - a fully static CPython, from source, for every musl target")
		fmt.Fprintln(w)
		fmt.Fprintln(w, overview)
		fmt.Fprintln(w)
		fmt.Fprintln(w, bold("USAGE"))
		fmt.Fprintln(w, "  staticpy [global flags] <command> [flags]")
		fmt.Fprintln(w)
		fmt.Fprintln(w, bold("COMMANDS"))
		for _, c := range commands() {
			fmt.Fprintf(w, "  %-8s %s\n", c.Name, c.Short)
		}
		fmt.Fprintf(w, "  %-8s %s\n", "help", "this, or `help <command>` for the details")
		fmt.Fprintln(w)
		fmt.Fprintln(w, bold("GETTING STARTED"))
		fmt.Fprintln(w, "  ./staticpy doctor                 what this machine has and is missing")
		fmt.Fprintln(w, "  ./staticpy build                  a static interpreter for this machine")
		fmt.Fprintln(w, "  ./staticpy build --target aarch64-linux-musl --verify smoke")
		fmt.Fprintln(w, "  ./staticpy status                 what exists, what is stale, what is running")
		fmt.Fprintln(w, "  ./staticpy logs <slug> --follow   watch a job as it builds")
		fmt.Fprintln(w)
		fmt.Fprintln(w, globalHelp)
		fmt.Fprintln(w)
		fmt.Fprintln(w, layoutHelp)
		fmt.Fprintln(w)
		fmt.Fprint(w, targetsHelp())
		fmt.Fprintln(w)
		fmt.Fprintln(w, dim("more: `staticpy help layout`, `staticpy help targets`"))
		return
	case "layout":
		fmt.Fprintln(w, layoutHelp)
		return
	case "targets":
		fmt.Fprint(w, targetsHelp())
		return
	}
	c := lookup(topic)
	if c == nil {
		fmt.Fprintf(w, "no such command %q\n", topic)
		return
	}
	fmt.Fprintln(w, bold("staticpy "+c.Name)+" - "+c.Short)
	fmt.Fprintln(w)
	fmt.Fprintln(w, bold("USAGE"))
	fmt.Fprintln(w, "  "+c.Synopsis)
	if c.Long != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, strings.TrimRight(c.Long, "\n"))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, globalHelp)
}
