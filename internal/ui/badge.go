package ui

import (
	"regexp"

	"charm.land/lipgloss/v2"
)

var conventional = regexp.MustCompile(`^(\w+)(?:\(([^)]+)\))?(!)?:\s*(.+)$`)

type Subject struct {
	Type, Scope, Summary string
	Breaking             bool
}

// ParseSubject splits a conventional-commit subject; anything else keeps its
// text as the summary with no type.
func ParseSubject(s string) Subject {
	m := conventional.FindStringSubmatch(s)
	if m == nil {
		return Subject{Summary: s}
	}

	return Subject{Type: m[1], Scope: m[2], Breaking: m[3] == "!", Summary: m[4]}
}

type chip struct{ lightBg, lightFg, darkBg, darkFg string }

var chips = map[string]chip{
	"feat":     {"#D3F2D0", "#1A5E20", "#A6E3A1", "#11111B"},
	"fix":      {"#F9D3DB", "#8B1A33", "#F38BA8", "#11111B"},
	"docs":     {"#D6E4FD", "#1D3F86", "#89B4FA", "#11111B"},
	"chore":    {"#E3E4EA", "#44475A", "#9399B2", "#11111B"},
	"build":    {"#FBEFCB", "#6B4E00", "#F9E2AF", "#11111B"},
	"ci":       {"#FCE1CF", "#7A3A0E", "#FAB387", "#11111B"},
	"test":     {"#EADDFB", "#5B2A86", "#CBA6F7", "#11111B"},
	"refactor": {"#D2F2EE", "#145E56", "#94E2D5", "#11111B"},
	"perf":     {"#D2F2EE", "#145E56", "#94E2D5", "#11111B"},
	"revert":   {"#F9D3DB", "#8B1A33", "#EBA0AC", "#11111B"},
	"deps":     {"#ECECF0", "#8C8FA1", "#45475A", "#A6ADC8"},
}

const BadgeWidth = 10

// Badge is a fixed-width coloured chip for a commit type.
func (t *Theme) Badge(kind string) string {
	if kind == "" {
		return Pad("", BadgeWidth)
	}

	c, ok := chips[kind]
	if !ok {
		c = chips["chore"]
	}

	pick := lipgloss.LightDark(t.mode.Dark)
	st := lipgloss.NewStyle().
		Background(pick(lipgloss.Color(c.lightBg), lipgloss.Color(c.darkBg))).
		Foreground(pick(lipgloss.Color(c.lightFg), lipgloss.Color(c.darkFg))).
		Bold(kind != "deps").
		Width(BadgeWidth - 1).
		Align(lipgloss.Center)

	return Pad(st.Render(Fit(kind, BadgeWidth-1)), BadgeWidth)
}
