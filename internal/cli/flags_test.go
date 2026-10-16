package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/engine"
)

func TestInstallFlagsMapToAnswers(t *testing.T) {
	a := engine.DefaultAnswers()

	var yes, doMigrate, moveRepos bool

	cmd := &cobra.Command{Use: "install"}
	installFlags(cmd, &a, &yes, &doMigrate, &moveRepos)

	require.NoError(t, cmd.ParseFlags([]string{
		"--auth-mode", "none", "--server-port", "28080", "--github-mode", "pat", "--github-pat", "tok",
		"--boards-url", "https://github.com/o/b.git", "--no-services", "--yes", "--migrate",
		"--architect-port", "29094", "--architect-model", "x/architect",
	}))

	assert.Equal(t, "none", a.AuthMode)
	assert.Equal(t, 28080, a.ServerPort)
	assert.Equal(t, 29094, a.ArchitectPort)
	assert.Equal(t, "x/architect", a.ArchitectModel)
	assert.Equal(t, "pat", a.GitHubMode)
	assert.Equal(t, "tok", a.GitHubPAT)
	assert.Equal(t, "https://github.com/o/b.git", a.BoardsURL)
	assert.False(t, a.Services)
	assert.True(t, yes)
	assert.True(t, doMigrate)
	assert.False(t, moveRepos)
}

func TestOverlayChangedFlags(t *testing.T) {
	a := engine.DefaultAnswers()

	var yes, doMigrate, moveRepos bool

	cmd := &cobra.Command{Use: "install"}
	installFlags(cmd, &a, &yes, &doMigrate, &moveRepos)

	require.NoError(t, cmd.ParseFlags([]string{"--server-port", "28080", "--auth-mode", "none"}))

	prefill := engine.Answers{
		AuthMode: "multi", ServerPort: 8080, AgentPort: 9092, ChatPort: 9093, ArchitectPort: 9095,
		OpenRouterKey: "carried-key", DefaultModel: "carried/model", ArchitectModel: "carried/architect",
		GitHubMode: "app", GitHubPAT: "carried-pat", GitHubAppID: 7, GitHubInstallID: 8,
		GitHubKeyFile: "/carried/key.pem", AAKey: "carried-aa",
		TaskSkillsURL: "https://example.test/skills.git", BoardsURL: "https://example.test/boards.git",
		BoardsName: "carried-boards",
	}

	overlayChangedFlags(cmd.Flags(), &prefill, a)

	// Only the two flags actually given win over the migrated values.
	assert.Equal(t, "none", prefill.AuthMode)
	assert.Equal(t, 28080, prefill.ServerPort)

	assert.Equal(t, 9092, prefill.AgentPort)
	assert.Equal(t, 9093, prefill.ChatPort)
	assert.Equal(t, 9095, prefill.ArchitectPort)
	assert.Equal(t, "carried/architect", prefill.ArchitectModel)
	assert.Equal(t, "carried-key", prefill.OpenRouterKey)
	assert.Equal(t, "carried/model", prefill.DefaultModel)
	assert.Equal(t, "app", prefill.GitHubMode)
	assert.Equal(t, "carried-pat", prefill.GitHubPAT)
	assert.Equal(t, int64(7), prefill.GitHubAppID)
	assert.Equal(t, int64(8), prefill.GitHubInstallID)
	assert.Equal(t, "/carried/key.pem", prefill.GitHubKeyFile)
	assert.Equal(t, "carried-aa", prefill.AAKey)
	assert.Equal(t, "https://example.test/skills.git", prefill.TaskSkillsURL)
	assert.Equal(t, "https://example.test/boards.git", prefill.BoardsURL)
	assert.Equal(t, "carried-boards", prefill.BoardsName)
}

func TestRootHasRealCommands(t *testing.T) {
	root := NewRootCmd()

	for _, name := range []string{"install", "update", "status", "migrate", "uninstall"} {
		sub, _, err := root.Find([]string{name})
		require.NoError(t, err)
		assert.NotNil(t, sub.RunE, name)
	}

	update, _, _ := root.Find([]string{"update"})
	assert.NotNil(t, update.Flags().Lookup("yes"))
	assert.NotNil(t, update.Flags().Lookup("no-self-update"))
}
