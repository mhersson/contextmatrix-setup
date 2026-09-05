package engine

import (
	"fmt"
	"path/filepath"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
)

type Facts struct {
	Layout     layout.Layout
	Gateway    string
	Docker     bool
	Keys       Keys
	InstanceID string
	AgentImage string
	ChatImage  string
	GitHubKey  string
}

type Trees struct {
	Server configsync.Tree
	Agent  configsync.Tree
	Chat   configsync.Tree
}

// Opinionated returns the values the installer writes on a fresh install.
// Every value here loses to an existing user value in the merge, except
// base_image, which both flows force after a build.
func Opinionated(a Answers, f Facts) Trees {
	l := f.Layout
	serverURL := fmt.Sprintf("http://localhost:%d", a.ServerPort)
	containerURL := fmt.Sprintf("http://%s:%d", f.Gateway, a.ServerPort)

	s := configsync.Tree{}
	configsync.Set(s, "port", a.ServerPort)
	configsync.Set(s, "instance.id", f.InstanceID)
	configsync.Set(s, "workflow_skills_dir", l.WorkflowSkillsDir())
	configsync.Set(s, "mcp_api_key", f.Keys.MCP)
	configsync.Set(s, "llm_endpoint.type", "openrouter")
	configsync.Set(s, "llm_endpoint.api_key", a.OpenRouterKey)

	configsync.Set(s, "boards.dir", l.BoardsDir(a.BoardsName))

	if a.BoardsURL != "" {
		configsync.Set(s, "boards.git_remote_url", a.BoardsURL)
		configsync.Set(s, "boards.git_clone_on_empty", true)
		configsync.Set(s, "boards.git_auto_push", true)
	}

	configsync.Set(s, "task_skills.dir", l.TaskSkillsDir())

	if a.TaskSkillsURL != "" {
		configsync.Set(s, "task_skills.git_remote_url", a.TaskSkillsURL)
		configsync.Set(s, "task_skills.git_clone_on_empty", true)
	}

	configsync.Set(s, "backends.agent.url", fmt.Sprintf("http://localhost:%d", a.AgentPort))
	configsync.Set(s, "backends.agent.api_key", f.Keys.Agent)
	configsync.Set(s, "backends.agent.enabled", f.Docker)
	configsync.Set(s, "backends.agent.default_model", a.DefaultModel)
	configsync.Set(s, "backends.agent.aa_api_key", a.AAKey)
	configsync.Set(s, "backends.chat.url", fmt.Sprintf("http://localhost:%d", a.ChatPort))
	configsync.Set(s, "backends.chat.api_key", f.Keys.Chat)
	configsync.Set(s, "backends.chat.enabled", f.Docker)
	configsync.Set(s, "backends.chat.default_model", a.DefaultModel)

	configsync.Set(s, "auth.mode", a.AuthMode)
	configsync.Set(s, "auth.master_key_file", filepath.Join(l.ServerStateDir(), "master.key"))
	configsync.Set(s, "auth.db_path", filepath.Join(l.ServerStateDir(), "auth.db"))
	configsync.Set(s, "images.db_path", filepath.Join(l.ServerStateDir(), "images.db"))
	configsync.Set(s, "op_store.db_path", filepath.Join(l.ServerStateDir(), "ops.db"))

	switch a.GitHubMode {
	case "pat":
		configsync.Set(s, "github.auth_mode", "pat")
		configsync.Set(s, "github.pat.token", a.GitHubPAT)
	case "app":
		configsync.Set(s, "github.auth_mode", "app")
		configsync.Set(s, "github.app.app_id", a.GitHubAppID)
		configsync.Set(s, "github.app.installation_id", a.GitHubInstallID)
		configsync.Set(s, "github.app.private_key_path", f.GitHubKey)
	}

	ag := configsync.Tree{}
	configsync.Set(ag, "contextmatrix_url", serverURL)
	configsync.Set(ag, "container_contextmatrix_url", containerURL)
	configsync.Set(ag, "api_key", f.Keys.Agent)
	configsync.Set(ag, "mcp_api_key", f.Keys.MCP)
	configsync.Set(ag, "port", a.AgentPort)
	configsync.Set(ag, "secrets_dir", l.AgentSecretsDir())
	configsync.Set(ag, "log_dir", l.AgentLogsDir())
	configsync.Set(ag, "default_model", a.DefaultModel)

	if f.AgentImage != "" {
		configsync.Set(ag, "base_image", f.AgentImage)
	}

	ch := configsync.Tree{}
	configsync.Set(ch, "contextmatrix_url", serverURL)
	configsync.Set(ch, "container_contextmatrix_url", containerURL)
	configsync.Set(ch, "api_key", f.Keys.Chat)
	configsync.Set(ch, "port", a.ChatPort)
	configsync.Set(ch, "secrets_dir", l.ChatSecretsDir())
	configsync.Set(ch, "chat_run_dir", l.ChatSessionsDir())

	if f.ChatImage != "" {
		configsync.Set(ch, "base_image", f.ChatImage)
	}

	return Trees{Server: s, Agent: ag, Chat: ch}
}
