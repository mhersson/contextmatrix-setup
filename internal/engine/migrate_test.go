package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/migrate"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
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
	write(l.OldAgentConfig(), "port: 9092\napi_key: OLDAGENT\nbase_image: contextmatrix-agent-worker:dev\n")
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
	answers, _ := AnswersFrom(Trees{Server: plan.Server, Agent: plan.Agent, Chat: plan.Chat})
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
	assert.Equal(t, "contextmatrix-agent-worker:bbbbbbb", get(t, agent, "base_image"), "the image this install built replaces the carried one")

	data, err = os.ReadFile(filepath.Join(l.WorkflowSkillsDir(), "create-plan.md"))
	require.NoError(t, err)
	assert.Equal(t, "plan aaaaaaa1111\n", string(data), "install refreshes skills from the checkout")

	unit, _ := os.ReadFile(h.e.Services.UnitPath("contextmatrix"))
	assert.Contains(t, string(unit), "-"+filepath.Join(l.Home, "contextmatrix-boards"), "kept-in-place boards dir is writable")
}

func writeOldLayout(t *testing.T, l layout.Layout) {
	t.Helper()

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
}

func TestMigrateIsRerunnable(t *testing.T) {
	h := newHarness(t, true)
	l := h.e.L

	writeOldLayout(t, l)

	first, err := migrate.Build(l, migrate.Detect(l), nil)
	require.NoError(t, err)
	require.NoError(t, h.e.Migrate(context.Background(), first))

	// The second run detects nothing left to migrate. It must not write a
	// near-empty tree over the config the first run carried.
	second, err := migrate.Build(l, migrate.Detect(l), nil)
	require.NoError(t, err)
	require.NoError(t, h.e.Migrate(context.Background(), second))

	server, _, err := configsync.LoadFile(l.ServerConfig())
	require.NoError(t, err)
	assert.Equal(t, "OLDMCP", get(t, server, "mcp_api_key"))
	assert.Equal(t, "oldbox-111111", get(t, server, "instance.id"))
	assert.Equal(t, "T", get(t, server, "github.pat.token"))

	data, err := os.ReadFile(filepath.Join(l.ServerStateDir(), "master.key"))
	require.NoError(t, err)
	assert.Equal(t, "K", string(data))

	st, _, err := state.Load(l.StateFile())
	require.NoError(t, err)
	require.NotNil(t, st.Migration)
	assert.Contains(t, st.Migration.From, l.OldServerConfig(), "the rerun must not erase the record")
}

func TestMigrateSkipsAConfigWithNoOldFile(t *testing.T) {
	h := newHarness(t, true)
	l := h.e.L

	require.NoError(t, os.MkdirAll(filepath.Dir(l.OldAgentConfig()), 0o755))
	require.NoError(t, os.WriteFile(l.OldAgentConfig(), []byte("port: 9092\napi_key: OLDAGENT\n"), 0o600))

	plan, err := migrate.Build(l, migrate.Detect(l), nil)
	require.NoError(t, err)
	assert.False(t, plan.HasServer)

	require.NoError(t, h.e.Migrate(context.Background(), plan))

	_, err = os.Stat(l.ServerConfig())
	assert.True(t, os.IsNotExist(err), "a config with no old file is never written")

	agent, _, err := configsync.LoadFile(l.AgentConfig())
	require.NoError(t, err)
	assert.Equal(t, "OLDAGENT", get(t, agent, "api_key"))
}

func TestInstallAfterMigrationForcesChangedAnswers(t *testing.T) {
	h := newHarness(t, true)
	l := h.e.L

	writeOldLayout(t, l)

	plan, err := migrate.Build(l, migrate.Detect(l), nil)
	require.NoError(t, err)
	require.NoError(t, h.e.Migrate(context.Background(), plan))

	// What the wizard shows after a migration, with two values changed.
	answers, _ := AnswersFrom(Trees{Server: plan.Server, Agent: plan.Agent, Chat: plan.Chat})
	answers.ServerPort = 28080
	answers.AuthMode = "none"

	require.NoError(t, h.e.Install(context.Background(), answers))

	server, _, err := configsync.LoadFile(l.ServerConfig())
	require.NoError(t, err)
	assert.Equal(t, 28080, get(t, server, "port"))
	assert.Equal(t, "none", get(t, server, "auth.mode"))
	assert.Equal(t, "OLDMCP", get(t, server, "mcp_api_key"), "carried secret stays")
	assert.Equal(t, "T", get(t, server, "github.pat.token"), "carried token stays when the answer says skip")
	assert.Equal(t, "pat", get(t, server, "github.auth_mode"))
	assert.Equal(t, "~/contextmatrix-boards", get(t, server, "boards.dir"), "an unchanged answer forces nothing")

	agent, _, err := configsync.LoadFile(l.AgentConfig())
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:28080", get(t, agent, "contextmatrix_url"))
	assert.Equal(t, "http://172.17.0.1:28080", get(t, agent, "container_contextmatrix_url"))
	assert.Equal(t, 9092, get(t, agent, "port"), "unchanged port stays carried")
	assert.Equal(t, "OLDAGENT", get(t, agent, "api_key"))

	chat, _, err := configsync.LoadFile(l.ChatConfig())
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:28080", get(t, chat, "contextmatrix_url"))
}

func TestMigrateRemovesOldUnits(t *testing.T) {
	h := newHarness(t, true)
	l := h.e.L

	writeOldLayout(t, l)

	unit := h.e.Services.UnitPath(repos.Agent)
	require.NoError(t, os.MkdirAll(filepath.Dir(unit), 0o755))
	require.NoError(t, os.WriteFile(unit, []byte("[Service]\nExecStart=/old/contextmatrix-agent serve --config ~/.config/contextmatrix-agent/serve.yaml\n"), 0o644))

	plan, err := migrate.Build(l, migrate.Detect(l), nil)
	require.NoError(t, err)
	require.NoError(t, h.e.Migrate(context.Background(), plan))

	calls := systemctlCalls(h)

	for _, name := range repos.Apps {
		assert.Contains(t, calls, "--user stop "+name)
		assert.Contains(t, calls, "--user disable "+name, "a stopped but enabled unit crash-loops at the next login")
	}

	assert.NoFileExists(t, unit, "the old unit points at a config the migration deletes")
}

func TestInstallAfterMigrationKeepsABoardsDirNamedBoards(t *testing.T) {
	h := newHarness(t, true)
	l := h.e.L

	write := func(p, s string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(s), 0o600))
	}

	write(l.OldServerConfig(), "port: 8080\ngithub:\n  auth_mode: pat\n  pat:\n    token: T\nboards:\n  dir: ~/work/boards\n  git_remote_url: https://example.test/team-boards.git\n")

	plan, err := migrate.Build(l, migrate.Detect(l), nil)
	require.NoError(t, err)
	require.NoError(t, h.e.Migrate(context.Background(), plan))

	// The wizard normalizes its answers, and "boards" counts as unset there;
	// the carried dir must still not be repointed.
	answers, _ := AnswersFrom(Trees{Server: plan.Server})
	answers.Normalize()

	require.NoError(t, h.e.Install(context.Background(), answers))

	server, _, err := configsync.LoadFile(l.ServerConfig())
	require.NoError(t, err)
	assert.Equal(t, "~/work/boards", get(t, server, "boards.dir"))
}
