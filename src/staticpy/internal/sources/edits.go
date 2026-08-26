package sources

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
)

// Edit actions.
const (
	ActionInsertAfter  = "insert_after"
	ActionInsertBefore = "insert_before"
	ActionReplaceLine  = "replace_line"
	ActionDeleteLine   = "delete_line"
)

// MatchCountError is what makes edits safe to keep in a build that runs for
// years. The Makefile this replaces used sed, which exits 0 when its anchor has
// moved: the edit silently does nothing and the build ships a subtly broken
// interpreter. Here a moved anchor stops the job.
type MatchCountError struct {
	Source string
	File   string
	Anchor string
	Want   int
	Got    int
	Why    string
}

func (e *MatchCountError) Error() string {
	s := fmt.Sprintf("sources: %s: edit anchor %q matched %d line(s) in %s, expected exactly %d",
		e.Source, e.Anchor, e.Got, e.File, e.Want)
	if e.Why != "" {
		s += fmt.Sprintf(" (edit exists to: %s)", e.Why)
	}
	return s + "; upstream moved and the edit must be re-pinned"
}

// ApplyEdits runs s.Edits against the tree in order.
func ApplyEdits(a Assets, s config.Source, tree string) error {
	for i, e := range s.Edits {
		if err := applyEdit(a, s, e, tree); err != nil {
			return fmt.Errorf("edit %d of %s: %w", i, Slug(s), err)
		}
	}
	return nil
}

func applyEdit(a Assets, s config.Source, e config.Edit, tree string) error {
	if err := validateEdit(s, e); err != nil {
		return err
	}
	target, err := resolve(tree, path.Clean(filepath.ToSlash(e.File)))
	if err != nil {
		return fmt.Errorf("sources: %s: edit file %q: %w", s.Name, e.File, err)
	}
	re, err := regexp.Compile(e.Anchor)
	if err != nil {
		return fmt.Errorf("sources: %s: edit anchor %q: %w", s.Name, e.Anchor, err)
	}
	text, err := editText(a, e)
	if err != nil {
		return fmt.Errorf("sources: %s: %w", s.Name, err)
	}

	fi, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("sources: %s: edit target %s: %w", s.Name, e.File, err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	lines, trailingNewline := splitLines(string(raw))

	want := e.MustMatch
	if want == 0 {
		want = 1
	}
	got := 0
	for _, l := range lines {
		if re.MatchString(l) {
			got++
		}
	}
	if got != want {
		return &MatchCountError{Source: s.Name, File: e.File, Anchor: e.Anchor, Want: want, Got: got, Why: e.Why}
	}

	ins, _ := splitLines(text)
	out := make([]string, 0, len(lines)+got*len(ins))
	for _, l := range lines {
		if !re.MatchString(l) {
			out = append(out, l)
			continue
		}
		switch e.Action {
		case ActionInsertAfter:
			out = append(out, l)
			out = append(out, ins...)
		case ActionInsertBefore:
			out = append(out, ins...)
			out = append(out, l)
		case ActionReplaceLine:
			out = append(out, ins...)
		case ActionDeleteLine:
		}
	}

	body := strings.Join(out, "\n")
	if trailingNewline && body != "" {
		body += "\n"
	}
	return os.WriteFile(target, []byte(body), fi.Mode().Perm())
}

func validateEdit(s config.Source, e config.Edit) error {
	switch e.Action {
	case ActionInsertAfter, ActionInsertBefore, ActionReplaceLine:
		if e.Text == "" && e.TextFile == "" {
			return fmt.Errorf("sources: %s: %s needs text or text_file", s.Name, e.Action)
		}
	case ActionDeleteLine:
		if e.Text != "" || e.TextFile != "" {
			return fmt.Errorf("sources: %s: delete_line takes no text", s.Name)
		}
	case "":
		return fmt.Errorf("sources: %s: edit on %s has no action", s.Name, e.File)
	default:
		return fmt.Errorf("sources: %s: unknown edit action %q", s.Name, e.Action)
	}
	if e.Text != "" && e.TextFile != "" {
		return fmt.Errorf("sources: %s: edit on %s sets both text and text_file", s.Name, e.File)
	}
	if e.File == "" {
		return fmt.Errorf("sources: %s: edit has no file", s.Name)
	}
	if e.Anchor == "" {
		return fmt.Errorf("sources: %s: edit on %s has no anchor", s.Name, e.File)
	}
	if e.MustMatch < 0 {
		return fmt.Errorf("sources: %s: edit on %s has negative must_match", s.Name, e.File)
	}
	return nil
}

func editText(a Assets, e config.Edit) (string, error) {
	if e.TextFile == "" {
		return e.Text, nil
	}
	if a == nil {
		return "", fmt.Errorf("edit on %s needs text_file %s but no asset tree was provided", e.File, e.TextFile)
	}
	name := path.Clean(filepath.ToSlash(e.TextFile))
	b, err := fs.ReadFile(a, name)
	if err != nil {
		return "", fmt.Errorf("reading edit text_file %s: %w", name, err)
	}
	return strings.TrimSuffix(string(b), "\n"), nil
}

// splitLines reports whether the content ended with a newline so the rewrite
// can put it back: a Makefile fragment that loses its final newline breaks make.
func splitLines(s string) ([]string, bool) {
	if s == "" {
		return nil, false
	}
	trailing := strings.HasSuffix(s, "\n")
	if trailing {
		s = strings.TrimSuffix(s, "\n")
	}
	return strings.Split(s, "\n"), trailing
}

// EditSetHash is the edit series' contribution to the job key. Edit.Why is
// excluded: it documents the edit, and rewording it should not rebuild.
func EditSetHash(a Assets, s config.Source) (string, error) {
	if len(s.Edits) == 0 {
		return "none", nil
	}
	h := sha256.New()
	for _, e := range s.Edits {
		text, err := editText(a, e)
		if err != nil {
			return "", fmt.Errorf("sources: %s: %w", s.Name, err)
		}
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d\x00%d\x00%s\x00",
			e.File, e.Anchor, e.Action, e.MustMatch, len(text), text)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
