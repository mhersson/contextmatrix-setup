//go:build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallUpdateCycle(t *testing.T) {
	e := newEnv(t)

	out, err := e.run("install", "--yes", "--auth-mode", "multi", "--github-mode", "pat", "--github-pat", "tok", "--openrouter-key", "or")
	require.NoError(t, err, out)

	assert.Contains(t, out, "/auth/token/stub-token")
	assert.FileExists(t, filepath.Join(e.home, ".config", "contextmatrix", "server.yaml"))
	assert.FileExists(t, filepath.Join(e.home, ".config", "contextmatrix", "agent.yaml"))
	assert.FileExists(t, filepath.Join(e.home, ".config", "contextmatrix", "chat.yaml"))
	assert.FileExists(t, filepath.Join(e.home, ".contextmatrix", "setup", "state.yaml"))
	assert.FileExists(t, filepath.Join(e.home, ".contextmatrix", "workflow-skills", "create-plan.md"))
	assert.FileExists(t, filepath.Join(e.home, ".config", "systemd", "user", "contextmatrix.service"))
	assert.FileExists(t, filepath.Join(e.gobin, "contextmatrix-agent"))

	server := e.read(".config/contextmatrix/server.yaml")
	assert.Contains(t, server, "port: 18080")
	assert.Contains(t, server, "auth_mode: pat")
	assert.Contains(t, server, "enabled: true")

	log := e.stubLog()
	assert.Contains(t, log, "make install-frontend contextmatrix")
	assert.Contains(t, log, "make docker-worker contextmatrix-agent")
	assert.Contains(t, log, "make docker-worker contextmatrix-chat")
	assert.Equal(t, 3, strings.Count(log, "systemctl --user start "))
	assert.Contains(t, log, "xdg-open http://localhost:18080/auth/token/stub-token")

	// Nothing moved: quiet.
	require.NoError(t, os.Remove(e.log))

	out, err = e.run("update", "--yes")
	require.NoError(t, err, out)

	assert.Contains(t, out, "up to date")
	assert.NotContains(t, e.stubLog(), "make install")
	assert.NotContains(t, e.stubLog(), "restart")

	// Agent moved: rebuild binary and image, restart agent only.
	require.NoError(t, os.Remove(e.log))
	e.commit("contextmatrix-agent", "x.go", "package x\n")

	out, err = e.run("update", "--yes")
	require.NoError(t, err, out)

	log = e.stubLog()
	assert.Contains(t, log, "make install contextmatrix-agent")
	assert.NotContains(t, log, "make install contextmatrix-chat")
	assert.NotContains(t, log, "make install-frontend", "only the server has a frontend")
	assert.Contains(t, log, "make docker-worker contextmatrix-agent")
	assert.Contains(t, log, "systemctl --user restart contextmatrix-agent")
	assert.NotContains(t, log, "systemctl --user restart contextmatrix-chat")
	assert.Contains(t, log, "docker rmi contextmatrix-agent-worker:")

	// Manual edit survives; stale key dropped; server restarted. Replace the
	// existing key rather than append: yaml rejects duplicate keys.
	path := filepath.Join(e.home, ".config", "contextmatrix", "server.yaml")
	edited := strings.Replace(e.read(".config/contextmatrix/server.yaml"), "log_level: info", "log_level: debug", 1) + "obsolete_key: 1\n"
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o600))
	require.NoError(t, os.Remove(e.log))

	out, err = e.run("update", "--yes")
	require.NoError(t, err, out)

	assert.Contains(t, out, "dropped obsolete_key")
	assert.Contains(t, out, "edited by hand")
	assert.Contains(t, e.read(".config/contextmatrix/server.yaml"), "log_level: debug")
	assert.NotContains(t, e.read(".config/contextmatrix/server.yaml"), "obsolete_key")
	assert.Contains(t, e.stubLog(), "systemctl --user restart contextmatrix\n")

	// Invalid value of a real key: reported, not repaired, service not
	// restarted. The edit also makes the file count as hand-edited, so the
	// restart is held back by validation alone.
	edited = strings.Replace(e.read(".config/contextmatrix/server.yaml"), "log_level: debug", "log_level: invalid", 1)
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o600))
	require.NoError(t, os.Remove(e.log))

	out, err = e.run("update", "--yes")
	require.NoError(t, err, out)

	assert.Contains(t, out, "log_level must be")
	assert.Contains(t, e.read(".config/contextmatrix/server.yaml"), "log_level: invalid", "value kept, not repaired")
	assert.NotContains(t, e.stubLog(), "systemctl --user restart contextmatrix\n")

	// Status prints ports and commits.
	out, err = e.run("status")
	require.NoError(t, err, out)

	assert.Contains(t, out, "18080")
	assert.Contains(t, out, "contextmatrix-agent")
}

