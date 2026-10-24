package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Theme styles output for one Mode.
type Theme struct {
	mode Mode

	accent, muted, ok, fail, warn color.Color
}

// NewTheme builds the palette for m.
func NewTheme(m Mode) *Theme {
	pick := lipgloss.LightDark(m.Dark)

	return &Theme{
		mode:   m,
		accent: pick(lipgloss.Color("#5A56E0"), lipgloss.Color("#8B87FF")),
		muted:  pick(lipgloss.Color("#6C6F85"), lipgloss.Color("#7F849C")),
		ok:     pick(lipgloss.Color("#1F883D"), lipgloss.Color("#A6E3A1")),
		fail:   pick(lipgloss.Color("#CF222E"), lipgloss.Color("#F38BA8")),
		warn:   pick(lipgloss.Color("#9A6700"), lipgloss.Color("#F9E2AF")),
	}
}

// Mode returns the mode the theme was built for.
func (t *Theme) Mode() Mode { return t.mode }

func (t *Theme) fg(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

func (t *Theme) Brand(s string) string   { return t.fg(t.accent).Bold(true).Render(s) }
func (t *Theme) Dim(s string) string     { return t.fg(t.muted).Render(s) }
func (t *Theme) Bold(s string) string    { return lipgloss.NewStyle().Bold(true).Render(s) }
func (t *Theme) Accent(s string) string  { return t.fg(t.accent).Render(s) }
func (t *Theme) Good(s string) string    { return t.fg(t.ok).Render(s) }
func (t *Theme) Bad(s string) string     { return t.fg(t.fail).Render(s) }
func (t *Theme) Caution(s string) string { return t.fg(t.warn).Render(s) }

// Link wraps text in an OSC 8 hyperlink; plain output strips it with the rest.
func (t *Theme) Link(text, url string) string {
	if url == "" {
		return text
	}

	return ansi.SetHyperlink(url) + text + ansi.ResetHyperlink()
}

// Rule is a section heading: a label, a dimmed detail and a rule filling Width-1.
func (t *Theme) Rule(label, detail string) string {
	head := "── " + t.Bold(label)
	if detail != "" {
		head += "  " + t.Dim(detail)
	}

	fill := t.mode.Width - 1 - lipgloss.Width(head) - 1
	if fill < 2 {
		fill = 2
	}

	return t.Dim("── ") + strings.TrimPrefix(head, "── ") + " " + t.Dim(strings.Repeat("─", fill))
}

// Fit truncates a styled string to w cells.
func Fit(s string, w int) string {
	if w <= 0 {
		return ""
	}

	return ansi.Truncate(s, w, "…")
}

// Pad right-pads a styled string to w cells.
func Pad(s string, w int) string {
	if n := lipgloss.Width(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}

	return s
}

// Output strips every escape sequence in plain mode, links included.
func (t *Theme) Output(s string) string {
	if t.mode.Plain {
		return ansi.Strip(s)
	}

	return s
}
