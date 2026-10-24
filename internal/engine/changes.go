package engine

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/ui"
)

// repoChanges is what one moved repo contributes to the update preview.
type repoChanges struct {
	Repo, From, To, Web string
	Changes             []repos.Change
	Err                 error
	Images              int
	Skills              bool
}

const (
	refWidth       = 8
	scopeWidth     = 14
	maxDepRowsEach = 3
)

// renderChanges renders the summary table and the per-repo change rows; the
// caller adds the prompt.
func renderChanges(t *ui.Theme, list []repoChanges) string {
	var b strings.Builder

	noun := "repos have"
	if len(list) == 1 {
		noun = "repo has"
	}

	b.WriteString("\n " + t.Bold(fmt.Sprintf("%d %s changes", len(list), noun)) + "\n")

	rows := make([][]string, 0, len(list))

	for _, rc := range list {
		from, summary := t.Dim("–"), t.Dim("new at "+repos.Short(rc.To))
		if rc.From != "" {
			from, summary = t.Dim(repos.Short(rc.From)), changeCounts(t, rc)
		}

		rows = append(rows, []string{t.Bold(rc.Repo), from + " → " + repos.Short(rc.To), summary, whatHappens(rc)})
	}

	// Table and rule fill their theme's width, and both are indented one cell.
	m := t.Mode()
	m.Width--
	boxed := ui.NewTheme(m)

	b.WriteString(indent(boxed.Table([]string{"REPO", "FROM → TO", "CHANGES", "WHAT HAPPENS"}, rows), " ") + "\n")

	for _, rc := range list {
		if rc.From == "" {
			continue
		}

		b.WriteString("\n " + boxed.Rule(rc.Repo, repos.Short(rc.From)+" → "+repos.Short(rc.To)) + "\n")

		if rc.Err != nil {
			b.WriteString(ui.Fit("   "+t.Dim("change list unavailable: "+rc.Err.Error()), t.Mode().Width-1) + "\n")

			continue
		}

		b.WriteString(changeRows(t, rc))
	}

	return t.Output(b.String())
}

func whatHappens(rc repoChanges) string {
	parts := []string{"rebuild"}
	if rc.Images > 0 {
		parts = append(parts, fmt.Sprintf("images ×%d", rc.Images))
	}

	if rc.Skills {
		parts = append(parts, "workflow skills")
	}

	return strings.Join(append(parts, "restart"), " · ")
}

func changeCounts(t *ui.Theme, rc repoChanges) string {
	if rc.Err != nil {
		return t.Dim("–")
	}

	var prs, deps, direct int

	for _, c := range rc.Changes {
		switch {
		case c.PR > 0 && c.Dependabot:
			deps++
		case c.PR > 0:
			prs++
		default:
			direct++
		}
	}

	var parts []string
	if prs > 0 {
		parts = append(parts, plural(prs, "PR"))
	}

	if direct > 0 {
		parts = append(parts, plural(direct, "commit"))
	}

	s := strings.Join(parts, " · ")

	if deps > 0 {
		d := t.Dim(plural(deps, "dep"))
		if s == "" {
			return d
		}

		s += t.Dim(" · ") + d
	}

	return s
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}

	return fmt.Sprintf("%d %ss", n, word)
}

func changeRows(t *ui.Theme, rc repoChanges) string {
	var main, deps []repos.Change

	for _, c := range rc.Changes {
		if c.Dependabot {
			deps = append(deps, c)
		} else {
			main = append(main, c)
		}
	}

	var b strings.Builder

	for _, c := range main {
		b.WriteString(changeRow(t, rc.Web, c) + "\n")
	}

	if len(deps) > maxDepRowsEach {
		refs := make([]string, 0, len(deps))
		for _, c := range deps {
			refs = append(refs, t.Link(refText(c), refURL(rc.Web, c)))
		}

		lead := "   " + ui.Pad("", refWidth) + t.Badge("deps") + ui.Pad("", scopeWidth)
		line := lead + t.Dim(fmt.Sprintf("%d dependency updates  ", len(deps))+strings.Join(refs, " "))
		b.WriteString(ui.Fit(line, t.Mode().Width-1) + "\n")

		return b.String()
	}

	for _, c := range deps {
		b.WriteString(changeRow(t, rc.Web, c) + "\n")
	}

	return b.String()
}

func refText(c repos.Change) string {
	if c.PR > 0 {
		return fmt.Sprintf("#%d", c.PR)
	}

	return repos.Short(c.SHA)
}

func refURL(web string, c repos.Change) string {
	switch {
	case web == "":
		return ""
	case c.PR > 0:
		return fmt.Sprintf("%s/pull/%d", web, c.PR)
	default:
		return web + "/commit/" + c.SHA
	}
}

func changeRow(t *ui.Theme, web string, c repos.Change) string {
	ref := t.Link(refText(c), refURL(web, c))
	if c.Dependabot {
		ref = t.Dim(ref)
	} else {
		ref = t.Accent(ref)
	}

	subj := ui.ParseSubject(c.Title)
	kind, scope := subj.Type, subj.Scope

	if c.Dependabot {
		kind, scope = "deps", ""
	}

	summary := subj.Summary
	if subj.Breaking {
		summary = t.Bad("! ") + summary
	}

	if c.Dependabot {
		summary = t.Dim(summary)
	}

	extra := ""
	if c.PR > 0 && c.Commits > 1 {
		extra = t.Dim(fmt.Sprintf(" · %d commits", c.Commits))
	}

	lead := "   " + ui.Pad(ref, refWidth) + t.Badge(kind) + ui.Pad(t.Dim(ui.Fit(scope, scopeWidth-1)), scopeWidth)
	room := t.Mode().Width - 1 - lipgloss.Width(lead) - lipgloss.Width(extra)

	return lead + ui.Fit(summary, room) + extra
}

func indent(s, pad string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}

	return strings.Join(lines, "\n")
}
