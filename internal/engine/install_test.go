package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/services"
	"github.com/mhersson/contextmatrix-setup/internal/state"
)

func TestInstallFreshWithDocker(t *testing.T) {
	h := newHarness(t, true)
	a := DefaultAnswers()
	a.AuthMode = "none"
	a.GitHubMode = "pat"
	a.GitHubPAT = "ghp_x"
	a.OpenRouterKey = "or"

	require.NoError(t, h.e.Install(context.Background(), a))

	// Configs exist, are 0600, carry the header and the merged values.
	for _, name := range []string{"server.yaml", "agent.yaml", "chat.yaml"} {
		info, err := os.Stat(filepath.Join(h.e.L.ConfigDir, name))
		require.NoError(t, err, name)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	server, _, err := configsync.LoadFile(h.e.L.ServerConfig())
	require.NoError(t, err)
	assert.Equal(t, 18080, get(t, server, "port"))
	assert.Equal(t, "none", get(t, server, "auth.mode"))
	assert.Equal(t, "info", get(t, server, "log_level"), "schema default kept")
	assert.Equal(t, true, get(t, server, "backends.agent.enabled"))
	assert.Len(t, get(t, server, "mcp_api_key"), 64)
	assert.Regexp(t, `^box-[0-9a-f]{6}$`, get(t, server, "instance.id"))

	agent, _, err := configsync.LoadFile(h.e.L.AgentConfig())
	require.NoError(t, err)
	assert.Equal(t, get(t, server, "backends.agent.api_key"), get(t, agent, "api_key"))
	assert.Equal(t, get(t, server, "mcp_api_key"), get(t, agent, "mcp_api_key"))
	assert.Equal(t, "contextmatrix-agent-worker:bbbbbbb", get(t, agent, "base_image"))
	assert.Equal(t, "if-not-present", get(t, agent, "image_pull_policy"))

	chat, _, err := configsync.LoadFile(h.e.L.ChatConfig())
	require.NoError(t, err)
	assert.Equal(t, "contextmatrix-chat-worker:ccccccc", get(t, chat, "base_image"))

	// Runtime dirs, workflow skills, units.
	for _, d := range h.e.L.RuntimeDirs() {
		_, err := os.Stat(d)
		require.NoError(t, err, d)
	}

	_, err = os.Stat(filepath.Join(h.e.L.WorkflowSkillsDir(), "create-plan.md"))
	require.NoError(t, err)

	for _, name := range []string{"contextmatrix", "contextmatrix-agent", "contextmatrix-chat"} {
		_, err := os.Stat(h.e.Services.UnitPath(name))
		require.NoError(t, err, name)
	}

	unit, _ := os.ReadFile(h.e.Services.UnitPath("contextmatrix"))
	assert.Contains(t, string(unit), "ExecStart="+h.e.Host.GoBin+"/contextmatrix -config "+h.e.L.ServerConfig())
	// The default boards dir sits under StateDir, which is always listed, so
	// serverPaths adds an entry only for a boards dir outside it.
	assert.Contains(t, string(unit), "-"+h.e.L.StateDir)
	agentUnit, _ := os.ReadFile(h.e.Services.UnitPath("contextmatrix-agent"))
	assert.Contains(t, string(agentUnit), "ExecStart="+h.e.Host.GoBin+"/contextmatrix-agent serve --config "+h.e.L.AgentConfig())

	// The server installs its frontend dependencies before its binary; a
	// backend has no frontend and must never see that target.
	var serverMake []string

	for _, c := range h.runner.Calls() {
		if c.Name != "make" {
			continue
		}

		if c.Dir == h.e.L.SrcDir("contextmatrix") {
			serverMake = append(serverMake, c.Args[0])

			continue
		}

		assert.NotContains(t, c.Args, "install-frontend", c.Dir)
	}

	assert.Equal(t, []string{"install-frontend", "install"}, serverMake)

	// Images built for both backends.
	assert.Equal(t, []string{"contextmatrix-agent-worker:bbbbbbb", "contextmatrix-chat-worker:ccccccc"}, h.images.built)

	// Services started: three starts.
	starts := 0

	for _, c := range h.runner.Calls() {
		if c.Name == "systemctl" && len(c.Args) == 3 && c.Args[1] == "start" {
			starts++
		}
	}

	assert.Equal(t, 3, starts)

	// State written.
	st, found, err := state.Load(h.e.L.StateFile())
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "aaaaaaa1111", st.Repos["contextmatrix"].Commit)
	assert.Equal(t, "contextmatrix-agent-worker:bbbbbbb", st.Images["contextmatrix-agent-worker"].Tag)
	assert.True(t, st.Docker)
	assert.Equal(t, "systemd", st.ServiceManager)
	assert.NotEmpty(t, st.Configs["server.yaml"].SHA256)
	assert.NotEmpty(t, st.WorkflowSkills.Files["create-plan.md"])
	assert.Contains(t, h.out.String(), "server.yaml")
}

