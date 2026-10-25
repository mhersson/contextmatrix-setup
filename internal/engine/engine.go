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
	"github.com/mhersson/contextmatrix-setup/internal/state"
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

// CheckServiceManager stops a run before it fetches anything when the
// install recorded a service manager this run cannot reach.
func (e *Engine) CheckServiceManager() error {
	st, _, err := state.Load(e.L.StateFile())
	if err != nil {
		return err
	}

	return e.recordedManagerReachable(st.ServiceManager)
}

// recordedManagerReachable fails when the install recorded systemd or
// launchd and this run detects none, as from an SSH session or a cron job
// without the user manager: going on would restart nothing.
func (e *Engine) recordedManagerReachable(recorded string) error {
	if (recorded != "systemd" && recorded != "launchd") || e.Services.Kind() != "none" {
		return nil
	}

	reason := e.Host.ServiceManagerReason
	if reason == "" {
		reason = "none detected"
	}

	return fmt.Errorf("recorded service manager is %s but the user manager is not reachable (%s); rerun when it is", recorded, reason)
}

func (e *Engine) logf(format string, args ...any) {
	fmt.Fprintf(e.Out, format+"\n", args...)
}

// needsDocker reports whether a repo's service runs its workers in
// containers. The architect is one process and runs without docker.
func needsDocker(repo string) bool {
	return repo == repos.Agent || repo == repos.Chat
}

// disableHeldBack disables, best-effort, the unit of each repo whose skip
// reason keeps it from running at all, so a unit enabled before does not
// start at boot or login on a file its validator rejects. A backend with a
// built image is left alone: its file is complete and docker may be down
// only for now. Each failure comes back as a note.
func (e *Engine) disableHeldBack(ctx context.Context, github bool, built map[string]state.Image) []note {
	var notes []note

	for _, repo := range repos.Apps {
		if e.skipReason(repo, github) == "" || (needsDocker(repo) && built[images.Family(repo)].Tag != "") {
			continue
		}

		if err := e.Services.Disable(ctx, repo); err != nil {
			notes = append(notes, note{repo, "could not disable its unit: " + err.Error()})
		}
	}

	return notes
}

// skipReason names the documented reason a repo is not started on this
// host, "" when there is none. Its file lacks the matching values until
// then, which its validator rejects, so such a repo is not validated either.
func (e *Engine) skipReason(repo string, github bool) string {
	switch {
	case repo == repos.Server && !github:
		return "github block missing in server.yaml; fill it in, then run update"
	case needsDocker(repo) && !e.Host.Docker:
		return "docker not available"
	default:
		return ""
	}
}
