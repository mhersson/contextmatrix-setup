package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/migrate"
	"github.com/mhersson/contextmatrix-setup/internal/state"
)

func TestMigrateThenInstallCarriesValuesOver(t *testing.T) {
	h := newHarness(t, true)
	l := h.e.L

	write := func(p, s string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(s), 0o600))
	}

	write(l.OldServerConfig(), "port: 8080\nmcp_api_key: OLDMCP\ngithub:\n  auth_mode: pat\n  pat:\n    token: T\nboards:\n  dir: ~/contextmatrix-boards\n")
	write(l.OldAgentConfig(), "port: 9092\napi_key: OLDAGENT\n")
	write(l.OldChatConfig(), "port: 9093\napi_key: OLDCHAT\n")
	write(filepath.Join(l.OldStateDir, "master.key"), "K")
	write(filepath.Join(l.OldStateDir, "instance_id"), "oldbox-111111\n")
	write(filepath.Join(l.OldWorkflowSkillsDir(), "create-plan.md"), "old skill\n")

	found := migrate.Detect(l)
	require.True(t, found.Any())

	plan, err := migrate.Build(l, found, nil)
	require.NoError(t, err)
	require.NoError(t, h.e.Migrate(context.Background(), plan))

	// Old files are gone, state files moved, trees written at the new paths.
	_, err = os.Stat(l.OldServerConfig())
	assert.True(t, os.IsNotExist(err))
	data, err := os.ReadFile(filepath.Join(l.ServerStateDir(), "master.key"))
	require.NoError(t, err)
	assert.Equal(t, "K", string(data))

	st, _, err := state.Load(l.StateFile())
	require.NoError(t, err)
	require.NotNil(t, st.Migration)
	assert.Contains(t, st.Migration.From, l.OldServerConfig())

	// Install afterwards keeps every carried value.
	answers := AnswersFrom(Trees{Server: plan.Server, Agent: plan.Agent, Chat: plan.Chat})
	assert.Equal(t, 8080, answers.ServerPort)

	require.NoError(t, h.e.Install(context.Background(), answers))

	server, _, _ := configsync.LoadFile(l.ServerConfig())
	assert.Equal(t, "OLDMCP", get(t, server, "mcp_api_key"))
	assert.Equal(t, "oldbox-111111", get(t, server, "instance.id"))
	assert.Equal(t, "T", get(t, server, "github.pat.token"))
	assert.Equal(t, "~/contextmatrix-boards", get(t, server, "boards.dir"))
	assert.Equal(t, 8080, get(t, server, "port"))

	agent, _, _ := configsync.LoadFile(l.AgentConfig())
	assert.Equal(t, "OLDAGENT", get(t, agent, "api_key"))
	assert.Equal(t, "OLDMCP", get(t, agent, "mcp_api_key"))

	data, err = os.ReadFile(filepath.Join(l.WorkflowSkillsDir(), "create-plan.md"))
	require.NoError(t, err)
	assert.Equal(t, "plan aaaaaaa1111\n", string(data), "install refreshes skills from the checkout")

	unit, _ := os.ReadFile(h.e.Services.UnitPath("contextmatrix"))
	assert.Contains(t, string(unit), "-"+filepath.Join(l.Home, "contextmatrix-boards"), "kept-in-place boards dir is writable")
}
