// Package images builds and tags the per-commit worker images.
package images

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/run"
)

const defaultSocket = "unix:///var/run/docker.sock"

func Family(repo string) string {
	switch repo {
	case repos.Agent, repos.Chat:
		return repo + "-worker"
	default:
		return ""
	}
}

func Tag(repo, commit string) string {
	return Family(repo) + ":" + repos.Short(commit)
}

type Docker struct {
	R run.Runner
}

func (d Docker) Host(ctx context.Context) string {
	res, err := d.R.Run(ctx, run.Cmd{
		Name: "docker",
		Args: []string{"context", "inspect", "--format", "{{.Endpoints.docker.Host}}"},
	})
	if err != nil || res.ExitCode != 0 {
		return ""
	}

	host := strings.TrimSpace(res.Stdout)
	if host == defaultSocket {
		return ""
	}

	return host
}

func (d Docker) BridgeGateway(ctx context.Context) string {
	res, err := d.R.Run(ctx, run.Cmd{
		Name: "docker",
		Args: []string{"network", "inspect", "bridge", "--format", "{{(index .IPAM.Config 0).Gateway}}"},
	})
	if err != nil || res.ExitCode != 0 || strings.TrimSpace(res.Stdout) == "" {
		return "172.17.0.1"
	}

	return strings.TrimSpace(res.Stdout)
}

// Build runs the repo's own image target and retags the result per commit.
// A failed build returns before tagging, so the previous tag stays valid.
func (d Docker) Build(ctx context.Context, repoDir, repo, commit string, out io.Writer) (string, string, error) {
	family := Family(repo)
	if family == "" {
		return "", "", fmt.Errorf("%s has no worker image", repo)
	}

	if err := d.R.Stream(ctx, run.Cmd{Name: "make", Args: []string{"docker-worker"}, Dir: repoDir}, out); err != nil {
		return "", "", fmt.Errorf("build %s image: %w", repo, err)
	}

	tag := Tag(repo, commit)

	res, err := d.R.Run(ctx, run.Cmd{Name: "docker", Args: []string{"tag", family + ":dev", tag}})
	if err != nil {
		return "", "", err
	}

	if res.ExitCode != 0 {
		return "", "", fmt.Errorf("docker tag %s: %s", tag, strings.TrimSpace(res.Stderr))
	}

	id, err := d.ImageID(ctx, tag)
	if err != nil {
		return "", "", err
	}

	return tag, id, nil
}

func (d Docker) ImageID(ctx context.Context, ref string) (string, error) {
	res, err := d.R.Run(ctx, run.Cmd{Name: "docker", Args: []string{"image", "inspect", "--format", "{{.Id}}", ref}})
	if err != nil {
		return "", err
	}

	if res.ExitCode != 0 {
		return "", fmt.Errorf("docker image inspect %s: %s", ref, strings.TrimSpace(res.Stderr))
	}

	return strings.TrimSpace(res.Stdout), nil
}

func (d Docker) RemoveTag(ctx context.Context, tag string) error {
	res, err := d.R.Run(ctx, run.Cmd{Name: "docker", Args: []string{"rmi", tag}})
	if err != nil {
		return err
	}

	if res.ExitCode != 0 {
		return fmt.Errorf("docker rmi %s: %s", tag, strings.TrimSpace(res.Stderr))
	}

	return nil
}
