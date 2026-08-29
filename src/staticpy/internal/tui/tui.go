// Package tui presents a choice staticpy needs but was not given on the
// command line.
//
// Every prompt here is a fallback for a flag, never the only way to say
// something: a menu that cannot be answered non-interactively would break
// scripts and CI. Menu.Flag names the flag that skips the prompt, and it is
// printed with the answer so using the menu teaches the flag.
//
// Rendering is charmbracelet/huh. The policy above is ours; the terminal
// handling is not something worth owning.
package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
)

var (
	// ErrNotInteractive means there is no terminal to prompt on. Callers fall
	// back to their default rather than failing.
	ErrNotInteractive = errors.New("tui: not interactive")
	// ErrAborted means the user quit the menu.
	ErrAborted = errors.New("tui: aborted")
)

// Choice is one selectable row.
type Choice struct {
	// Value is what the equivalent flag would take, e.g. "3" for --cpu 3.
	Value string
	// Cells are the columns, aligned against Menu.Headers.
	Cells []string
	// Note is a short right-hand annotation, e.g. "recommended".
	Note string
	// Disabled rows are shown in the header table but cannot be picked:
	// seeing why an option is unavailable beats not seeing it at all.
	Disabled bool
	Why      string
}

// Group is a labelled block of choices.
type Group struct {
	Title   string
	Choices []Choice
}

type Menu struct {
	Title   string
	Help    string
	Headers []string
	Groups  []Group
	// Flag is the command-line flag this menu stands in for, e.g. "--cpu".
	Flag string
	// Default is the Value pre-selected when the menu opens, and the one a
	// non-interactive caller should use.
	Default string
}

// Interactive reports whether a menu can be shown. Menus render on stderr so
// a command's real output can still be piped, but input has to come from a
// real terminal either way.
func Interactive() bool {
	if os.Getenv("STATICPY_NO_TUI") != "" || os.Getenv("TERM") == "dumb" || os.Getenv("CI") != "" {
		return false
	}
	return isTTY(os.Stdin) && isTTY(os.Stderr)
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Select shows the menu and returns the chosen row.
func Select(m Menu) (Choice, error) {
	byValue := map[string]Choice{}
	var opts []huh.Option[string]
	for _, g := range m.Groups {
		for _, c := range g.Choices {
			byValue[c.Value] = c
			if c.Disabled {
				continue
			}
			opts = append(opts, huh.NewOption(label(m, g, c), c.Value))
		}
	}
	if len(opts) == 0 {
		return Choice{}, fmt.Errorf("tui: menu %q has no selectable choices", m.Title)
	}
	if !Interactive() {
		return Choice{}, ErrNotInteractive
	}

	chosen := m.Default
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(m.Title).
			Description(describe(m)).
			Options(opts...).
			Value(&chosen),
	)).WithOutput(os.Stderr).WithShowHelp(true)

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return Choice{}, ErrAborted
		}
		return Choice{}, err
	}
	return byValue[chosen], nil
}

// SelectOr runs the menu, falling back to the default when there is no
// terminal. The fallback names the flag that would have made the choice
// explicit, so a non-interactive run still says what it picked and how to pin
// it.
func SelectOr(m Menu) (string, error) {
	c, err := Select(m)
	switch {
	case err == nil:
		fmt.Fprintf(os.Stderr, "  using %s %s\n", m.Flag, c.Value)
		return c.Value, nil
	case errors.Is(err, ErrNotInteractive):
		fmt.Fprintf(os.Stderr, "  defaulting to %s %s (pass %s to choose)\n",
			m.Flag, m.Default, m.Flag)
		return m.Default, nil
	}
	return "", err
}

// label is one option's row, column-aligned across the whole menu so the
// options line up however huh lays them out.
func label(m Menu, g Group, c Choice) string {
	s := row(c.Cells, widths(m))
	if g.Title != "" {
		s = fmt.Sprintf("%-14s %s", g.Title, s)
	}
	if c.Note != "" {
		s += "  (" + c.Note + ")"
	}
	return s
}

// describe renders the rows that cannot be chosen, so the shape of the machine
// and the reason part of it is unavailable stay visible while choosing.
func describe(m Menu) string {
	var b strings.Builder
	if m.Help != "" {
		b.WriteString(m.Help + "\n")
	}
	w := widths(m)
	var unavailable []string
	for _, g := range m.Groups {
		for _, c := range g.Choices {
			if c.Disabled {
				unavailable = append(unavailable, "  "+row(c.Cells, w)+"  -- "+c.Why)
			}
		}
	}
	if len(unavailable) > 0 {
		b.WriteString("not selectable:\n")
		b.WriteString(strings.Join(unavailable, "\n") + "\n")
	}
	b.WriteString("equivalent flag: " + m.Flag)
	// Headers sized the columns and were then never shown, which left every
	// menu presenting aligned data with nothing naming it. Last in the
	// description puts it directly above the first option, indented to clear
	// the cursor huh draws in front of the selected row.
	if len(m.Headers) > 0 {
		b.WriteString("\n    " + row(m.Headers, w))
	}
	return b.String()
}

func widths(m Menu) []int {
	w := make([]int, len(m.Headers))
	for i, h := range m.Headers {
		w[i] = len(h)
	}
	for _, g := range m.Groups {
		for _, c := range g.Choices {
			for i, cell := range c.Cells {
				if i < len(w) && len(cell) > w[i] {
					w[i] = len(cell)
				}
			}
		}
	}
	return w
}

func row(cells []string, w []int) string {
	parts := make([]string, 0, len(cells))
	for i, c := range cells {
		if i < len(w) {
			parts = append(parts, fmt.Sprintf("%-*s", w[i], c))
		} else {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, "  ")
}
