package engine

import (
	"context"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/repos"
)

// The name column fits the longest app name, so the second column of the
// header and of every app row starts at the same offset.
func TestPrintStatusAlignsTheNameColumn(t *testing.T) {
	h := installed(t, true)

	s, err := h.e.Status(context.Background())
	require.NoError(t, err)

	h.e.PrintStatus(s)

	lines := strings.Split(h.out.String(), "\n")
	require.Greater(t, len(lines), len(repos.Apps))

	want := secondColumn(lines[0])

	for i, repo := range repos.Apps {
		row := lines[i+1]
		require.True(t, strings.HasPrefix(row, repo+" "), "row %d is %s: %q", i+1, repo, row)
		assert.Equal(t, want, secondColumn(row), "%s: %q", repo, row)
	}
}

// secondColumn is the offset at which a line's second field starts.
func secondColumn(line string) int {
	gap := strings.IndexFunc(line, unicode.IsSpace)
	if gap < 0 {
		return -1
	}

	return gap + strings.IndexFunc(line[gap:], func(r rune) bool { return !unicode.IsSpace(r) })
}