func TestWorkflowSkillsCopyOnlyWhenChanged(t *testing.T) {
	e := newEnv(t)

	out, err := e.run("install", "--yes", "--auth-mode", "none", "--github-mode", "pat", "--github-pat", "t")
	require.NoError(t, err, out)

	e.commit("contextmatrix", "docs/x.md", "doc\n")

	out, err = e.run("update", "--yes")
	require.NoError(t, err, out)

	assert.NotContains(t, out, fmt.Sprintf("%-22s updated", "workflow-skills"))

	e.commit("contextmatrix", "workflow-skills/create-plan.md", "v2\n")

	out, err = e.run("update", "--yes")
	require.NoError(t, err, out)

	assert.Equal(t, "v2\n", e.read(".contextmatrix/workflow-skills/create-plan.md"))
}

func TestInstallWithoutDocker(t *testing.T) {
	e := newEnv(t)
	e.writeStub("docker", "#!/bin/sh\necho 'Cannot connect to the Docker daemon' >&2\nexit 1\n")

	out, err := e.run("install", "--yes", "--auth-mode", "none", "--github-mode", "pat", "--github-pat", "t")
	require.NoError(t, err, out)

	assert.Contains(t, out, "docker not available")
	assert.Contains(t, e.read(".config/contextmatrix/server.yaml"), "enabled: false")
	assert.NotContains(t, e.stubLog(), "docker-worker")
	assert.Equal(t, 1, strings.Count(e.stubLog(), "systemctl --user start "), "only the server starts")
}

func TestMigrateFromOldLayout(t *testing.T) {
	e := newEnv(t)

	write := func(rel, content string) {
		p := filepath.Join(e.home, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}

	write(".config/contextmatrix/config.yaml", "port: 8080\nmcp_api_key: OLDMCP\ngithub:\n  auth_mode: pat\n  pat:\n    token: T\nboards:\n  dir: ~/contextmatrix-boards\n")
	write(".config/contextmatrix-agent/serve.yaml", "port: 9092\napi_key: OLDAGENTOLDAGENTOLDAGENTOLDAGENT\n")
	write(".config/contextmatrix-chat/serve.yaml", "port: 9093\napi_key: OLDCHATOLDCHATOLDCHATOLDCHATOLDCH\n")
	write(".local/state/contextmatrix/master.key", "K")
	write(".local/state/contextmatrix/instance_id", "oldbox-123456\n")
	write("contextmatrix-boards/.git/HEAD", "ref: refs/heads/main\n")

	out, err := e.run("install", "--yes", "--migrate", "--move-repos")
	require.NoError(t, err, out)

	assert.NoFileExists(t, filepath.Join(e.home, ".config", "contextmatrix", "config.yaml"))
	assert.NoFileExists(t, filepath.Join(e.home, ".config", "contextmatrix-agent", "serve.yaml"))
	assert.FileExists(t, filepath.Join(e.home, ".contextmatrix", "server", "master.key"))
	assert.FileExists(t, filepath.Join(e.home, ".contextmatrix", "boards", "contextmatrix-boards", ".git", "HEAD"))

	server := e.read(".config/contextmatrix/server.yaml")
	assert.Contains(t, server, "port: 8080")
	assert.Contains(t, server, "mcp_api_key: OLDMCP")
	assert.Contains(t, server, "id: oldbox-123456")
	assert.Contains(t, server, "dir: "+e.home+"/.contextmatrix/boards/contextmatrix-boards")
	assert.Contains(t, e.read(".config/contextmatrix/agent.yaml"), "api_key: OLDAGENTOLDAGENTOLDAGENTOLDAGENT")
	assert.Contains(t, e.read(".contextmatrix/setup/state.yaml"), "migration:")
}
