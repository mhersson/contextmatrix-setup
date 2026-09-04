package migrate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func oldLayout(t *testing.T) layout.Layout {
	t.Helper()

	l := layout.New(t.TempDir(), nil)
	write(t, l.OldServerConfig(), "port: 8080\nboards:\n  dir: ~/contextmatrix-boards\ntask_skills:\n  dir: ~/skills\ngithub:\n  auth_mode: pat\n  pat:\n    token: T\nmcp_api_key: MCP\n")
	write(t, l.OldAgentConfig(), "port: 9092\napi_key: AGENT\nsecrets_dir: /var/run/cm-agent/secrets\n")
	write(t, l.OldChatConfig(), "port: 9093\napi_key: CHAT\nchat_run_dir: /var/run/cm-chat/sessions\n")
	write(t, filepath.Join(l.OldWorkflowSkillsDir(), "create-plan.md"), "skill\n")
	write(t, filepath.Join(l.OldStateDir, "auth.db"), "db")
	write(t, filepath.Join(l.OldStateDir, "master.key"), "key")
	write(t, filepath.Join(l.OldStateDir, "instance_id"), "box-abc123\n")

	return l
}

func TestDetect(t *testing.T) {
	l := layout.New(t.TempDir(), nil)
	f := Detect(l)
	assert.False(t, f.Any())

	l = oldLayout(t)
	f = Detect(l)
	assert.True(t, f.Any())
	assert.Equal(t, l.OldServerConfig(), f.ServerConfig)
	assert.Equal(t, l.OldAgentConfig(), f.AgentConfig)
	assert.Equal(t, l.OldChatConfig(), f.ChatConfig)
	assert.Equal(t, l.OldWorkflowSkillsDir(), f.WorkflowSkills)
	assert.ElementsMatch(t, []string{
		filepath.Join(l.OldStateDir, "auth.db"), filepath.Join(l.OldStateDir, "master.key"), filepath.Join(l.OldStateDir, "instance_id"),
	}, f.StateFiles)
	assert.Len(t, f.Sources(), 7)
}

func TestBuildRewritesPathsAndListsMoves(t *testing.T) {
	l := oldLayout(t)
	p, err := Build(l, Detect(l), nil)
	require.NoError(t, err)

	v, _ := configsync.Get(p.Server, "instance.id")
	assert.Equal(t, "box-abc123", v)
	v, _ = configsync.Get(p.Server, "auth.db_path")
	assert.Equal(t, "~/.contextmatrix/server/auth.db", v)
	v, _ = configsync.Get(p.Server, "auth.master_key_file")
	assert.Equal(t, "~/.contextmatrix/server/master.key", v)
	v, _ = configsync.Get(p.Server, "workflow_skills_dir")
	assert.Equal(t, "~/.contextmatrix/workflow-skills", v)
	v, _ = configsync.Get(p.Server, "github.pat.token")
	assert.Equal(t, "T", v, "every other key carries over")
	v, _ = configsync.Get(p.Server, "boards.dir")
	assert.Equal(t, "~/contextmatrix-boards", v, "repos stay put by default")

	v, _ = configsync.Get(p.Agent, "secrets_dir")
	assert.Equal(t, "~/.contextmatrix/agent/secrets", v)
	v, _ = configsync.Get(p.Agent, "log_dir")
	assert.Equal(t, "~/.contextmatrix/agent/logs", v)
	v, _ = configsync.Get(p.Chat, "secrets_dir")
	assert.Equal(t, "~/.contextmatrix/chat/secrets", v)
	v, _ = configsync.Get(p.Chat, "chat_run_dir")
	assert.Equal(t, "~/.contextmatrix/chat/sessions", v)

	assert.Equal(t, []RepoDir{{Key: "boards", Path: "~/contextmatrix-boards"}, {Key: "task_skills", Path: "~/skills"}}, p.RepoDirs)

	to := map[string]string{}
	for _, m := range p.Moves {
		to[m.From] = m.To
	}

	// The three configs are rewritten in place and their old files deleted
	// last, so they are removals, never moves.
	assert.True(t, p.HasServer)
	assert.True(t, p.HasAgent)
	assert.True(t, p.HasChat)
	assert.Equal(t, []string{l.OldServerConfig(), l.OldAgentConfig(), l.OldChatConfig()}, p.Remove)

	for _, old := range []string{l.OldServerConfig(), l.OldAgentConfig(), l.OldChatConfig()} {
		_, moved := to[old]
		assert.False(t, moved, old)
	}

	assert.Equal(t, filepath.Join(l.ServerStateDir(), "auth.db"), to[filepath.Join(l.OldStateDir, "auth.db")])
	assert.Equal(t, l.WorkflowSkillsDir(), to[l.OldWorkflowSkillsDir()])
	_, movesBoards := to[filepath.Join(l.Home, "contextmatrix-boards")]
	assert.False(t, movesBoards)
}

