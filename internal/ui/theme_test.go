package ui

import (
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
)

func testTheme(dark, plain bool, width int) *Theme {
	return NewTheme(Mode{TTY: !plain, Plain: plain, Dark: dark, Width: width})
}

func TestPlainOutputHasNoEscapes(t *testing.T) {
	t.Parallel()

	th := testTheme(true, true, 80)
	out := th.Output(th.Bold("x") + th.Link("#5", "https://e") + th.Badge("feat") +
		th.Table([]string{"a", "b"}, [][]string{{"1", "2"}}))
	assert.NotContains(t, out, "\x1b")
	assert.Contains(t, out, "#5")
}

func TestLinkWithoutURLIsText(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "#5", testTheme(true, false, 80).Link("#5", ""))
}

func TestTableFitsWidth(t *testing.T) {
	t.Parallel()

	th := testTheme(true, false, 40)
	long := "a very long cell value that cannot possibly fit in the width"
	out := th.Table([]string{"name", "detail"}, [][]string{{long, long}, {"x", "y"}})
	assert.LessOrEqual(t, lipgloss.Width(out), 39)
}

func TestRuleSpansWidth(t *testing.T) {
	t.Parallel()

	for _, dark := range []bool{true, false} {
		th := testTheme(dark, false, 80)
		assert.Equal(t, 79, lipgloss.Width(th.Rule("repo", "a → b")))
	}
}

func TestFitTruncatesWithEllipsis(t *testing.T) {
	t.Parallel()

	got := Fit("hello world", 6)
	assert.Equal(t, 6, lipgloss.Width(got))
	assert.Contains(t, got, "…")
	assert.Equal(t, "hi", Fit("hi", 6))
	assert.Empty(t, Fit("hi", 0))
	assert.Equal(t, "hi   ", Pad("hi", 5))
}

func TestRuleTruncatesLongHeading(t *testing.T) {
	t.Parallel()

	th := testTheme(true, false, 40)
	out := th.Rule("a-very-long-repository-name-indeed", "some/long/path -> another/long/path")
	assert.Equal(t, 39, lipgloss.Width(out))
	assert.NotContains(t, out, "\n")
}

func TestNewThemeDefaultsWidth(t *testing.T) {
	t.Parallel()

	for _, w := range []int{0, -3} {
		th := NewTheme(Mode{Plain: true, Width: w})
		assert.Equal(t, 100, th.Mode().Width, w)
		assert.NotEmpty(t, th.Output(th.StepLine(Done, "build", "contextmatrix", "ok", time.Second)), w)
	}
}