func TestInstallWithoutDockerAndGitHubSkipped(t *testing.T) {
	h := newHarness(t, false)
	h.runner.On(filepath.Join(h.e.Host.GoBin, "contextmatrix"), "config", "validate").Return("", "error: github.auth_mode is required", 1)

	require.NoError(t, h.e.Install(context.Background(), DefaultAnswers()))

	server, _, err := configsync.LoadFile(h.e.L.ServerConfig())
	require.NoError(t, err)
	assert.Equal(t, false, get(t, server, "backends.agent.enabled"))
	assert.Equal(t, false, get(t, server, "backends.chat.enabled"))
	assert.Empty(t, h.images.built)

	for _, c := range h.runner.Calls() {
		if c.Name == "systemctl" && len(c.Args) == 3 {
			assert.NotEqual(t, "start", c.Args[1], "nothing is started: server invalid, backends without docker")
		}
	}

	out := h.out.String()
	assert.Contains(t, out, "github.auth_mode is required")
	assert.Contains(t, out, "docker")

	st, _, err := state.Load(h.e.L.StateFile())
	require.NoError(t, err)
	assert.False(t, st.Docker)
}

func TestInstallOpensBootstrapLinkInMultiMode(t *testing.T) {
	h := newHarness(t, true)
	h.runner.On("journalctl", "--user", "-u", "contextmatrix", "-f").Return(`msg="auth: bootstrap link" path=/auth/token/tok123`+"\n", "", 0)
	gate := newFollowGate(h.e.Services, false)
	h.e.Services = gate

	var opened string

	h.e.Browser = func(_ context.Context, url string) error {
		opened = url

		return nil
	}

	a := DefaultAnswers()
	a.GitHubMode = "pat"
	a.GitHubPAT = "x"

	require.NoError(t, h.e.Install(context.Background(), a))
	assert.Equal(t, "http://localhost:18080/auth/token/tok123", opened)
	assert.Contains(t, h.out.String(), "/auth/token/tok123")
	assert.True(t, gate.attachedBeforeStart, "the log is followed before the server starts, or the line is missed")
}

func TestInstallFailedServerStartPrintsPlainURL(t *testing.T) {
	h := newHarness(t, true)
	h.runner.On("systemctl", "--user", "start", "contextmatrix").Return("", "Job for contextmatrix.service failed", 1)
	h.e.Services = newFollowGate(h.e.Services, true)

	opened := false
	h.e.Browser = func(context.Context, string) error {
		opened = true

		return nil
	}

	a := DefaultAnswers()
	a.GitHubMode = "pat"
	a.GitHubPAT = "x"

	began := time.Now()

	require.NoError(t, h.e.Install(context.Background(), a))
	assert.Less(t, time.Since(began), 2*time.Second, "no waiting for a link a server that did not start cannot log")

	out := h.out.String()
	assert.Contains(t, out, "start failed: systemctl --user start contextmatrix")
	assert.Contains(t, out, "server URL: http://localhost:18080")
	assert.NotContains(t, out, "first admin")
	assert.False(t, opened)
}

func TestInstallNoLinkSeenNamesTheLogCommand(t *testing.T) {
	h := newHarness(t, true)

	a := DefaultAnswers()
	a.GitHubMode = "pat"
	a.GitHubPAT = "x"

	require.NoError(t, h.e.Install(context.Background(), a))

	out := h.out.String()
	assert.Contains(t, out, "server URL: http://localhost:18080")
	assert.Contains(t, out, "journalctl --user -u contextmatrix -n 50 | grep 'bootstrap link'")
	assert.NotContains(t, out, "users may already exist")

	h = newHarness(t, true)
	h.e.Host.OS = "darwin"
	h.e.Services = services.New("launchd", h.runner, h.e.L, 501)
	h.runner.On("launchctl").Return("", "", 0)
	h.runner.On("tail").Return("", "", 0)

	require.NoError(t, h.e.Install(context.Background(), a))
	assert.Contains(t, h.out.String(), "grep 'bootstrap link' ~/Library/Logs/contextmatrix/contextmatrix.log")
}

func TestInstallWithoutServiceManagerPrintsStartCommands(t *testing.T) {
	h := newHarness(t, true)
	h.e.Host.ServiceManager = "none"
	h.e.Host.ServiceManagerReason = "systemctl not found"
	h.e.Services = services.New("none", h.runner, h.e.L, 1000)

	a := DefaultAnswers()
	a.GitHubMode = "pat"
	a.GitHubPAT = "x"
	require.True(t, a.Services, "the flags and the wizard default to services on")

	began := time.Now()

	require.NoError(t, h.e.Install(context.Background(), a))
	assert.Less(t, time.Since(began), 2*time.Second, "no link can come from a server nobody started")

	out := h.out.String()
	assert.NotContains(t, out, "started")
	assert.Contains(t, out, "services: no usable service manager (systemctl not found); start commands are printed instead")
	assert.Contains(t, out, "start by hand")
	assert.Contains(t, out, h.e.Host.Binary("contextmatrix")+" -config "+h.e.L.ServerConfig())
	assert.Contains(t, out, "server URL: http://localhost:18080")
	assert.Empty(t, systemctlCalls(h))

	st, _, err := state.Load(h.e.L.StateFile())
	require.NoError(t, err)
	assert.Equal(t, "none", st.ServiceManager)
}

