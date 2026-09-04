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
