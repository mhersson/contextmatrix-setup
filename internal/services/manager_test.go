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

// A unit is enabled only when setup starts it, so writing one leaves a
// service held back at install out of the next boot.
func TestSystemdInstallWritesAndReloadsWithoutEnabling(t *testing.T) {
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

	assert.Equal(t, [][]string{{"--user", "daemon-reload"}}, argsOf(f))

	changed, err = m.Install(context.Background(), agentService())
	require.NoError(t, err)
	assert.False(t, changed, "identical unit is not rewritten")
	assert.Len(t, f.Calls(), 1, "nothing runs for an unchanged unit")
}

func TestSystemdLifecycle(t *testing.T) {
	l := layout.New(t.TempDir(), nil)
	f := run.NewFake()
	f.On("systemctl", "--user", "is-active", "contextmatrix").Return("active\n", "", 0)
	f.On("systemctl").Return("", "", 0)
	f.On("journalctl").Return("line1\nline2\n", "", 0)

	m := New("systemd", f, l, 1000)
	enable := []string{"--user", "enable", "contextmatrix"}

	n := len(f.Calls())

	require.NoError(t, m.Start(context.Background(), Server))
	assertBefore(t, argsOf(f)[n:], enable, []string{"--user", "start", "contextmatrix"}, "start enables the unit first")

	n = len(f.Calls())

	require.NoError(t, m.Restart(context.Background(), Server))
	assertBefore(t, argsOf(f)[n:], enable, []string{"--user", "restart", "contextmatrix"}, "restart enables the unit first")

	require.NoError(t, m.Stop(context.Background(), Server))

	n = len(f.Calls())

	require.NoError(t, m.Disable(context.Background(), Server))
	assert.Equal(t, [][]string{{"--user", "disable", "contextmatrix"}}, argsOf(f)[n:])

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

	target := "gui/501/com.github.mhersson.contextmatrix-agent"
	assert.NotContains(t, argsOf(f), []string{"disable", target}, "a loaded agent is running and stays enabled")

	active, err := m.Active(context.Background(), Agent)
	require.NoError(t, err)
	assert.True(t, active)

	n := len(f.Calls())

	require.NoError(t, m.Restart(context.Background(), Agent))
	assertBefore(t, argsOf(f)[n:], []string{"enable", target}, []string{"kickstart", "-k", target}, "restart enables the agent first")

	require.NoError(t, m.Stop(context.Background(), Agent))

	n = len(f.Calls())

	require.NoError(t, m.Disable(context.Background(), Agent))
	assert.Equal(t, [][]string{{"disable", target}}, argsOf(f)[n:])

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
}

// launchd loads every plist in LaunchAgents at login, so a new plist is
// disabled until Start enables it, the way a systemd unit is enabled only
// when started.
func TestLaunchdInstallNeverBootstraps(t *testing.T) {
	l := layout.New(t.TempDir(), nil)
	f := run.NewFake()
	f.On("launchctl", "print").Return("", "Could not find service", 113)
	f.On("launchctl").Return("", "", 0)

	m := New("launchd", f, l, 501)

	changed, err := m.Install(context.Background(), agentService())
	require.NoError(t, err)
	assert.True(t, changed)

	target := "gui/501/com.github.mhersson.contextmatrix-agent"
	bootstrap := []string{"bootstrap", "gui/501", m.UnitPath(Agent)}
	assert.NotContains(t, argsOf(f), bootstrap, "writing a plist must not start the agent")
	assert.NotContains(t, argsOf(f), []string{"bootout", target})
	assert.Contains(t, argsOf(f), []string{"disable", target}, "a new plist does not load at the next login")
	assert.NotContains(t, argsOf(f), []string{"enable", target})

	changed, err = m.Install(context.Background(), agentService())
	require.NoError(t, err)
	assert.False(t, changed)

	disables := 0

	for _, a := range argsOf(f) {
		if a[0] == "disable" {
			disables++
		}
	}

	assert.Equal(t, 1, disables, "only a new plist is disabled")

	n := len(f.Calls())

	require.NoError(t, m.Start(context.Background(), Agent))
	assertBefore(t, argsOf(f)[n:], []string{"enable", target}, bootstrap, "start enables the agent before loading it")
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
	require.NoError(t, m.Disable(context.Background(), Server))
}

// assertBefore asserts that both calls ran, first before then.
func assertBefore(t *testing.T, args [][]string, first, then []string, msg string) {
	t.Helper()

	i, j := indexOf(args, first), indexOf(args, then)
	require.GreaterOrEqual(t, i, 0, msg)
	require.GreaterOrEqual(t, j, 0, msg)
	assert.Less(t, i, j, msg)
}

func indexOf(args [][]string, want []string) int {
	for i, a := range args {
		if slices.Equal(a, want) {
			return i
		}
	}

	return -1
}
