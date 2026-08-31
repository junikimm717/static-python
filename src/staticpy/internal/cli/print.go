package cli

import (
	"fmt"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/buildinfo"
	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/recipe"
)

var cmdPrint = &command{
	Name:     "print",
	Short:    "print one resolved value, for scripts",
	Synopsis: "staticpy print <key>",
	Long: `Prints a single value and nothing else - no label, no colour, one line - so a
shell script can read it without parsing a table. Everything print reports comes
from the same resolved configuration a build uses, which is the point: a script
that asks staticpy for the Python version cannot disagree with the interpreter
staticpy built.

KEYS
  python-version    the pinned CPython version, e.g. 3.x.y
  python-abi        its major.minor, e.g. 3.x - the interpreter's directory and
                    binary suffix (bin/python3.x, lib/python3.x)
  host              this machine's target triple
  targets           the targets this invocation selects, space separated
  targets-all       every triple in targets.toml
  targets-proven    the triples marked proven, which are the ones CI gates on
  profile           the selected profile name
  dist              the absolute artifact root
  toolchains        the toolchain directory in use
  recipe-version    the recipe generation; every job key includes it
  git-revision      the commit stamped into this executable (-X / --git-revision)
  version:<name>    the pinned version of one source, e.g. version:openssl
  sha256:<name>     its pinned checksum

  staticpy print python-abi   ->  3.x`,
	Run: runPrint,
}

func runPrint(g *Global, args []string) error {
	fs := g.flagSet("print")
	if err := parse(fs, args); err != nil {
		return finish("print", err)
	}
	g.applyGitRevision()
	if fs.NArg() == 0 {
		return usagef("need a key")
	}
	if fs.NArg() > 1 {
		return usagef("one key at a time; got %d", fs.NArg())
	}
	key := fs.Arg(0)
	cfg, err := g.load()
	if err != nil {
		return err
	}

	if name, ok := strings.CutPrefix(key, "version:"); ok {
		s, err := lookupSource(cfg, name)
		if err != nil {
			return err
		}
		fmt.Println(s.Version)
		return nil
	}
	if name, ok := strings.CutPrefix(key, "sha256:"); ok {
		s, err := lookupSource(cfg, name)
		if err != nil {
			return err
		}
		fmt.Println(s.SHA256)
		return nil
	}

	switch key {
	case "python-version", "python-abi":
		s, err := lookupSource(cfg, "python")
		if err != nil {
			return err
		}
		if key == "python-version" {
			fmt.Println(s.Version)
			return nil
		}
		parts := strings.Split(s.Version, ".")
		if len(parts) < 2 {
			return fmt.Errorf("the pinned python version %q has no major.minor to take an ABI from", s.Version)
		}
		fmt.Println(parts[0] + "." + parts[1])
		return nil
	case "host":
		host, err := g.HostTriple(cfg)
		if err != nil {
			return err
		}
		fmt.Println(host)
		return nil
	case "targets":
		host, err := g.HostTriple(cfg)
		if err != nil {
			return err
		}
		targets, err := g.selectTargets(cfg, host)
		if err != nil {
			return err
		}
		fmt.Println(strings.Join(targets, " "))
		return nil
	case "targets-all":
		fmt.Println(strings.Join(sortedTriples(cfg), " "))
		return nil
	case "targets-proven":
		var out []string
		for _, n := range sortedKeys(cfg.Targets) {
			if cfg.Targets[n].Status == "proven" {
				out = append(out, cfg.Targets[n].Triple)
			}
		}
		fmt.Println(strings.Join(out, " "))
		return nil
	case "profile":
		fmt.Println(g.Profile)
		return nil
	case "dist":
		fmt.Println(g.Dist)
		return nil
	case "toolchains":
		fmt.Println(g.Toolchains)
		return nil
	case "recipe-version":
		fmt.Println(recipe.Version)
		return nil
	case "git-revision":
		fmt.Println(buildinfo.GitRevision)
		return nil
	}
	return usagef("unknown key %q", key)
}

func lookupSource(cfg *config.Config, name string) (config.Source, error) {
	s, ok := cfg.Sources[name]
	if !ok {
		return config.Source{}, fmt.Errorf("no source named %q is pinned.\nPinned sources: %s", name, strings.Join(sortedKeys(cfg.Sources), ", "))
	}
	return s, nil
}
