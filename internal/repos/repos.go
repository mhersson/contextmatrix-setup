// Package repos manages the installer-owned source cache.
package repos

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mhersson/contextmatrix-setup/internal/run"
)

const (
	Setup  = "contextmatrix-setup"
	Server = "contextmatrix"
	Agent  = "contextmatrix-agent"
	Chat   = "contextmatrix-chat"
)

// Apps lists the application repos in install order: the server's config
// schema must exist before the backends are configured against it.
var Apps = []string{Server, Agent, Chat}

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

func (g Git) Log(ctx context.Context, dir, from, to string) (string, error) {
	res, err := g.R.Run(ctx, run.Cmd{Name: "git", Dir: dir, Args: []string{"log", "--oneline", "--no-decorate", from + ".." + to}})
	if err != nil {
		return "", err
	}

	if res.ExitCode != 0 {
		return "", fmt.Errorf("git log in %s: %s", dir, strings.TrimSpace(res.Stderr))
	}

	return res.Stdout, nil
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
