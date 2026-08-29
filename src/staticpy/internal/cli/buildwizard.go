package cli

import (
	"fmt"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/ensure"
	"github.com/junikimm717/static-python/src/staticpy/internal/tui"
)

// buildOpts are the build flags the wizard may fill in; each points at the
// variable the flag would have set.
type buildOpts struct {
	verify *string
	pack   *bool
	bundle *string
}

// runBuildWizard walks the choices `build` was not given. It runs only when
// --target is absent, so the printed equivalent command - which always pins
// --target - is also what skips the wizard entirely.
func runBuildWizard(g *Global, o buildOpts) error {
	cfg, err := g.load()
	if err != nil {
		return err
	}
	host, err := g.HostTriple(cfg)
	if err != nil {
		return err
	}
	stages, err := buildStages(g, cfg, host, o)
	if err != nil {
		return err
	}
	return tui.Wizard("staticpy build", stages)
}

func buildStages(g *Global, cfg *config.Config, host string, o buildOpts) ([]tui.Stage, error) {
	targets, err := targetMenu(g, cfg, host)
	if err != nil {
		return nil, err
	}
	stages := []tui.Stage{
		{
			Multi: true,
			Menu:  func() tui.Menu { return targets },
			Apply: func(vs []string) []string {
				g.Targets = vs
				args := make([]string, 0, 2*len(vs))
				for _, v := range vs {
					args = append(args, "--target", v)
				}
				return args
			},
		},
		{
			Given: g.flagGiven("profile"),
			Menu:  func() tui.Menu { return profileMenu(cfg, g.Profile) },
			Apply: func(vs []string) []string {
				g.Profile = vs[0]
				if vs[0] == "default" {
					return nil
				}
				return []string{"--profile", vs[0]}
			},
		},
		{
			Given: g.flagGiven("verify"),
			// Built after the target stage has applied, so it can name the
			// chosen targets this machine could build but never run.
			Menu: func() tui.Menu { return verifyMenu(g, cfg) },
			Apply: func(vs []string) []string {
				if vs[0] == "none" {
					*o.verify = ""
					return nil
				}
				*o.verify = vs[0]
				return []string{"--verify", vs[0]}
			},
		},
		{
			Given: g.flagGiven("pack"),
			Menu:  func() tui.Menu { return packMenu(g) },
			Apply: func(vs []string) []string {
				*o.pack = vs[0] == "yes"
				if !*o.pack {
					return nil
				}
				return []string{"--pack"}
			},
		},
	}
	// With no [bundle.*] defined the question has exactly one answer, which is
	// not a question.
	if len(cfg.Bundles) > 0 {
		stages = append(stages, tui.Stage{
			Given: g.flagGiven("bundle"),
			Menu:  func() tui.Menu { return bundleMenu(cfg) },
			Apply: func(vs []string) []string {
				if vs[0] == "none" {
					return nil
				}
				*o.bundle = vs[0]
				return []string{"--bundle", vs[0]}
			},
		})
	}
	return stages, nil
}

// targetMenu offers every configured triple, the unbuildable ones visible but
// disabled: a target absent from the menu reads as unsupported, when all it is
// missing is a toolchain the shim fetches the moment --target names it.
func targetMenu(g *Global, cfg *config.Config, host string) (tui.Menu, error) {
	qemu := g.qemuMap(cfg)
	m := tui.Menu{
		Title: "Which targets should be built?",
		Help: "--target is repeatable and also accepts the sets \"all\" and \"proven\".\n" +
			"\"no qemu\" still builds; that target just cannot be verified or run here.",
		Headers: []string{"triple", "status", "toolchain", "run"},
		Flag:    "--target",
		Default: host,
	}
	selectable := 0
	var grp tui.Group
	for _, name := range sortedKeys(cfg.Targets) {
		t := cfg.Targets[name]
		st := g.toolchainState(t.Triple)
		tc := "-"
		switch {
		case st.Override != "":
			tc = "override"
		case st.Cross != "" && st.Native != "":
			tc = "cross+native"
		case st.Cross != "":
			tc = "cross"
		case st.Native != "":
			tc = "native"
		}
		run := ensure.RunnerNative
		if !ensure.IsNativeTarget(t) {
			run = "qemu"
			if _, ok := qemu[t.Triple]; !ok {
				run = "no qemu"
			}
		}
		ch := tui.Choice{
			Value: t.Triple,
			Cells: []string{t.Triple, t.Status, tc, run},
		}
		if t.Triple == host {
			ch.Note = "this machine"
		}
		if missing := g.toolchainMissing(host, t.Triple); missing != "" {
			ch.Disabled = true
			ch.Why = "no " + missing + " toolchain; `staticpy build --target " + t.Triple + "` makes the shim fetch it"
		} else {
			selectable++
		}
		grp.Choices = append(grp.Choices, ch)
	}
	if selectable == 0 {
		return tui.Menu{}, fmt.Errorf("no target has a toolchain on this machine; run through the ./staticpy shim, which fetches them, or pass --toolchains <dir> (`staticpy doctor` has the details)")
	}
	m.Groups = []tui.Group{grp}
	return m, nil
}

