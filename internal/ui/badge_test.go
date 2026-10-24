package ui

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
)

func TestParseSubject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want Subject
	}{
		{"feat(web): add x", Subject{"feat", "web", "add x", false}},
		{"fix!: drop y", Subject{"fix", "", "drop y", true}},
		{"Merge branch 'x'", Subject{"", "", "Merge branch 'x'", false}},
		{"chore(deps): bump a", Subject{"chore", "deps", "bump a", false}},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ParseSubject(tt.in))
		})
	}
}

func TestBadgeHasFixedWidth(t *testing.T) {
	t.Parallel()

	kinds := []string{
		"feat", "fix", "docs", "chore", "build", "ci", "test",
		"refactor", "perf", "revert", "deps", "unknown", "documentation", "",
	}

	for _, dark := range []bool{true, false} {
		th := testTheme(dark, false, 80)
		for _, k := range kinds {
			assert.Equal(t, BadgeWidth, lipgloss.Width(th.Badge(k)), "%q dark=%v", k, dark)
			assert.NotContains(t, th.Badge(k), "\n", "%q dark=%v", k, dark)
		}
	}
}
