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

	lines := strings.Split(describe(m, headerIndent(th)), "\n")
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
