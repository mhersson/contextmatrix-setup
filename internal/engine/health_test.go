package engine

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/services"
)

func refused(context.Context, string) error { return errors.New("connection refused") }

// restartAgent restarts the agent and waits for its health the way Update
// does, within the health budget.
func restartAgent(t *testing.T, e *Engine) healthResult {
	t.Helper()

	urls := map[string]string{repos.Agent: "http://127.0.0.1:19092/health"}
	o := <-e.restartInOrder(context.Background(), []string{repos.Agent}, urls)[repos.Agent]
	require.NoError(t, o.err)

	return o.health
}

func TestHealthHealthyAfterRetries(t *testing.T) {
	e := newHarness(t, true).e

	var calls atomic.Int32

	e.Probe = func(context.Context, string) error {
		if calls.Add(1) <= 2 {
			return errors.New("connection refused")
		}

		return nil
	}

	r := restartAgent(t, e)

	assert.True(t, r.Checked)
	assert.True(t, r.OK)
	assert.Empty(t, r.Reason)
	assert.Equal(t, int32(3), calls.Load())
}

func TestHealthTimesOutWithLastError(t *testing.T) {
	tests := []struct {
		name   string
		probe  func(context.Context, string) error
		reason string
	}{
		{"refused", refused, "connection refused"},
		{"hanging server", func(ctx context.Context, _ string) error {
			<-ctx.Done()

			return errors.New("timed out")
		}, "timed out"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newHarness(t, true).e
			e.Probe = tt.probe
			e.HealthBudget = 20 * time.Millisecond

			start := time.Now()
			r := restartAgent(t, e)

			assert.True(t, r.Checked)
			assert.False(t, r.OK)
			assert.Equal(t, tt.reason, r.Reason)
			assert.Less(t, time.Since(start), 5*time.Second, "the budget ends the wait")
		})
	}
}

// inactive reports every service stopped, the way a crashed unit looks.
type inactive struct{ services.Manager }

func (inactive) Active(context.Context, string) (bool, error) { return false, nil }

func TestHealthStopsEarlyWhenServiceNotRunning(t *testing.T) {
	e := newHarness(t, true).e
	e.Services = inactive{e.Services}
	e.Probe = refused
	e.HealthBudget = 10 * time.Second
	e.NotRunningGrace = 20 * time.Millisecond

	start := time.Now()
	r := restartAgent(t, e)

	assert.True(t, r.Checked)
	assert.False(t, r.OK)
	assert.Equal(t, "not running", r.Reason)
	assert.Less(t, time.Since(start), time.Second, "a stopped service is not waited for")
}

// startsLate reports a service stopped for its first few checks, the way a
// unit looks while systemd waits to restart it after a startup crash.
type startsLate struct {
	services.Manager

	stopped atomic.Int32
}

func (m *startsLate) Active(context.Context, string) (bool, error) {
	return m.stopped.Add(-1) < 0, nil
}

func TestHealthRidesOutAnAutoRestart(t *testing.T) {
	e := newHarness(t, true).e
	m := &startsLate{Manager: e.Services}
	m.stopped.Store(3)
	e.Services = m
	e.NotRunningGrace = time.Second
	e.HealthBudget = 10 * time.Second

	var calls atomic.Int32

	e.Probe = func(context.Context, string) error {
		if calls.Add(1) <= 4 {
			return errors.New("connection refused")
		}

		return nil
	}

	r := restartAgent(t, e)

	assert.True(t, r.OK, "stopped for less than the grace, then healthy: %+v", r)
}

// A deadline that cuts systemctl short makes the service look stopped; that
// answer must not count.
func TestHealthIgnoresAnActiveCheckCutOffByTheDeadline(t *testing.T) {
	e := newHarness(t, true).e
	e.Services = inactive{e.Services}
	e.NotRunningGrace = 5 * time.Millisecond
	e.HealthBudget = 30 * time.Millisecond

	var calls atomic.Int32

	e.Probe = func(ctx context.Context, _ string) error {
		if calls.Add(1) == 1 {
			return errors.New("connection refused")
		}

		<-ctx.Done()

		return errors.New("timed out")
	}

	r := restartAgent(t, e)

	assert.Equal(t, "timed out", r.Reason)
}

