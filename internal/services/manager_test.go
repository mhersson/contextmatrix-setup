package services

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/run"
)

func TestSystemdInstallWritesReloadsEnables(t *testing.T) {
	home := t.TempDir()
	l := layout.New(home, nil)
	f := run.NewFake()
	f.On("systemctl").Return("", "", 0)

	m := New("systemd", f, l, 1000)
	assert.Equal(t, "systemd", m.Kind())
	assert.Equal(t, filepath.Join(home, ".config", "systemd", "user", "contextmatrix-agent.service"), m.UnitPath(Agent))

	changed, err := m.Install(context.Background(), agentService())
	require.NoError(t, err)
	assert.True(t, changed)

	data, err := os.ReadFile(m.UnitPath(Agent))
	require.NoError(t, err)
	assert.Equal(t, wantSystemd, string(data))

	calls := f.Calls()
	require.Len(t, calls, 2)
	assert.Equal(t, []string{"--user", "daemon-reload"}, calls[0].Args)
	assert.Equal(t, []string{"--user", "enable", "contextmatrix-agent"}, calls[1].Args)

	changed, err = m.Install(context.Background(), agentService())
	require.NoError(t, err)
	assert.False(t, changed, "identical unit is not rewritten")
	assert.Len(t, f.Calls(), 3, "only enable runs on an unchanged unit")
}

func TestSystemdLifecycle(t *testing.T) {
	l := layout.New(t.TempDir(), nil)
	f := run.NewFake()
	f.On("systemctl", "--user", "is-active", "contextmatrix").Return("active\n", "", 0)
	f.On("systemctl").Return("", "", 0)
	f.On("journalctl").Return("line1\nline2\n", "", 0)

	m := New("systemd", f, l, 1000)

	require.NoError(t, m.Start(context.Background(), Server))
	require.NoError(t, m.Restart(context.Background(), Server))
	require.NoError(t, m.Stop(context.Background(), Server))

	active, err := m.Active(context.Background(), Server)
	require.NoError(t, err)
	assert.True(t, active)

	logs, err := m.Logs(context.Background(), Server, 20)
	require.NoError(t, err)
	assert.Equal(t, "line1\nline2\n", logs)

	require.NoError(t, m.Remove(context.Background(), Server), "missing unit file is fine")

	args := [][]string{}
	for _, c := range f.Calls() {
		args = append(args, c.Args)
	}

	assert.Contains(t, args, []string{"--user", "start", "contextmatrix"})
	assert.Contains(t, args, []string{"--user", "restart", "contextmatrix"})
	assert.Contains(t, args, []string{"--user", "stop", "contextmatrix"})
	assert.Contains(t, args, []string{"--user", "-u", "contextmatrix", "-n", "20", "--no-pager", "-o", "cat"})
	assert.Contains(t, args, []string{"--user", "disable", "contextmatrix"})
}

func TestLaunchdInstallAndLifecycle(t *testing.T) {
	home := t.TempDir()
	l := layout.New(home, nil)
	f := run.NewFake()
	f.On("launchctl", "print", "gui/501/com.github.mhersson.contextmatrix-agent").Return("state = running\n", "", 0)
	f.On("launchctl").Return("", "", 0)
	f.On("tail").Return("l1\n", "", 0)

	m := New("launchd", f, l, 501)
	assert.Equal(t, filepath.Join(home, "Library", "LaunchAgents", "com.github.mhersson.contextmatrix-agent.plist"), m.UnitPath(Agent))

	changed, err := m.Install(context.Background(), agentService())
	require.NoError(t, err)
	assert.True(t, changed)

	active, err := m.Active(context.Background(), Agent)
	require.NoError(t, err)
	assert.True(t, active)

	require.NoError(t, m.Restart(context.Background(), Agent))
	require.NoError(t, m.Stop(context.Background(), Agent))

	logs, err := m.Logs(context.Background(), Agent, 5)
	require.NoError(t, err)
	assert.Equal(t, "l1\n", logs)

	args := [][]string{}
	for _, c := range f.Calls() {
		args = append(args, c.Args)
	}

	// print reports the agent as loaded, so a changed plist is booted out
	// and bootstrapped again so the running agent picks it up.
	bootout := []string{"bootout", "gui/501/com.github.mhersson.contextmatrix-agent"}
	bootstrap := []string{"bootstrap", "gui/501", m.UnitPath(Agent)}

	assert.Contains(t, args, bootout)
	assert.Contains(t, args, bootstrap)
	assert.Less(t, indexOf(args, bootout), indexOf(args, bootstrap), "bootout precedes bootstrap")
	assert.Contains(t, args, []string{"kickstart", "-k", "gui/501/com.github.mhersson.contextmatrix-agent"})
}

func TestLaunchdInstallNeverBootstraps(t *testing.T) {
	l := layout.New(t.TempDir(), nil)
	f := run.NewFake()
	f.On("launchctl", "print").Return("", "Could not find service", 113)
	f.On("launchctl").Return("", "", 0)

	m := New("launchd", f, l, 501)

	changed, err := m.Install(context.Background(), agentService())
	require.NoError(t, err)
	assert.True(t, changed)

	bootstrap := []string{"bootstrap", "gui/501", m.UnitPath(Agent)}
	assert.NotContains(t, argsOf(f), bootstrap, "writing a plist must not start the agent")
	assert.NotContains(t, argsOf(f), []string{"bootout", "gui/501/com.github.mhersson.contextmatrix-agent"})

	require.NoError(t, m.Start(context.Background(), Agent))
	assert.Contains(t, argsOf(f), bootstrap)
}

func argsOf(f *run.Fake) [][]string {
	args := [][]string{}
	for _, c := range f.Calls() {
		args = append(args, c.Args)
	}

	return args
}

func TestNoneManagerIsInert(t *testing.T) {
	m := New("none", run.NewFake(), layout.New(t.TempDir(), nil), 1000)
	assert.Equal(t, "none", m.Kind())
	assert.Empty(t, m.UnitPath(Server))

	changed, err := m.Install(context.Background(), agentService())
	require.NoError(t, err)
	assert.False(t, changed)

	active, err := m.Active(context.Background(), Server)
	require.NoError(t, err)
	assert.False(t, active)
	require.NoError(t, m.Start(context.Background(), Server))
}

func indexOf(args [][]string, want []string) int {
	for i, a := range args {
		if slices.Equal(a, want) {
			return i
		}
	}

	return -1
}
