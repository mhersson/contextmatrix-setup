package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"charm.land/lipgloss/v2"

	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/ui"
)

func plainTheme(width int) *ui.Theme { return ui.NewTheme(ui.Mode{Plain: true, Width: width}) }

func pr(n int, title string, deps bool) repos.Change {
	return repos.Change{SHA: strings.Repeat("a", 40), PR: n, Title: title, Dependabot: deps, Commits: 1}
}

func one(changes ...repos.Change) []repoChanges {
	return []repoChanges{{Repo: "contextmatrix", From: "1111111aaaa", To: "2222222bbbb", Changes: changes}}
}

func lineWith(out, needle string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}

	return ""
}

func TestChangesViewRowOrderAndDependabotLast(t *testing.T) {
	out := renderChanges(plainTheme(100), one(
		pr(3, "build(deps): bump a", true),
		pr(2, "feat(x): add b", false),
		pr(1, "build(deps): bump c", true),
		pr(4, "fix(y): fix d", false),
	))

	for _, n := range []string{"#1", "#2", "#3", "#4"} {
		assert.Equal(t, 1, strings.Count(out, n), n)
	}

	i2, i4, i3, i1 := strings.Index(out, "#2"), strings.Index(out, "#4"), strings.Index(out, "#3"), strings.Index(out, "#1")
	assert.Less(t, i2, i4)
	assert.Less(t, i4, i3)
	assert.Less(t, i4, i1)
}

func TestChangesViewCollapsesMoreThanThreeDependabot(t *testing.T) {
	four := renderChanges(plainTheme(100), one(
		pr(1, "build(deps): a", true), pr(2, "build(deps): b", true),
		pr(3, "build(deps): c", true), pr(4, "build(deps): d", true),
	))

	line := lineWith(four, "#1")
	require.NotEmpty(t, line)

	for _, n := range []string{"#1", "#2", "#3", "#4"} {
		assert.Contains(t, line, n)
		assert.Equal(t, 1, strings.Count(four, n))
	}

	three := renderChanges(plainTheme(100), one(
		pr(1, "build(deps): a", true), pr(2, "build(deps): b", true), pr(3, "build(deps): c", true),
	))

	rows := 0

	for _, l := range strings.Split(three, "\n") {
		if strings.Contains(l, "#") {
			rows++

			assert.Equal(t, 1, strings.Count(l, "#"), "one ref per row")
		}
	}

	assert.Equal(t, 3, rows)
}

func TestChangesViewCountsAndImages(t *testing.T) {
	list := []repoChanges{
		{Repo: "contextmatrix-agent", From: "1111111", To: "2222222", Images: 4, Changes: []repos.Change{
			pr(1, "feat: a", false), pr(2, "fix: b", false), pr(3, "build(deps): c", true),
		}},
		{Repo: "contextmatrix", From: "1111111", To: "2222222", Changes: []repos.Change{pr(4, "feat: d", false)}},
	}
	out := renderChanges(plainTheme(100), list)

	agent := lineWith(out, "contextmatrix-agent")
	assert.Contains(t, agent, "×4")
	assert.Contains(t, agent, "2 PRs")
	assert.Contains(t, agent, "1 dep")
	assert.NotContains(t, lineWith(out, "│ contextmatrix "), "×")
}

func TestChangesViewMultiCommitPR(t *testing.T) {
	multi := pr(1, "feat: a", false)
	multi.Commits = 3
	out := renderChanges(plainTheme(100), one(multi))
	assert.Contains(t, lineWith(out, "#1"), "3 commits")

	out = renderChanges(plainTheme(100), one(pr(1, "feat: a", false)))
	assert.NotContains(t, lineWith(out, "#1"), "commits")
}

func TestChangesViewLinksOnlyWithWebBase(t *testing.T) {
	th := ui.NewTheme(ui.Mode{Dark: true, Width: 100})
	list := one(pr(7, "feat: a", false))
	list[0].Web = "https://github.com/o/r"
	assert.Contains(t, renderChanges(th, list), "\x1b]8;;https://github.com/o/r/pull/7")

	list[0].Web = ""
	assert.NotContains(t, renderChanges(th, list), "]8;;")
}

func TestChangesViewShowsUnavailableList(t *testing.T) {
	list := one()
	list[0].Err = errors.New("bad revision")
	out := renderChanges(plainTheme(100), list)

	assert.Contains(t, lineWith(out, "contextmatrix"), "restart")
	assert.Contains(t, out, "change list unavailable")
}

func TestChangesViewFitsWidth(t *testing.T) {
	list := one(
		pr(1, "feat(scope): "+strings.Repeat("long ", 40), false),
		pr(2, "build(deps): "+strings.Repeat("x", 200), true),
	)
	list[0].Err = nil
	out := renderChanges(plainTheme(60), list)

	for _, l := range strings.Split(out, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(l), 59, l)
	}

	list[0].Err = errors.New(strings.Repeat("e", 200))

	for _, l := range strings.Split(renderChanges(plainTheme(60), list), "\n") {
		assert.LessOrEqual(t, lipgloss.Width(l), 59, l)
	}

	list[0].Err = errors.New("fatal: ambiguous argument 'x..y'\nUse '--' to separate paths")
	multi := renderChanges(plainTheme(100), list)
	assert.Contains(t, lineWith(multi, "change list unavailable"), "Use '--' to separate paths")

	for _, l := range strings.Split(renderChanges(plainTheme(60), list), "\n") {
		assert.LessOrEqual(t, lipgloss.Width(l), 59, l)
	}
}

func TestChangesViewPlainHasNoEscapes(t *testing.T) {
	list := one(pr(7, "feat!: a", false), pr(8, "build(deps): b", true))
	list[0].Web = "https://github.com/o/r"
	out := renderChanges(plainTheme(100), list)
	assert.NotContains(t, out, "\x1b")
	assert.Equal(t, ansi.Strip(out), out)
}

func TestChangesViewCountsClassifyDependabotFirst(t *testing.T) {
	direct := repos.Change{SHA: strings.Repeat("b", 40), Title: "build(deps): bump z", Dependabot: true, Commits: 1}
	out := renderChanges(plainTheme(100), one(pr(1, "feat: a", false), direct))
	row := lineWith(out, "│ contextmatrix")

	assert.Contains(t, row, "1 PR")
	assert.Contains(t, row, "1 dep")
	assert.NotContains(t, row, "commit")
}
