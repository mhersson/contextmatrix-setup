package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
)

func testFacts() Facts {
	return Facts{
		Layout:     layout.New("/home/u", nil),
		Gateway:    "172.17.0.1",
		Docker:     true,
		Keys:       Keys{MCP: "mcp", Agent: "agentkey", Chat: "chatkey"},
		InstanceID: "box-abc123",
		AgentImage: "contextmatrix-agent-worker:abc1234",
		ChatImage:  "contextmatrix-chat-worker:def5678",
	}
}

func get(t *testing.T, tr configsync.Tree, path string) any {
	t.Helper()

	v, ok := configsync.Get(tr, path)
	require.True(t, ok, path)

	return v
}

func TestOpinionatedServer(t *testing.T) {
	a := DefaultAnswers()
	a.AuthMode = "none"
	a.OpenRouterKey = "or-key"
	a.AAKey = "aa-key"
	a.BoardsURL = "https://github.com/org/team.git"
	a.TaskSkillsURL = "https://github.com/org/skills.git"
	a.GitHubMode = "pat"
	a.GitHubPAT = "ghp_x"
	a.Normalize()

	s := Opinionated(a, testFacts()).Server

	assert.Equal(t, 18080, get(t, s, "port"))
	assert.Equal(t, "box-abc123", get(t, s, "instance.id"))
	assert.Equal(t, "~/.contextmatrix/workflow-skills", get(t, s, "workflow_skills_dir"))
	assert.Equal(t, "mcp", get(t, s, "mcp_api_key"))
	assert.Equal(t, "openrouter", get(t, s, "llm_endpoint.type"))
	assert.Equal(t, "or-key", get(t, s, "llm_endpoint.api_key"))
	assert.Equal(t, "~/.contextmatrix/boards/team", get(t, s, "boards.dir"))
	assert.Equal(t, "https://github.com/org/team.git", get(t, s, "boards.git_remote_url"))
	assert.Equal(t, true, get(t, s, "boards.git_clone_on_empty"))
	assert.Equal(t, true, get(t, s, "boards.git_auto_push"))
	assert.Equal(t, "~/.contextmatrix/task-skills", get(t, s, "task_skills.dir"))
	assert.Equal(t, true, get(t, s, "task_skills.git_clone_on_empty"))
	assert.Equal(t, "http://localhost:19092", get(t, s, "backends.agent.url"))
	assert.Equal(t, "agentkey", get(t, s, "backends.agent.api_key"))
	assert.Equal(t, true, get(t, s, "backends.agent.enabled"))
	assert.Equal(t, DefaultModel, get(t, s, "backends.agent.default_model"))
	assert.Equal(t, "aa-key", get(t, s, "backends.agent.aa_api_key"))
	assert.Equal(t, "http://localhost:19093", get(t, s, "backends.chat.url"))
	assert.Equal(t, "chatkey", get(t, s, "backends.chat.api_key"))
	assert.Equal(t, DefaultModel, get(t, s, "backends.chat.default_model"))
	assert.Equal(t, "none", get(t, s, "auth.mode"))
	assert.Equal(t, "~/.contextmatrix/server/master.key", get(t, s, "auth.master_key_file"))
	assert.Equal(t, "~/.contextmatrix/server/auth.db", get(t, s, "auth.db_path"))
	assert.Equal(t, "~/.contextmatrix/server/images.db", get(t, s, "images.db_path"))
	assert.Equal(t, "~/.contextmatrix/server/ops.db", get(t, s, "op_store.db_path"))
	assert.Equal(t, "pat", get(t, s, "github.auth_mode"))
	assert.Equal(t, "ghp_x", get(t, s, "github.pat.token"))
}

func TestOpinionatedServerSkipsAndNoDocker(t *testing.T) {
	a := DefaultAnswers()
	f := testFacts()
	f.Docker = false
	f.AgentImage = ""
	f.ChatImage = ""

	tr := Opinionated(a, f)

	assert.Equal(t, "~/.contextmatrix/boards/boards", get(t, tr.Server, "boards.dir"))
	_, has := configsync.Get(tr.Server, "boards.git_remote_url")
	assert.False(t, has, "no remote written without a URL")
	_, has = configsync.Get(tr.Server, "boards.git_auto_push")
	assert.False(t, has)
	assert.Equal(t, "~/.contextmatrix/task-skills", get(t, tr.Server, "task_skills.dir"))
	_, has = configsync.Get(tr.Server, "task_skills.git_remote_url")
	assert.False(t, has)
	assert.Equal(t, false, get(t, tr.Server, "backends.agent.enabled"))
	assert.Equal(t, false, get(t, tr.Server, "backends.chat.enabled"))
	_, has = configsync.Get(tr.Server, "github.auth_mode")
	assert.False(t, has, "github skipped leaves the block to the schema")
	_, has = configsync.Get(tr.Agent, "base_image")
	assert.False(t, has)
}

func TestOpinionatedGitHubApp(t *testing.T) {
	a := DefaultAnswers()
	a.GitHubMode = "app"
	a.GitHubAppID = 12
	a.GitHubInstallID = 34
	a.GitHubKeyFile = "/tmp/key.pem"
	f := testFacts()
	f.GitHubKey = "/home/u/.contextmatrix/server/github-app.pem"

	s := Opinionated(a, f).Server
	assert.Equal(t, "app", get(t, s, "github.auth_mode"))
	assert.Equal(t, int64(12), get(t, s, "github.app.app_id"))
	assert.Equal(t, int64(34), get(t, s, "github.app.installation_id"))
	assert.Equal(t, "~/.contextmatrix/server/github-app.pem", get(t, s, "github.app.private_key_path"))
}

func TestOpinionatedBackends(t *testing.T) {
	tr := Opinionated(DefaultAnswers(), testFacts())

	assert.Equal(t, "http://localhost:18080", get(t, tr.Agent, "contextmatrix_url"))
	assert.Equal(t, "http://172.17.0.1:18080", get(t, tr.Agent, "container_contextmatrix_url"))
	assert.Equal(t, "agentkey", get(t, tr.Agent, "api_key"))
	assert.Equal(t, "mcp", get(t, tr.Agent, "mcp_api_key"))
	assert.Equal(t, 19092, get(t, tr.Agent, "port"))
	assert.Equal(t, "contextmatrix-agent-worker:abc1234", get(t, tr.Agent, "base_image"))
	assert.Equal(t, "~/.contextmatrix/agent/secrets", get(t, tr.Agent, "secrets_dir"))
	assert.Equal(t, "~/.contextmatrix/agent/logs", get(t, tr.Agent, "log_dir"))
	assert.Equal(t, DefaultModel, get(t, tr.Agent, "default_model"))
	_, has := configsync.Get(tr.Agent, "image_pull_policy")
	assert.False(t, has, "pull policy is never written")

	assert.Equal(t, "http://localhost:18080", get(t, tr.Chat, "contextmatrix_url"))
	assert.Equal(t, "http://172.17.0.1:18080", get(t, tr.Chat, "container_contextmatrix_url"))
	assert.Equal(t, "chatkey", get(t, tr.Chat, "api_key"))
	assert.Equal(t, 19093, get(t, tr.Chat, "port"))
	assert.Equal(t, "contextmatrix-chat-worker:def5678", get(t, tr.Chat, "base_image"))
	assert.Equal(t, "~/.contextmatrix/chat/secrets", get(t, tr.Chat, "secrets_dir"))
	assert.Equal(t, "~/.contextmatrix/chat/sessions", get(t, tr.Chat, "chat_run_dir"))
}
