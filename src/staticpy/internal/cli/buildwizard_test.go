package cli

import (
	"strings"
	"testing"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

const wizHost = "x86_64-linux-musl"

func wizardGlobal(t *testing.T) (*Global, *config.Config) {
	t.Helper()
	cfg, err := config.Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// One provisioned toolchain, so exactly one target is buildable and the
	// rest exercise the disabled-with-a-reason path.
	g := &Global{
		Profile:   "default",
		Overrides: map[string]string{wizHost: t.TempDir()},
		Qemu:      map[string]string{},
	}
	return g, cfg
}

func TestBuildStagesApplyAndRenderFlags(t *testing.T) {
	g, cfg := wizardGlobal(t)
	verify, pack, bundle := "", false, ""
	stages, err := buildStages(g, cfg, wizHost, buildOpts{&verify, &pack, &bundle})
	if err != nil {
		t.Fatal(err)
	}
	// No [bundle.*] is defined in the embedded config, so no bundle stage.
	if len(stages) != 4 {
		t.Fatalf("got %d stages, want 4 (target, profile, verify, pack)", len(stages))
	}

	answers := map[string][]string{
		"--target":  {wizHost},
		"--profile": {"default"},
		"--verify":  {"core"},
		"--pack":    {"yes"},
	}
	var args []string
	for _, s := range stages {
		m := s.Menu()
		args = append(args, s.Apply(answers[m.Flag])...)
	}
	if got, want := strings.Join(args, " "), "--target "+wizHost+" --verify core --pack"; got != want {
		t.Fatalf("rendered %q, want %q", got, want)
	}
	if len(g.Targets) != 1 || g.Targets[0] != wizHost {
		t.Fatalf("g.Targets = %v", g.Targets)
	}
	if verify != "core" || !pack || bundle != "" {
		t.Fatalf("verify=%q pack=%v bundle=%q", verify, pack, bundle)
	}
}

func TestBuildStagesAnswerOfDefaultsRendersNoFlag(t *testing.T) {
	g, cfg := wizardGlobal(t)
	verify, pack, bundle := "", false, ""
	stages, err := buildStages(g, cfg, wizHost, buildOpts{&verify, &pack, &bundle})
	if err != nil {
		t.Fatal(err)
	}
	answers := map[string][]string{
		"--target":  {wizHost},
		"--profile": {"default"},
		"--verify":  {"none"},
		"--pack":    {"no"},
	}
	var args []string
	for _, s := range stages {
		args = append(args, s.Apply(answers[s.Menu().Flag])...)
	}
	// --target always renders: it is what skips the wizard next time. The
	// stages answered with their flag's own default add nothing.
	if got, want := strings.Join(args, " "), "--target "+wizHost; got != want {
		t.Fatalf("rendered %q, want %q", got, want)
	}
	if verify != "" || pack {
		t.Fatalf("verify=%q pack=%v", verify, pack)
	}
}

func TestBuildStagesSkipFlagsAlreadyGiven(t *testing.T) {
	g, cfg := wizardGlobal(t)
	g.givenFlags = map[string]bool{"profile": true, "pack": true}
	verify, pack, bundle := "", false, ""
	stages, err := buildStages(g, cfg, wizHost, buildOpts{&verify, &pack, &bundle})
	if err != nil {
		t.Fatal(err)
	}
	var open []string
	for _, s := range stages {
		if !s.Given {
			open = append(open, s.Menu().Flag)
		}
	}
	if got, want := strings.Join(open, " "), "--target --verify"; got != want {
		t.Fatalf("open stages %q, want %q", got, want)
	}
}

func TestTargetMenuDisablesTargetsWithoutAToolchain(t *testing.T) {
	g, cfg := wizardGlobal(t)
	m, err := targetMenu(g, cfg, wizHost)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, c := range m.Groups[0].Choices {
		seen++
		switch {
		case c.Value == wizHost:
			if c.Disabled {
				t.Fatalf("%s has a toolchain override and must be selectable", wizHost)
			}
		case !c.Disabled:
			t.Fatalf("%s has no toolchain and must be disabled, not offered", c.Value)
		case !strings.Contains(c.Why, "--target "+c.Value):
			t.Fatalf("%s's reason must teach the flag that fixes it, got %q", c.Value, c.Why)
		}
	}
	if seen != len(cfg.Targets) {
		t.Fatalf("menu shows %d targets, config has %d; unbuildable ones must not vanish", seen, len(cfg.Targets))
	}

	// Nothing buildable is an error up front, not an empty menu mid-wizard.
	if _, err := targetMenu(&Global{}, cfg, wizHost); err == nil {
		t.Fatal("no toolchains anywhere must refuse with advice")
	}
}
