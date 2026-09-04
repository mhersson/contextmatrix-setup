package cli

import (
	"context"
	"os"
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
	}))

	assert.Equal(t, "none", a.AuthMode)
	assert.Equal(t, 28080, a.ServerPort)
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
		AuthMode: "multi", ServerPort: 8080, AgentPort: 9092, ChatPort: 9093,
		OpenRouterKey: "carried-key", DefaultModel: "carried/model",
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

// TestChainedInstallAcceptsNoArgs pins the argument handling of the install
// command run from inside migrate. Cobra falls back to os.Args when a command
// has no argument list of its own.
func TestChainedInstallAcceptsNoArgs(t *testing.T) {
	saved := os.Args

	t.Cleanup(func() { os.Args = saved })

	os.Args = []string{"contextmatrix-setup", "migrate"}

	sub := chainedInstall()
	sub.RunE = func(*cobra.Command, []string) error { return nil }

	require.NoError(t, sub.ExecuteContext(context.Background()))

	// Without the empty argument list the same command rejects "migrate".
	bare := newInstallCmd()
	bare.RunE = func(*cobra.Command, []string) error { return nil }
	bare.SilenceUsage, bare.SilenceErrors = true, true

	require.Error(t, bare.ExecuteContext(context.Background()))
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