func TestBuildMovesReposWhenAsked(t *testing.T) {
	l := oldLayout(t)
	p, err := Build(l, Detect(l), map[string]bool{"boards": true, "task_skills": true})
	require.NoError(t, err)

	v, _ := configsync.Get(p.Server, "boards.dir")
	assert.Equal(t, "~/.contextmatrix/boards/contextmatrix-boards", v)
	v, _ = configsync.Get(p.Server, "task_skills.dir")
	assert.Equal(t, "~/.contextmatrix/task-skills", v)

	to := map[string]string{}
	for _, m := range p.Moves {
		to[m.From] = m.To
	}

	assert.Equal(t, l.BoardsDir("contextmatrix-boards"), to[filepath.Join(l.Home, "contextmatrix-boards")])
	assert.Equal(t, l.TaskSkillsDir(), to[filepath.Join(l.Home, "skills")])
}

func TestBuildHandlesBoardsList(t *testing.T) {
	l := layout.New(t.TempDir(), nil)
	write(t, l.OldServerConfig(), "boards:\n  - name: a\n    dir: ~/a\n  - name: b\n    dir: ~/b\n")

	p, err := Build(l, Detect(l), map[string]bool{"boards[1]": true})
	require.NoError(t, err)
	assert.Equal(t, []RepoDir{{Key: "boards[0]", Path: "~/a"}, {Key: "boards[1]", Path: "~/b"}}, p.RepoDirs)

	list := p.Server["boards"].([]any)
	assert.Equal(t, "~/a", list[0].(map[string]any)["dir"])
	assert.Equal(t, "~/.contextmatrix/boards/b", list[1].(map[string]any)["dir"])
}

func TestBuildFlagsAConfigWithNoOldFile(t *testing.T) {
	l := layout.New(t.TempDir(), nil)
	write(t, l.OldAgentConfig(), "port: 9092\napi_key: AGENT\n")

	p, err := Build(l, Detect(l), nil)
	require.NoError(t, err)

	assert.False(t, p.HasServer)
	assert.True(t, p.HasAgent)
	assert.False(t, p.HasChat)
	assert.Equal(t, []string{l.OldAgentConfig()}, p.Remove)
}

func TestBuildReadsInstanceIDAfterItMoved(t *testing.T) {
	l := layout.New(t.TempDir(), nil)
	write(t, l.OldServerConfig(), "port: 8080\n")
	write(t, filepath.Join(l.ServerStateDir(), "instance_id"), "box-abc123\n")

	p, err := Build(l, Detect(l), nil)
	require.NoError(t, err)

	v, _ := configsync.Get(p.Server, "instance.id")
	assert.Equal(t, "box-abc123", v, "a rerun reads the id from wherever it now lives")

	for _, m := range p.Moves {
		assert.NotEqual(t, filepath.Join(l.OldStateDir, "instance_id"), m.From)
	}
}

func TestApplyMovesAndRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "old", "a.txt"), "A")
	write(t, filepath.Join(root, "old", "dir", "f"), "F")
	write(t, filepath.Join(root, "new", "taken"), "X")

	var out bytes.Buffer

	moves := []Move{
		{From: filepath.Join(root, "old", "a.txt"), To: filepath.Join(root, "new", "sub", "a.txt")},
		{From: filepath.Join(root, "old", "dir"), To: filepath.Join(root, "new", "dir")},
		{From: filepath.Join(root, "old", "missing"), To: filepath.Join(root, "new", "missing")},
	}
	require.NoError(t, Apply(moves, &out))

	data, err := os.ReadFile(filepath.Join(root, "new", "sub", "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "A", string(data))

	_, err = os.Stat(filepath.Join(root, "old", "a.txt"))
	assert.True(t, os.IsNotExist(err))

	_, err = os.Stat(filepath.Join(root, "new", "dir", "f"))
	require.NoError(t, err)
	assert.Contains(t, out.String(), "missing")

	err = Apply([]Move{{From: filepath.Join(root, "new", "sub", "a.txt"), To: filepath.Join(root, "new", "taken")}}, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}
