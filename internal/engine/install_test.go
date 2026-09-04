package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
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

func TestInstallFailsWhenBuildFails(t *testing.T) {
	h := newHarness(t, true)
	h.runner.On("make", "install").Return("", "compile error", 2)

	err := h.e.Install(context.Background(), DefaultAnswers())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contextmatrix")

	_, err = os.Stat(h.e.L.ServerConfig())
	assert.True(t, os.IsNotExist(err), "no config written after a failed build")
}
