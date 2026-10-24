package engine

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/mhersson/contextmatrix-setup/internal/host"
	"github.com/mhersson/contextmatrix-setup/internal/images"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/run"
	"github.com/mhersson/contextmatrix-setup/internal/services"
)

type Git interface {
	Sync(ctx context.Context, dir, url string, out io.Writer) (string, error)
	Head(ctx context.Context, dir string) (string, error)
	Changes(ctx context.Context, dir, from, to string) ([]repos.Change, error)
	PathChanged(ctx context.Context, dir, from, to, path string) (bool, error)
}

type Images interface {
	Host(ctx context.Context) string
	BridgeGateway(ctx context.Context) string
	Build(ctx context.Context, repoDir, repo, commit string, out io.Writer) (images.Built, error)
	RemoveImage(ctx context.Context, ref string) error
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
	// PortFree reports whether a port can be bound; nil uses host.PortFree.
	PortFree func(port int) bool
	// Probe checks one health URL; nil uses httpProbe.
	Probe func(ctx context.Context, url string) error
	// HealthBudget bounds the health wait for all restarted services
	// together, counted from the return of the last restart (zero means
	// 60s); HealthInterval spaces one service's probes (zero means 1s).
	// SkipHealth restarts without probing.
	HealthBudget   time.Duration
	HealthInterval time.Duration
	SkipHealth     bool
	// NotRunningGrace is how long a service must stay stopped before the
	// health wait gives up on it; zero means 15s, which outlasts the
	// units' 10s restart delay.
	NotRunningGrace time.Duration
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}

	return time.Now().UTC()
}

func (e *Engine) portFree(port int) bool {
	if e.PortFree != nil {
		return e.PortFree(port)
	}

	return host.PortFree(port)
}

func (e *Engine) repoURL(name string) string {
	if e.RepoURL != nil {
		return e.RepoURL(name)
	}

	return repos.URL(name)
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

// needsDocker reports whether a repo's service runs its workers in
// containers. The architect is one process and runs without docker.
func needsDocker(repo string) bool {
	return repo == repos.Agent || repo == repos.Chat
}
