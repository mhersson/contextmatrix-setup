package engine

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/ui"
)

func sampleRows() []serviceResult {
	return []serviceResult{
		{Repo: repos.Server, Result: "✓ restarted", Running: "aaaaaaa", Health: healthResult{Checked: true, OK: true}},
		{
			Repo: repos.Agent, Result: "● kept", Running: "bbbbbbb", Image: "agent-worker:bbbbbbb", Kept: true, Problem: true,
			Note: "keeps running bbbbbbb: its worker image did not build, so it was not restarted. The next update retries it.",
		},
	}
}

// headerRow returns the table line holding the column headers.
func headerRow(t *testing.T, out string) string {
	t.Helper()

	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "SERVICE") {
			return l
		}
	}

	t.Fatalf("no header row in:\n%s", out)

	return ""
}

func TestRenderResultColumns(t *testing.T) {
	theme := ui.NewTheme(ui.Mode{Plain: true, Width: 100})

	without := theme.Output(renderResult(theme, sampleRows(), false))
	assert.Equal(t, []string{"SERVICE", "RESULT", "HEALTH", "RUNNING"}, strings.Fields(strings.Trim(headerRow(t, without), "│ ")))

	with := theme.Output(renderResult(theme, sampleRows(), true))
	assert.Equal(t, []string{"SERVICE", "RESULT", "HEALTH", "RUNNING", "WORKER", "IMAGE"}, strings.Fields(strings.Trim(headerRow(t, with), "│ ")))

	lines := strings.Split(with, "\n")
	bottom, note := -1, -1

	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "╰") {
			bottom = i
		}

		if strings.Contains(l, "keeps running bbbbbbb") {
			note = i
		}
	}

	require.GreaterOrEqual(t, bottom, 0)
	assert.Greater(t, note, bottom, "the kept row's note sits under the table")
	assert.Contains(t, lines[note], repos.Agent)
}

func TestRenderResultFitsWidth(t *testing.T) {
	rows := sampleRows()
	rows = append(rows, serviceResult{
		Repo: repos.Chat, Result: "✗ restart failed", Running: "ccccccc", Problem: true,
		Note: "restart failed: " + strings.Repeat("x", 150) + "\n" + strings.Repeat("log line ", 30),
	})

	for _, w := range []int{60, 100} {
		theme := ui.NewTheme(ui.Mode{Plain: true, Width: w})

		for _, l := range strings.Split(theme.Output(renderResult(theme, rows, true)), "\n") {
			assert.LessOrEqual(t, lipgloss.Width(l), w-1, "width %d: %q", w, l)
		}
	}
}