// What a profile is for lives in profiles.toml comments, which nobody can read
// mid-prompt. A profile only an overlay knows about gets no note, not a guess.
var profileNotes = map[string]string{
	"default":   "recommended",
	"debug":     "unstripped, with -g",
	"nolto":     "no LTO on the CPython tree, so it builds much faster",
	"nopgo":     "no profile-guided optimization",
	"bootstrap": "internal: the minimal host python that cross builds run",
	"reference": "dynamic baseline the benchmarks compare against",
}

func profileMenu(cfg *config.Config, def string) tui.Menu {
	var grp tui.Group
	for _, name := range sortedKeys(cfg.Profiles) {
		grp.Choices = append(grp.Choices, tui.Choice{
			Value: name, Cells: []string{name}, Note: profileNotes[name],
		})
	}
	return tui.Menu{
		Title:   "Which flag profile?",
		Help:    "`staticpy config show --profile NAME` prints one fully resolved.",
		Headers: []string{"profile"},
		Flag:    "--profile",
		Default: def,
		Groups:  []tui.Group{grp},
	}
}

func verifyMenu(g *Global, cfg *config.Config) tui.Menu {
	m := tui.Menu{
		Title: "Verify the interpreters as part of the build?",
		Help: "A failed verification stops the build, so a broken interpreter\n" +
			"never becomes a published artifact.",
		Headers: []string{"level", "what runs", "cost"},
		Flag:    "--verify",
		Default: "none",
		Groups: []tui.Group{{Choices: []tui.Choice{
			{Value: "none", Cells: []string{"none", "nothing", "-"}},
			{Value: string(ensure.LevelSmoke), Cells: []string{"smoke", "import probes", "seconds"}},
			{Value: string(ensure.LevelCore), Cells: []string{"core", "language core + every hand-linked extension", "minutes"}, Note: "recommended"},
			{Value: string(ensure.LevelFull), Cells: []string{"full", "CPython's whole test suite", "hours under qemu"}},
		}}},
	}
	if blocked := unrunnable(g, cfg); len(blocked) > 0 {
		m.Help += "\nVerification executes the target's binaries, and no qemu was found\n" +
			"for: " + strings.Join(blocked, " ") + ". Any level will fail there."
	}
	return m
}

func unrunnable(g *Global, cfg *config.Config) []string {
	qemu := g.qemuMap(cfg)
	var out []string
	for _, name := range g.Targets {
		t, ok := cfg.Targets[name]
		if !ok || ensure.IsNativeTarget(t) {
			continue
		}
		if _, ok := qemu[t.Triple]; !ok {
			out = append(out, t.Triple)
		}
	}
	return out
}

func packMenu(g *Global) tui.Menu {
	return tui.Menu{
		Title:   "Also pack a distributable tarball?",
		Help:    "The tarball is relocatable and lands in dist/out/" + g.Profile + "/<triple>/.",
		Headers: []string{"pack", "result"},
		Flag:    "--pack",
		Default: "no",
		Groups: []tui.Group{{Choices: []tui.Choice{
			{Value: "no", Cells: []string{"no", "artifact directory only"}},
			{Value: "yes", Cells: []string{"yes", "artifact directory + tarball"}},
		}}},
	}
}

func bundleMenu(cfg *config.Config) tui.Menu {
	grp := tui.Group{Choices: []tui.Choice{
		{Value: "none", Cells: []string{"none", "whatever the profile selects"}},
	}}
	for _, name := range sortedKeys(cfg.Bundles) {
		grp.Choices = append(grp.Choices, tui.Choice{
			Value: name, Cells: []string{name, strings.Join(cfg.Bundles[name].Packages, " ")},
		})
	}
	return tui.Menu{
		Title: "Compile a Python package bundle in?",
		Help: "A static interpreter cannot dlopen, so a compiled-in bundle is the\n" +
			"only way a third-party C module gets in.",
		Headers: []string{"bundle", "packages"},
		Flag:    "--bundle",
		Default: "none",
		Groups:  []tui.Group{grp},
	}
}
