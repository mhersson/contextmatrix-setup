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

func installed(t *testing.T, docker bool) *harness {
	t.Helper()

	h := newHarness(t, docker)
	a := DefaultAnswers()
	a.AuthMode = "none"
	a.GitHubMode = "pat"
	a.GitHubPAT = "x"
	require.NoError(t, h.e.Install(context.Background(), a))
	h.out.b = nil
	h.images.built = nil

	return h
}

func restarts(h *harness) []string {
	var out []string

	for _, c := range h.runner.Calls() {
		if c.Name == "systemctl" && len(c.Args) == 3 && c.Args[1] == "restart" {
			out = append(out, c.Args[2])
		}
	}

	return out
}

func TestUpdateNothingMovedIsQuiet(t *testing.T) {
	h := installed(t, true)

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Contains(t, h.out.String(), "up to date")
	assert.Empty(t, h.images.built)
	assert.Empty(t, restarts(h))
}

func TestUpdateRebuildsMovedRepoAndRestartsIt(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.git.logs["contextmatrix-agent"] = "b999999 fix(agent): something\n"

	var summary string

	opts := UpdateOptions{Confirm: func(s string) bool {
		summary = s

		return true
	}}

	require.NoError(t, h.e.Update(context.Background(), opts))

	assert.Contains(t, summary, "fix(agent): something")
	assert.Equal(t, []string{"contextmatrix-agent-worker:b999999"}, h.images.built)
	assert.Equal(t, []string{"contextmatrix-agent"}, restarts(h))
	assert.Equal(t, []string{"contextmatrix-agent-worker:bbbbbbb"}, h.images.removed, "old tag removed after restart")

	agent, _, _ := configsync.LoadFile(h.e.L.AgentConfig())
	assert.Equal(t, "contextmatrix-agent-worker:b999999", get(t, agent, "base_image"))

	st, _, _ := state.Load(h.e.L.StateFile())
	assert.Equal(t, "b9999999999", st.Repos["contextmatrix-agent"].Commit)
	assert.Equal(t, "aaaaaaa1111", st.Repos["contextmatrix"].Commit)
}

func TestUpdateDeclinedDoesNothing(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix"] = "aaaaaaa9999"

	err := h.e.Update(context.Background(), UpdateOptions{Confirm: func(string) bool { return false }})
	require.NoError(t, err)
	assert.Empty(t, restarts(h))

	st, _, _ := state.Load(h.e.L.StateFile())
	assert.Equal(t, "aaaaaaa1111", st.Repos["contextmatrix"].Commit)
}

func TestUpdateKeepsManualEditsAndDropsStaleKeys(t *testing.T) {
	h := installed(t, true)

	server, _, _ := configsync.LoadFile(h.e.L.ServerConfig())
	configsync.Set(server, "log_level", "debug")
	configsync.Set(server, "obsolete", 1)
	data, _ := configsync.Encode(server, "x")
	require.NoError(t, os.WriteFile(h.e.L.ServerConfig(), data, 0o600))

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	server, _, _ = configsync.LoadFile(h.e.L.ServerConfig())
	assert.Equal(t, "debug", get(t, server, "log_level"))
	_, has := configsync.Get(server, "obsolete")
	assert.False(t, has)
	assert.Contains(t, h.out.String(), "dropped obsolete")
	assert.Contains(t, h.out.String(), "edited by hand")
	assert.Equal(t, []string{"contextmatrix"}, restarts(h), "config changed, server restarted")
}

func TestUpdateWorkflowSkillsOnlyWhenChanged(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix"] = "aaaaaaa2222"
	h.git.changed["workflow-skills"] = false

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))
	data, _ := os.ReadFile(filepath.Join(h.e.L.WorkflowSkillsDir(), "create-plan.md"))
	assert.Equal(t, "plan aaaaaaa1111\n", string(data), "unchanged upstream skills are not recopied")

	h.git.heads["contextmatrix"] = "aaaaaaa3333"
	h.git.changed["workflow-skills"] = true
	require.NoError(t, os.WriteFile(filepath.Join(h.e.L.WorkflowSkillsDir(), "create-plan.md"), []byte("mine\n"), 0o600))

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))
	data, _ = os.ReadFile(filepath.Join(h.e.L.WorkflowSkillsDir(), "create-plan.md"))
	assert.Equal(t, "mine\n", string(data), "locally modified skill is left alone")
	assert.Contains(t, h.out.String(), "locally modified")
}

func TestUpdateDockerAppeared(t *testing.T) {
	h := installed(t, false)
	h.e.Host.Docker = true

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Len(t, h.images.built, 2)
	server, _, _ := configsync.LoadFile(h.e.L.ServerConfig())
	assert.Equal(t, true, get(t, server, "backends.agent.enabled"))
	assert.Equal(t, true, get(t, server, "backends.chat.enabled"))
	assert.Contains(t, h.out.String(), "docker found")

	st, _, _ := state.Load(h.e.L.StateFile())
	assert.True(t, st.Docker)
}

func TestUpdateRequiresInstall(t *testing.T) {
	h := newHarness(t, true)
	err := h.e.Update(context.Background(), UpdateOptions{Yes: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install")
}

func TestStatusAndUninstall(t *testing.T) {
	h := installed(t, true)
	h.runner.On("systemctl", "--user", "is-active").Return("active\n", "", 0)

	s, err := h.e.Status(context.Background())
	require.NoError(t, err)
	assert.Len(t, s.Repos, 3)
	assert.Equal(t, "aaaaaaa", s.Repos[0].Installed[:7])
	assert.Equal(t, 18080, s.Ports["contextmatrix"])
	assert.True(t, s.Docker)
	assert.Equal(t, "contextmatrix-agent-worker:bbbbbbb", s.Images["contextmatrix-agent-worker"])

	h.e.PrintStatus(s)
	assert.Contains(t, h.out.String(), "18080")

	require.NoError(t, h.e.Uninstall(context.Background()))

	for _, name := range []string{"contextmatrix", "contextmatrix-agent", "contextmatrix-chat"} {
		_, err := os.Stat(h.e.Services.UnitPath(name))
		assert.True(t, os.IsNotExist(err), name)
	}

	_, err = os.Stat(h.e.L.ServerConfig())
	assert.NoError(t, err, "configs survive uninstall")
}
