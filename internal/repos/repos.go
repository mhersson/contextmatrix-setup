// Package repos manages the installer-owned source cache.
package repos

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mhersson/contextmatrix-setup/internal/run"
)

const (
	Setup     = "contextmatrix-setup"
	Server    = "contextmatrix"
	Agent     = "contextmatrix-agent"
	Chat      = "contextmatrix-chat"
	Architect = "contextmatrix-architect"
)

// Apps lists the application repos in install order: the server's config
// schema must exist before the backends are configured against it.
var Apps = []string{Server, Agent, Chat, Architect}

func URL(name string) string {
	return "https://github.com/mhersson/" + name + ".git"
}

type Git struct {
	R run.Runner
}

func (g Git) Sync(ctx context.Context, dir, url string, out io.Writer) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", err
		}

		if err := g.R.Stream(ctx, run.Cmd{Name: "git", Args: []string{"clone", "--quiet", "--branch", "main", url, dir}}, out); err != nil {
			return "", fmt.Errorf("clone %s: %w", url, err)
		}

		return g.Head(ctx, dir)
	}

	if err := g.R.Stream(ctx, run.Cmd{Name: "git", Dir: dir, Args: []string{"fetch", "--quiet", "origin", "main"}}, out); err != nil {
		return "", fmt.Errorf("fetch %s: %w", url, err)
	}

	if err := g.R.Stream(ctx, run.Cmd{Name: "git", Dir: dir, Args: []string{"reset", "--quiet", "--hard", "origin/main"}}, out); err != nil {
		return "", fmt.Errorf("reset %s: %w", dir, err)
	}

	return g.Head(ctx, dir)
}

func (g Git) Head(ctx context.Context, dir string) (string, error) {
	res, err := g.R.Run(ctx, run.Cmd{Name: "git", Dir: dir, Args: []string{"rev-parse", "HEAD"}})
	if err != nil {
		return "", err
	}

	if res.ExitCode != 0 {
		return "", fmt.Errorf("git rev-parse in %s: %s", dir, strings.TrimSpace(res.Stderr))
	}

	return strings.TrimSpace(res.Stdout), nil
}

// Change is one first-parent entry between two commits: a merged PR or a
// direct commit.
type Change struct {
	SHA        string
	Parents    []string
	PR         int
	Title      string
	Dependabot bool
	Commits    int
}

var (
	mergePR  = regexp.MustCompile(`^Merge pull request #(\d+) from (\S+)`)
	squashPR = regexp.MustCompile(`^(.*) \(#(\d+)\)$`)
)

// ParseFirstParent parses `git log --first-parent
// --format=%H%x1f%P%x1f%an%x1f%s%x1f%b%x1e`. Commits is 1 for every entry;
// Changes fills in the real count for merge commits.
func ParseFirstParent(raw string) []Change {
	var out []Change

	for _, rec := range strings.Split(raw, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}

		f := strings.SplitN(rec, "\x1f", 5)
		if len(f) < 5 {
			continue
		}

		sha, parents, author, subject, body := f[0], strings.Fields(f[1]), f[2], f[3], f[4]
		c := Change{SHA: sha, Parents: parents, Title: subject, Commits: 1}

		if m := mergePR.FindStringSubmatch(subject); m != nil && len(parents) == 2 {
			c.PR, _ = strconv.Atoi(m[1])
			c.Title = firstLine(body)
			c.Dependabot = strings.Contains(m[2], "dependabot/")
		} else if m := squashPR.FindStringSubmatch(subject); m != nil {
			c.PR, _ = strconv.Atoi(m[2])
			c.Title = m[1]
		}

		if strings.HasPrefix(author, "dependabot") {
			c.Dependabot = true
		}

		out = append(out, c)
	}

	return out
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}

	return ""
}

// Changes lists the first-parent entries in from..to, newest first.
func (g Git) Changes(ctx context.Context, dir, from, to string) ([]Change, error) {
	res, err := g.R.Run(ctx, run.Cmd{Name: "git", Dir: dir, Args: []string{"log", "--first-parent", "--format=%H%x1f%P%x1f%an%x1f%s%x1f%b%x1e", from + ".." + to}})
	if err != nil {
		return nil, err
	}

	if res.ExitCode != 0 {
		return nil, fmt.Errorf("git log in %s: %s", dir, strings.TrimSpace(res.Stderr))
	}

	list := ParseFirstParent(res.Stdout)

	for i, c := range list {
		if c.PR == 0 || len(c.Parents) != 2 {
			continue
		}

		cnt, err := g.R.Run(ctx, run.Cmd{Name: "git", Dir: dir, Args: []string{"rev-list", "--count", c.Parents[0] + ".." + c.Parents[1]}})
		if err != nil {
			return nil, err
		}

		if cnt.ExitCode != 0 {
			return nil, fmt.Errorf("git rev-list in %s: %s", dir, strings.TrimSpace(cnt.Stderr))
		}

		if n, err := strconv.Atoi(strings.TrimSpace(cnt.Stdout)); err == nil && n > 0 {
			list[i].Commits = n
		}
	}

	return list, nil
}

var webBase = regexp.MustCompile(`^(?:https://github\.com/|ssh://(?:git@)?github\.com/|git@github\.com:)([^/\s]+)/([^/\s]+?)(?:\.git)?/?$`)

// WebBase maps a GitHub clone URL to its web URL, or "" for any other host.
func WebBase(cloneURL string) string {
	m := webBase.FindStringSubmatch(cloneURL)
	if m == nil {
		return ""
	}

	return "https://github.com/" + m[1] + "/" + m[2]
}

// PathChanged reports whether path differs between two commits. git diff
// --quiet exits 1 on a difference, so exit code is the answer.
func (g Git) PathChanged(ctx context.Context, dir, from, to, path string) (bool, error) {
	res, err := g.R.Run(ctx, run.Cmd{Name: "git", Dir: dir, Args: []string{"diff", "--quiet", from, to, "--", path}})
	if err != nil {
		return false, err
	}

	switch res.ExitCode {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("git diff in %s: %s", dir, strings.TrimSpace(res.Stderr))
	}
}

func Short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}

	return commit
}
