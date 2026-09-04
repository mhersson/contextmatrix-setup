package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mhersson/contextmatrix-setup/internal/host"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/run"
	"github.com/mhersson/contextmatrix-setup/internal/services"
)

type Git interface {
	Sync(ctx context.Context, dir, url string, out io.Writer) (string, error)
	Head(ctx context.Context, dir string) (string, error)
	Log(ctx context.Context, dir, from, to string) (string, error)
	PathChanged(ctx context.Context, dir, from, to, path string) (bool, error)
}

type Images interface {
	Host(ctx context.Context) string
	BridgeGateway(ctx context.Context) string
	Build(ctx context.Context, repoDir, repo, commit string, out io.Writer) (string, string, error)
	RemoveTag(ctx context.Context, tag string) error
}

type Engine struct {
	L        layout.Layout
	Host     host.Info
	R        run.Runner
	Git      Git
	Images   Images
	Services services.Manager
	Out      io.Writer
	Browser  func(ctx context.Context, url string) error
	Now      func() time.Time
	RepoURL  func(name string) string
	Path     string
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}

	return time.Now().UTC()
}

func (e *Engine) repoURL(name string) string {
	if e.RepoURL != nil {
		return e.RepoURL(name)
	}

	return repos.URL(name)
}

func (e *Engine) path() string {
	if e.Path != "" {
		return e.Path
	}

	return os.Getenv("PATH")
}

// useRecordedManager swaps in the inert manager when the state says "none":
// the user declined service management at install, so no unit is written,
// started or removed on a later run either.
func (e *Engine) useRecordedManager(kind string) {
	if kind == "none" {
		e.Services = services.New("none", e.R, e.L, 0)
	}
}

func (e *Engine) logf(format string, args ...any) {
	fmt.Fprintf(e.Out, format+"\n", args...)
}

func binaryFor(repo string) string {
	return repo
}