// flapping is stopped, then briefly back, then stopped again: two stretches
// each shorter than the grace, together longer.
type flapping struct {
	services.Manager

	start time.Time
}

func (m *flapping) Active(context.Context, string) (bool, error) {
	d := time.Since(m.start)

	return (d >= 70*time.Millisecond && d < 100*time.Millisecond) || d >= 170*time.Millisecond, nil
}

func TestHealthGraceRestartsWhenTheServiceComesBack(t *testing.T) {
	e := newHarness(t, true).e
	e.Services = &flapping{Manager: e.Services, start: time.Now()}
	e.NotRunningGrace = 100 * time.Millisecond
	e.HealthInterval = 2 * time.Millisecond
	e.HealthBudget = 10 * time.Second

	start := time.Now()
	e.Probe = func(context.Context, string) error {
		if time.Since(start) < 180*time.Millisecond {
			return errors.New("connection refused")
		}

		return nil
	}

	r := restartAgent(t, e)

	assert.True(t, r.OK, "neither stopped stretch outlasted the grace: %+v", r)
}

func TestHealthNoPortFailsFast(t *testing.T) {
	_, err := healthURL(repos.Agent, configsync.Tree{"log_level": "info"})
	require.ErrorContains(t, err, "agent.yaml")

	h := installed(t, true)

	agent, _, err := configsync.LoadFile(h.e.L.AgentConfig())
	require.NoError(t, err)
	configsync.Set(agent, "port", 0)
	data, err := configsync.Encode(agent, referenceFor(repos.Agent))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(h.e.L.AgentConfig(), data, 0o600))

	var calls atomic.Int32

	h.e.Probe = func(context.Context, string) error {
		calls.Add(1)

		return nil
	}

	err = h.e.Update(context.Background(), UpdateOptions{Yes: true})

	var pe *ProblemsError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, 1, pe.N)
	assert.Equal(t, []string{repos.Agent}, restarts(h))
	assert.Zero(t, calls.Load(), "nothing to probe without a port")
	assert.Contains(t, resultRow(t, h.out.String(), repos.Agent), "no port in agent.yaml")
}

func TestHealthURLPerService(t *testing.T) {
	tests := []struct {
		repo string
		port int
		want string
	}{
		{repos.Server, 18080, "http://127.0.0.1:18080/healthz"},
		{repos.Agent, 19092, "http://127.0.0.1:19092/health"},
		{repos.Chat, 19093, "http://127.0.0.1:19093/health"},
		{repos.Architect, 19111, "http://127.0.0.1:19111/health"},
	}

	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			got, err := healthURL(tt.repo, configsync.Tree{"port": tt.port})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHTTPProbe(t *testing.T) {
	var status atomic.Int32

	status.Store(http.StatusOK)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(status.Load()))
	}))
	defer srv.Close()

	require.NoError(t, httpProbe(context.Background(), srv.URL+"/health"))

	status.Store(http.StatusServiceUnavailable)
	require.EqualError(t, httpProbe(context.Background(), srv.URL+"/health"), "HTTP 503")

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	closed := "http://" + l.Addr().String() + "/health"
	require.NoError(t, l.Close())
	require.EqualError(t, httpProbe(context.Background(), closed), "connection refused")

	hang := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer hang.Close()

	e := newHarness(t, true).e
	e.HealthBudget = 20 * time.Millisecond

	budget, arm := e.healthBudget(context.Background())
	arm()
	require.EqualError(t, httpProbe(budget, hang.URL+"/health"), "timed out", "the budget ending a probe reads as a timeout")
}

// resultRow returns the result table row naming repo, found below the
// Result heading so the changes table above cannot match.
func resultRow(t *testing.T, out, repo string) string {
	t.Helper()

	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "Result" {
			continue
		}

		for _, row := range lines[i+1:] {
			if strings.HasPrefix(strings.TrimSpace(row), "│") && strings.Contains(row, repo+" ") {
				return row
			}
		}
	}

	t.Fatalf("no result row for %s in:\n%s", repo, out)

	return ""
}
