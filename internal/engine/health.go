package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
)

const (
	defaultHealthBudget    = 60 * time.Second
	defaultHealthInterval  = time.Second
	defaultNotRunningGrace = 15 * time.Second
	probeTimeout           = 2 * time.Second
	reasonNotRunning       = "not running"
)

// healthResult is one restarted service's health. Checked is false when no
// probe ran; Reason then says why, or is empty when there was nothing to
// check.
type healthResult struct {
	Checked, OK bool
	After       time.Duration
	Reason      string
}

// healthURL is the local health endpoint of repo's service, on the port its
// merged config sets.
func healthURL(repo string, tree configsync.Tree) (string, error) {
	port := integer(tree, "port")
	if port <= 0 || port > 65535 {
		return "", fmt.Errorf("no port in %s", filepath.Base(layout.Layout{}.ConfigFor(repo)))
	}

	path := "/health"
	if repo == repos.Server {
		path = "/healthz"
	}

	return fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil
}

// healthBudget returns a context for a set of health waits and arm, which
// starts the budget: once it has run, the context ends with
// context.DeadlineExceeded as its cause. Waits may start before arm.
func (e *Engine) healthBudget(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	budget := cmp.Or(e.HealthBudget, defaultHealthBudget)

	return ctx, func() { time.AfterFunc(budget, func() { cancel(context.DeadlineExceeded) }) }
}

// waitHealthy probes url until it answers, the service has stayed stopped
// past the grace, or ctx ends; the last probe error is the reason. The
// grace lets systemd's Restart=on-failure bring back a startup crash.
func (e *Engine) waitHealthy(ctx context.Context, repo, url string) healthResult {
	probe := e.Probe
	if probe == nil {
		probe = httpProbe
	}

	interval := cmp.Or(e.HealthInterval, defaultHealthInterval)
	grace := cmp.Or(e.NotRunningGrace, defaultNotRunningGrace)
	start := time.Now()

	var stoppedSince time.Time

	for {
		err := probe(ctx, url)
		if err == nil {
			return healthResult{Checked: true, OK: true, After: time.Since(start)}
		}

		active, aerr := e.Services.Active(ctx, repo)

		switch {
		case aerr != nil, ctx.Err() != nil:
			// A failed check, or one the deadline cut short, says nothing
			// about the service.
		case active:
			stoppedSince = time.Time{}
		case stoppedSince.IsZero():
			stoppedSince = time.Now()
		case time.Since(stoppedSince) > grace:
			return healthResult{Checked: true, After: time.Since(start), Reason: reasonNotRunning}
		}

		wait := time.NewTimer(interval)

		select {
		case <-ctx.Done():
			wait.Stop()

			return healthResult{Checked: true, After: time.Since(start), Reason: err.Error()}
		case <-wait.C:
		}
	}
}

// httpProbe is healthy on HTTP 200. Network errors are cut to the cause, so
// the reason fits a table cell.
func httpProbe(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("health request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return probeError(err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return nil
}

func probeError(err error) error {
	var ne net.Error

	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return errors.New("connection refused")
	// The budget's cancel cause is DeadlineExceeded, which net/http returns.
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return errors.New("timed out")
	}

	for {
		inner := errors.Unwrap(err)
		if inner == nil {
			return err
		}

		err = inner
	}
}
