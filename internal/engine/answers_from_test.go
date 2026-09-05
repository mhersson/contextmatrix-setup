package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
)

func parseTree(t *testing.T, yaml string) configsync.Tree {
	t.Helper()

	tr, err := configsync.Parse([]byte(yaml))
	require.NoError(t, err)

	return tr
}

func TestAnswersFromReadsEveryCarriedValue(t *testing.T) {
	server := parseTree(t, `
port: 8080
auth:
  mode: none
llm_endpoint:
  api_key: or-key
backends:
  agent:
    default_model: old/model
    aa_api_key: aa-key
github:
  auth_mode: app
  app:
    app_id: 12
    installation_id: 34
    private_key_path: ~/.config/contextmatrix/app.pem
task_skills:
  dir: ~/skills
  git_remote_url: https://example.test/skills.git
boards:
  dir: ~/team-boards
  git_remote_url: https://example.test/team-boards.git
`)

	a, known := AnswersFrom(Trees{Server: server, Agent: parseTree(t, "port: 9092\n"), Chat: parseTree(t, "port: 9093\n")})

	assert.Equal(t, "none", a.AuthMode)
	assert.Equal(t, 8080, a.ServerPort)
	assert.Equal(t, 9092, a.AgentPort)
	assert.Equal(t, 9093, a.ChatPort)
	assert.Equal(t, "or-key", a.OpenRouterKey)
	assert.Equal(t, "old/model", a.DefaultModel)
	assert.Equal(t, "app", a.GitHubMode)
	assert.Equal(t, int64(12), a.GitHubAppID)
	assert.Equal(t, int64(34), a.GitHubInstallID)
	assert.Equal(t, "~/.config/contextmatrix/app.pem", a.GitHubKeyFile)
	assert.Equal(t, "aa-key", a.AAKey)
	assert.Equal(t, "https://example.test/skills.git", a.TaskSkillsURL)
	assert.Equal(t, "https://example.test/team-boards.git", a.BoardsURL)
	assert.Equal(t, "team-boards", a.BoardsName)

	assert.Equal(t, Known{
		AuthMode: true, Ports: true, OpenRouterKey: true, DefaultModel: true,
		GitHub: true, AAKey: true, TaskSkills: true, Boards: true,
	}, known)
}

func TestAnswersFromReadsPATMode(t *testing.T) {
	server := parseTree(t, "github:\n  auth_mode: pat\n  pat:\n    token: ghp_old\n")

	a, known := AnswersFrom(Trees{Server: server})

	assert.Equal(t, "pat", a.GitHubMode)
	assert.Equal(t, "ghp_old", a.GitHubPAT)
	assert.True(t, known.GitHub)
}

func TestAnswersFromTreatsIncompleteGitHubAsUnknown(t *testing.T) {
	server := parseTree(t, "github:\n  auth_mode: pat\n")

	a, known := AnswersFrom(Trees{Server: server})

	assert.Equal(t, "pat", a.GitHubMode, "the mode is still prefilled")
	assert.False(t, known.GitHub, "a mode without its credentials must be asked for")

	server = parseTree(t, "github:\n  auth_mode: app\n  app:\n    app_id: 12\n")
	_, known = AnswersFrom(Trees{Server: server})
	assert.False(t, known.GitHub)
}

func TestAnswersFromEmptyTreesGivesDefaultsAndNothingKnown(t *testing.T) {
	a, known := AnswersFrom(Trees{})

	assert.Equal(t, DefaultAnswers(), a)
	assert.Equal(t, Known{}, known)
}

func TestAnswersFromKnowsPortsOnlyWhenAllThreeAreSet(t *testing.T) {
	_, known := AnswersFrom(Trees{Server: parseTree(t, "port: 8080\n"), Agent: parseTree(t, "port: 9092\n")})
	assert.False(t, known.Ports)

	_, known = AnswersFrom(Trees{Server: parseTree(t, "port: 8080\n"), Agent: parseTree(t, "port: 9092\n"), Chat: parseTree(t, "port: 9093\n")})
	assert.True(t, known.Ports)
}

func TestAnswersFromKnowsReposWithoutARemote(t *testing.T) {
	server := parseTree(t, "boards:\n  dir: ~/local-boards\ntask_skills:\n  dir: ~/skills\n")

	a, known := AnswersFrom(Trees{Server: server})

	assert.Empty(t, a.BoardsURL)
	assert.Equal(t, "local-boards", a.BoardsName)
	assert.True(t, known.Boards, "a local checkout is a configured board")
	assert.True(t, known.TaskSkills)
}

func TestAnswersFromReadsTheFirstOfABoardsList(t *testing.T) {
	server := parseTree(t, "boards:\n  - name: a\n    dir: ~/a\n    git_remote_url: https://example.test/a.git\n  - name: b\n    dir: ~/b\n")

	a, known := AnswersFrom(Trees{Server: server})

	assert.Equal(t, "https://example.test/a.git", a.BoardsURL)
	assert.Equal(t, "a", a.BoardsName)
	assert.True(t, known.Boards)
}
