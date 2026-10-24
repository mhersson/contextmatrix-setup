package engine

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/mhersson/contextmatrix-setup/internal/ui"
)

// serviceResult is one row of the result table. Result leads with its
// status glyph, which picks its colour. Note prints under the table: its
// first line is the message, any further lines are log output.
type serviceResult struct {
	Repo, Result, Running, Image string
	Health                       healthResult
	Problem, Kept                bool
	Note                         string
}

// ProblemsError reports a finished update that left something broken or
// behind; the state is already saved.
type ProblemsError struct{ N int }

func (e *ProblemsError) Error() string {
	return "update finished with " + plural(e.N, "problem")
}

func renderResult(t *ui.Theme, rows []serviceResult, docker bool) string {
	headers := []string{"SERVICE", "RESULT", "HEALTH", "RUNNING"}
	if docker {
		headers = append(headers, "WORKER IMAGE")
	}

	cells := make([][]string, 0, len(rows))

	for _, r := range rows {
		row := []string{t.Bold(r.Repo), glyphStyled(t, r.Result), healthCell(t, r.Health), versionCell(t, r.Running, r.Kept)}
		if docker {
			row = append(row, versionCell(t, r.Image, r.Kept))
		}

		cells = append(cells, row)
	}

	var b strings.Builder

	b.WriteString("\n " + t.Bold("Result") + "\n")
	b.WriteString(ui.Indent(t.Inset().Table(headers, cells)))

	for _, r := range rows {
		if n := noteLines(t, r); n != "" {
			b.WriteString("\n" + n)
		}
	}

	return b.String()
}

func glyphStyled(t *ui.Theme, s string) string {
	switch {
	case strings.HasPrefix(s, "✓"):
		return t.Good(s)
	case strings.HasPrefix(s, "✗"):
		return t.Bad(s)
	case strings.HasPrefix(s, "●"):
		return t.Caution(s)
	default:
		return s
	}
}

func healthCell(t *ui.Theme, h healthResult) string {
	switch {
	case h.OK:
		return t.Good("✓ healthy")
	case h.Checked:
		return t.Bad("✗ " + ui.OneLine(h.Reason))
	case h.Reason != "":
		return t.Dim(ui.OneLine(h.Reason))
	default:
		return t.Dim("–")
	}
}

func versionCell(t *ui.Theme, v string, kept bool) string {
	switch {
	case v == "":
		return t.Dim("–")
	case kept:
		return v + t.Dim(" (old)")
	default:
		return v
	}
}

// noteLines wraps the message under its glyph and repo, and fits each log
// line to the width so no line wraps on the terminal.
func noteLines(t *ui.Theme, r serviceResult) string {
	var lines []string

	for _, l := range strings.Split(r.Note, "\n") {
		if l = ui.OneLine(l); l != "" {
			lines = append(lines, l)
		}
	}

	if len(lines) == 0 {
		return ""
	}

	glyph := t.Caution("●")
	if r.Problem && !r.Kept {
		glyph = t.Bad("✗")
	}

	w := t.Mode().Width - 1
	head := ansi.Wrap(r.Repo+" "+lines[0], w-3, "")
	out := " " + glyph + " " + strings.ReplaceAll(head, "\n", "\n   ")

	for _, l := range lines[1:] {
		out += "\n     " + t.Dim(ui.Fit(l, w-5))
	}

	return out
}
