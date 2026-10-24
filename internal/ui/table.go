package ui

import (
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
)

// Table is the boxed summary table: rounded border, bold header, no column
// rules.
func (t *Theme) Table(headers []string, rows [][]string) string {
	tb := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(t.muted)).
		BorderColumn(false).
		BorderHeader(true).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			st := lipgloss.NewStyle().Padding(0, 1)
			if row == table.HeaderRow {
				return st.Foreground(t.accent).Bold(true)
			}

			return st
		})

	if out := tb.Render(); lipgloss.Width(out) < t.mode.Width {
		return out
	}

	return tb.Width(t.mode.Width - 1).Wrap(false).Render()
}
