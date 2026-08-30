package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

func alignMenu() Menu {
	return Menu{
		Title:   "pick",
		Headers: []string{"cpu", "core", "max clock"},
		Flag:    "--cpu",
		Groups: []Group{{Choices: []Choice{
			{Value: "0", Cells: []string{"cpu0", "core0", "5.16GHz"}, Note: "recommended"},
			{Value: "10", Cells: []string{"cpu10", "core15", "3.29GHz"}},
		}}},
	}
}

// The header is drawn as the last description line; the options are drawn by
// huh with a selector in front of each. Both come from row() with the same
// widths, so alignment is exactly whether the two prefixes match -- which a
// hardcoded indent did not, being guessed against a different renderer.
func TestColumnHeaderAlignsWithOptionRows(t *testing.T) {
	th := huh.ThemeCharm()
	m := alignMenu()

	lines := strings.Split(describe(m, headerIndent(th, false)), "\n")
	header := lines[len(lines)-1]

	// renderOption pads an unselected row by the selector's printable width.
	optPrefix := strings.Repeat(" ", lipgloss.Width(th.Focused.SelectSelector.String()))

	gotIndent := len(header) - len(strings.TrimLeft(header, " "))
	if gotIndent != len(optPrefix) {
		t.Fatalf("header indented %d, options indented %d", gotIndent, len(optPrefix))
	}
	for _, c := range m.Groups[0].Choices {
		opt := optPrefix + label(m, m.Groups[0], c)
		for i, h := range m.Headers {
			if strings.Index(header, h) != strings.Index(opt, c.Cells[i]) {
				t.Fatalf("column %q misaligned:\n%s\n%s", h, header, opt)
			}
		}
	}
}

// The single-select indent left every column label two cells short of its
// data.
func TestMultiSelectHeaderIndentCoversTheCheckMark(t *testing.T) {
	th := huh.ThemeCharm()
	want := lipgloss.Width(th.Focused.MultiSelectSelector.String()) +
		lipgloss.Width(th.Focused.UnselectedPrefix.String())
	if got := len(headerIndent(th, true)); got != want {
		t.Fatalf("multi header indented %d, renderOption prefixes options by %d", got, want)
	}
}

func TestMultiSelectFallsBackWithoutATerminal(t *testing.T) {
	t.Setenv("STATICPY_NO_TUI", "1")
	if _, err := MultiSelect(alignMenu()); err != ErrNotInteractive {
		t.Fatalf("err = %v, want ErrNotInteractive", err)
	}
	all := Menu{Title: "t", Groups: []Group{{Choices: []Choice{{Value: "a", Disabled: true, Why: "x"}}}}}
	if _, err := MultiSelect(all); err == nil || err == ErrNotInteractive {
		t.Fatalf("an all-disabled menu must fail loudly, got %v", err)
	}
}

func wizardStage(name string, given bool, log *[]string) Stage {
	return Stage{
		Given: given,
		Menu:  func() Menu { return Menu{Flag: "--" + name} },
		Apply: func(vals []string) []string {
			*log = append(*log, name+"="+strings.Join(vals, ","))
			if vals[0] == "default" {
				return nil
			}
			args := make([]string, 0, 2*len(vals))
			for _, v := range vals {
				args = append(args, "--"+name, v)
			}
			return args
		},
	}
}

func TestRunStagesSkipsGivenAndRendersOnlyNonDefaults(t *testing.T) {
	var applied []string
	stages := []Stage{
		wizardStage("target", false, &applied),
		wizardStage("profile", true, &applied),
		wizardStage("verify", false, &applied),
		wizardStage("pack", false, &applied),
	}
	answers := map[string][]string{
		"--target": {"a", "b"},
		"--verify": {"core"},
		"--pack":   {"default"},
	}
	args, err := runStages(stages, func(s Stage) ([]string, error) {
		return answers[s.Menu().Flag], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(args, " "), "--target a --target b --verify core"; got != want {
		t.Fatalf("rendered %q, want %q", got, want)
	}
	// The given stage is skipped silently: not asked, not applied.
	if got, want := strings.Join(applied, " "), "target=a,b verify=core pack=default"; got != want {
		t.Fatalf("applied %q, want %q", got, want)
	}
}

func TestRunStagesStopsAtTheFirstAbort(t *testing.T) {
	var applied []string
	stages := []Stage{
		wizardStage("target", false, &applied),
		wizardStage("verify", false, &applied),
	}
	asked := 0
	_, err := runStages(stages, func(Stage) ([]string, error) {
		asked++
		if asked == 2 {
			return nil, ErrAborted
		}
		return []string{"x"}, nil
	})
	if err != ErrAborted {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
	if got, want := strings.Join(applied, " "), "target=x"; got != want {
		t.Fatalf("applied %q after abort, want %q", got, want)
	}
}

func TestWizardWithoutATerminalPromptsNothing(t *testing.T) {
	t.Setenv("STATICPY_NO_TUI", "1")
	err := Wizard("staticpy build", []Stage{{
		Menu:  func() Menu { t.Fatal("menu built without a terminal"); return Menu{} },
		Apply: func([]string) []string { t.Fatal("applied without a terminal"); return nil },
	}})
	if err != ErrNotInteractive {
		t.Fatalf("err = %v, want ErrNotInteractive", err)
	}
}

func TestSplitDefaults(t *testing.T) {
	if got := strings.Join(splitDefaults(" a, b ,,c"), "|"); got != "a|b|c" {
		t.Fatalf("splitDefaults = %q", got)
	}
	if got := splitDefaults(""); got != nil {
		t.Fatalf("splitDefaults(\"\") = %v, want nil", got)
	}
}
