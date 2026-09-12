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

// Variants are the slim targets both backend Makefiles build next to the
// default image. Their tags are stable, so a project's worker_image override
// keeps resolving after an update.
var Variants = []string{"go-node", "python", "rust"}

func VariantTag(repo, variant string) string {
	return Family(repo) + ":" + variant
}

// Built records one build of a repo's worker images: the per-commit tag of
// the default image and the image ID behind each variant tag.
type Built struct {
	Tag      string
	ID       string
	Variants map[string]string
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

// Build runs the repo's image targets and retags the default result per
// commit. A failed build returns before tagging, so the previous tag stays
// valid; the variant tags move as their builds finish.
func (d Docker) Build(ctx context.Context, repoDir, repo, commit string, out io.Writer) (Built, error) {
	family := Family(repo)
	if family == "" {
		return Built{}, fmt.Errorf("%s has no worker image", repo)
	}

	if err := d.R.Stream(ctx, run.Cmd{Name: "make", Args: []string{"docker-worker"}, Dir: repoDir}, out); err != nil {
		return Built{}, fmt.Errorf("build %s image: %w", repo, err)
	}

	if err := d.R.Stream(ctx, run.Cmd{Name: "make", Args: []string{"docker-worker-variants"}, Dir: repoDir}, out); err != nil {
		return Built{}, fmt.Errorf("build %s image variants: %w", repo, err)
	}

	b := Built{Tag: Tag(repo, commit), Variants: map[string]string{}}

	res, err := d.R.Run(ctx, run.Cmd{Name: "docker", Args: []string{"tag", family + ":dev", b.Tag}})
	if err != nil {
		return Built{}, err
	}

	if res.ExitCode != 0 {
		return Built{}, fmt.Errorf("docker tag %s: %s", b.Tag, strings.TrimSpace(res.Stderr))
	}

	if b.ID, err = d.ImageID(ctx, b.Tag); err != nil {
		return Built{}, err
	}

	for _, v := range Variants {
		id, err := d.ImageID(ctx, VariantTag(repo, v))
		if err != nil {
			return Built{}, err
		}

		b.Variants[v] = id
	}

	return b, nil
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

func (d Docker) RemoveImage(ctx context.Context, ref string) error {
	res, err := d.R.Run(ctx, run.Cmd{Name: "docker", Args: []string{"rmi", ref}})
	if err != nil {
		return err
	}

	if res.ExitCode != 0 {
		return fmt.Errorf("docker rmi %s: %s", ref, strings.TrimSpace(res.Stderr))
	}

	return nil
}