func TestInstallLogsDockerHint(t *testing.T) {
	h := newHarness(t, false)
	h.e.Host.DockerHint = "docker is installed but your user cannot reach its socket"

	a := DefaultAnswers()
	a.AuthMode = "none"
	a.GitHubMode = "pat"
	a.GitHubPAT = "x"

	require.NoError(t, h.e.Install(context.Background(), a))
	assert.Contains(t, h.out.String(), "docker not available")
	assert.Contains(t, h.out.String(), "docker is installed but your user cannot reach its socket")

	h.out.b = nil

	s, err := h.e.Status(context.Background())
	require.NoError(t, err)
	h.e.PrintStatus(s)
	assert.Contains(t, h.out.String(), "docker is installed but your user cannot reach its socket")
}

func TestInstallCopiesGitHubAppKey(t *testing.T) {
	h := newHarness(t, true)
	src := filepath.Join(t.TempDir(), "app.pem")
	require.NoError(t, os.WriteFile(src, []byte("PEM"), 0o600))

	a := DefaultAnswers()
	a.GitHubMode = "app"
	a.GitHubAppID = 1
	a.GitHubInstallID = 2
	a.GitHubKeyFile = src

	require.NoError(t, h.e.Install(context.Background(), a))

	data, err := os.ReadFile(filepath.Join(h.e.L.ServerStateDir(), "github-app.pem"))
	require.NoError(t, err)
	assert.Equal(t, "PEM", string(data))

	server, _, _ := configsync.LoadFile(h.e.L.ServerConfig())
	assert.Equal(t, "app", get(t, server, "github.auth_mode"))
}

func TestInstallKeepsGitHubAppKeyWhenSourceIsDestination(t *testing.T) {
	h := newHarness(t, true)
	dst := filepath.Join(h.e.L.ServerStateDir(), "github-app.pem")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o700))

	require.NoError(t, os.WriteFile(dst, []byte("PEM"), 0o600))

	a := DefaultAnswers()
	a.GitHubMode = "app"
	a.GitHubAppID = 1
	a.GitHubInstallID = 2
	a.GitHubKeyFile = dst

	require.NoError(t, h.e.Install(context.Background(), a))

	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "PEM", string(data), "copying a file onto itself must not truncate it")
}

func TestCopyFileThroughSymlinkToItself(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "key.pem")
	link := filepath.Join(dir, "link.pem")

	require.NoError(t, os.WriteFile(dst, []byte("PEM"), 0o600))
	require.NoError(t, os.Symlink(dst, link))

	require.NoError(t, copyFile(link, dst))

	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "PEM", string(data))

	other := filepath.Join(dir, "other.pem")
	require.NoError(t, copyFile(link, other))

	data, err = os.ReadFile(other)
	require.NoError(t, err)
	assert.Equal(t, "PEM", string(data))

	info, err := os.Stat(other)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.NoFileExists(t, other+".tmp")
}

func TestInstallFailsWhenBuildFails(t *testing.T) {
	h := newHarness(t, true)
	h.runner.On("make", "install").Return("", "compile error", 2)

	err := h.e.Install(context.Background(), DefaultAnswers())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contextmatrix")

	_, err = os.Stat(h.e.L.ServerConfig())
	assert.True(t, os.IsNotExist(err), "no config written after a failed build")
}

func TestInstallWithoutServicesRecordsNone(t *testing.T) {
	h := newHarness(t, true)
	a := DefaultAnswers()
	a.AuthMode = "none"
	a.GitHubMode = "pat"
	a.GitHubPAT = "x"
	a.Services = false

	require.NoError(t, h.e.Install(context.Background(), a))

	assert.Empty(t, systemctlCalls(h), "no unit is written, enabled or started")

	st, found, err := state.Load(h.e.L.StateFile())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "none", st.ServiceManager)
	assert.Contains(t, h.out.String(), "start by hand")
}

func TestInstallExpandsTildeInGitHubKeyFile(t *testing.T) {
	h := newHarness(t, true)
	require.NoError(t, os.WriteFile(filepath.Join(h.e.L.Home, "app.pem"), []byte("PEM"), 0o600))

	a := DefaultAnswers()
	a.GitHubMode = "app"
	a.GitHubAppID = 1
	a.GitHubInstallID = 2
	a.GitHubKeyFile = "~/app.pem"

	require.NoError(t, h.e.Install(context.Background(), a))

	data, err := os.ReadFile(filepath.Join(h.e.L.ServerStateDir(), "github-app.pem"))
	require.NoError(t, err)
	assert.Equal(t, "PEM", string(data), "a carried ~ path is read from the home directory")
}
