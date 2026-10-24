package engine

import (
	"bytes"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/ui"
)

func TestPrintNotesWrapsAtWidth(t *testing.T) {
	const text = "image build failed, keeping contextmatrix-agent-worker:cfa04af: build contextmatrix-agent image: make docker-worker exited 2"

	var screen bytes.Buffer

	c := ui.NewConsole(&screen, ui.NewTheme(ui.Mode{Plain: true, Width: 60}), nil, "")
	printNotes(c, note{"contextmatrix-agent", text})

	out := strings.TrimRight(screen.String(), "\n")
	lines := strings.Split(out, "\n")
	require.Greater(t, len(lines), 1)

	for i, l := range lines {
		assert.LessOrEqual(t, lipgloss.Width(l), 59, l)

		if i > 0 {
			assert.True(t, strings.HasPrefix(l, "     "), "continuation %q", l)
		}
	}

	assert.NotContains(t, out, "\x1b")

	have := map[string]bool{}
	for _, w := range strings.Fields(out) {
		have[w] = true
	}

	for _, w := range strings.Fields(text) {
		assert.True(t, have[w], "word %q split", w)
	}
}
