package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultAnswers(t *testing.T) {
	a := DefaultAnswers()
	assert.Equal(t, "multi", a.AuthMode)
	assert.Equal(t, 18080, a.ServerPort)
	assert.Equal(t, 19092, a.AgentPort)
	assert.Equal(t, 19093, a.ChatPort)
	assert.Equal(t, 19094, a.ArchitectPort)
	assert.Equal(t, DefaultModel, a.DefaultModel)
	assert.Equal(t, "anthropic/claude-sonnet-5.5", a.ArchitectModel)
	assert.Equal(t, "skip", a.GitHubMode)
	assert.Equal(t, "boards", a.BoardsName)
	assert.True(t, a.Services)
	require.NoError(t, a.Validate())
	assert.False(t, a.GitHubConfigured())
}

func TestRepoName(t *testing.T) {
	assert.Equal(t, "team-boards", RepoName("https://github.com/org/team-boards.git"))
	assert.Equal(t, "team-boards", RepoName("git@github.com:org/team-boards"))
	assert.Empty(t, RepoName(""))
}

func TestNormalizeDerivesBoardsName(t *testing.T) {
	a := DefaultAnswers()
	a.BoardsURL = "https://github.com/org/my-boards.git"
	a.BoardsName = ""
	a.Normalize()
	assert.Equal(t, "my-boards", a.BoardsName)

	a.BoardsURL = ""
	a.BoardsName = ""
	a.Normalize()
	assert.Equal(t, "boards", a.BoardsName)

	a.BoardsName = "boards"
	a.BoardsURL = "https://github.com/org/my-boards.git"
	a.Normalize()
	assert.Equal(t, "my-boards", a.BoardsName)
}

func TestNormalizeFillsTheArchitectModel(t *testing.T) {
	a := DefaultAnswers()
	a.ArchitectModel = ""
	a.Normalize()
	assert.Equal(t, DefaultArchitectModel, a.ArchitectModel)

	a.ArchitectModel = "my/model"
	a.Normalize()
	assert.Equal(t, "my/model", a.ArchitectModel)
}

func TestValidateRejectsBadInput(t *testing.T) {
	cases := map[string]func(*Answers){
		"auth mode":       func(a *Answers) { a.AuthMode = "single" },
		"port collision":  func(a *Answers) { a.AgentPort = a.ServerPort },
		"port too low":    func(a *Answers) { a.ChatPort = 80 },
		"architect port":  func(a *Answers) { a.ArchitectPort = a.ChatPort },
		"architect low":   func(a *Answers) { a.ArchitectPort = 443 },
		"pat without tok": func(a *Answers) { a.GitHubMode = "pat" },
		"app without ids": func(a *Answers) { a.GitHubMode = "app"; a.GitHubKeyFile = "/k.pem" },
		"github mode":     func(a *Answers) { a.GitHubMode = "oauth" },
		"boards name":     func(a *Answers) { a.BoardsName = "../x" },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			a := DefaultAnswers()
			mutate(&a)
			require.Error(t, a.Validate())
		})
	}

	a := DefaultAnswers()
	a.GitHubMode = "pat"
	a.GitHubPAT = "ghp_x"
	require.NoError(t, a.Validate())
	assert.True(t, a.GitHubConfigured())
}

func TestKeysAndInstanceID(t *testing.T) {
	k, err := NewKeys()
	require.NoError(t, err)
	assert.Len(t, k.MCP, 64)
	assert.Len(t, k.Agent, 64)
	assert.Len(t, k.Chat, 64)
	assert.Len(t, k.Architect, 64)
	assert.NotEqual(t, k.MCP, k.Agent)
	assert.NotEqual(t, k.Chat, k.Architect)

	id, err := NewInstanceID("box")
	require.NoError(t, err)
	assert.Regexp(t, `^box-[0-9a-f]{6}$`, id)
}
