package tui

import (
	"fmt"
	"os"
	"strings"
)

// Stage is one decision in a multi-step wizard. A command assembles a slice of
// these - one per flag the wizard can stand in for - so adding a question is
// adding a stage, not editing a prompt sequence.
type Stage struct {
	// Given marks the choice as already made on the command line; the stage
	// is skipped without a word.
	Given bool
	// Multi asks with MultiSelect; the answer is every checked Value.
	Multi bool
	// Menu is called only when the stage actually runs, after every earlier
	// stage has applied, so a menu may react to earlier answers.
	Menu func() Menu
	// Apply records the answer wherever the flag would have put it, and
	// returns the command-line words that pin it - nil when the answer is
	// the flag's own default and no words are needed.
	Apply func(values []string) []string
}

// Wizard asks, in order, each stage the command line left open, then prints
// the flag-for-flag equivalent invocation: the point of the wizard is to teach
// the command that skips it. Aborting any stage abandons the whole run with
// ErrAborted; answers already applied are the caller's to discard.
func Wizard(command string, stages []Stage) error {
	if !Interactive() {
		return ErrNotInteractive
	}
	args, err := runStages(stages, ask)
	if err != nil {
		return err
	}
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "  to skip the menus next time: %s %s\n",
			command, strings.Join(args, " "))
	}
	return nil
}

func runStages(stages []Stage, ask func(Stage) ([]string, error)) ([]string, error) {
	var args []string
	for _, s := range stages {
		if s.Given {
			continue
		}
		vals, err := ask(s)
		if err != nil {
			return nil, err
		}
		args = append(args, s.Apply(vals)...)
	}
	return args, nil
}

func ask(s Stage) ([]string, error) {
	m := s.Menu()
	if s.Multi {
		cs, err := MultiSelect(m)
		if err != nil {
			return nil, err
		}
		vals := make([]string, 0, len(cs))
		for _, c := range cs {
			vals = append(vals, c.Value)
		}
		return vals, nil
	}
	c, err := Select(m)
	if err != nil {
		return nil, err
	}
	return []string{c.Value}, nil
}
