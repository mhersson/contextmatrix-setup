//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/state"
)

func TestInstallUpdateCycle(t *testing.T) {
	e := newEnv(t)

	out, err := e.run("install", "--yes", "--auth-mode", "multi", "--github-mode", "pat", "--github-pat", "tok", "--openrouter-key", "or")
	require.NoError(t, err, out)

	assert.Contains(t, out, "/auth/token/stub-token")
	assert.FileExists(t, filepath.Join(e.home, ".config", "contextmatrix", "server.yaml"))
	assert.FileExists(t, filepath.Join(e.home, ".config", "contextmatrix", "agent.yaml"))
	assert.FileExists(t, filepath.Join(e.home, ".config", "contextmatrix", "chat.yaml"))
	assert.FileExists(t, filepath.Join(e.home, ".config", "contextmatrix", "architect.yaml"))
	assert.FileExists(t, filepath.Join(e.home, ".contextmatrix", "setup", "state.yaml"))
	assert.FileExists(t, filepath.Join(e.home, ".contextmatrix", "workflow-skills", "create-plan.md"))
	assert.FileExists(t, filepath.Join(e.home, ".config", "systemd", "user", "contextmatrix.service"))
	assert.FileExists(t, filepath.Join(e.gobin, "contextmatrix-agent"))
	assert.FileExists(t, filepath.Join(e.gobin, "contextmatrix-architect"))
	assert.FileExists(t, filepath.Join(e.home, ".config", "systemd", "user", "contextmatrix-architect.service"))

	server := e.read(".config/contextmatrix/server.yaml")
	assert.Contains(t, server, "port: 18080")
	assert.Contains(t, server, "auth_mode: pat")
	assert.Contains(t, server, "enabled: true")

	log := e.stubLog()
	assert.Contains(t, log, "make install-frontend contextmatrix")
	assert.Contains(t, log, "make docker-worker contextmatrix-agent")
	assert.Contains(t, log, "make docker-worker contextmatrix-chat")
	assert.Contains(t, log, "make docker-worker-variants contextmatrix-agent")
	assert.Contains(t, log, "make docker-worker-variants contextmatrix-chat")
	assert.Equal(t, 4, strings.Count(log, "systemctl --user start "))
	assert.NotContains(t, log, "make docker-worker contextmatrix-architect", "the architect has no image")
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
	assert.NotContains(t, log, "make install contextmatrix-architect")
	assert.NotContains(t, log, "make install-frontend", "only the server has a frontend")
	assert.Contains(t, log, "make docker-worker contextmatrix-agent")
	assert.Contains(t, log, "make docker-worker-variants contextmatrix-agent")
	assert.NotContains(t, log, "make docker-worker-variants contextmatrix-chat")
	assert.Contains(t, log, "systemctl --user restart contextmatrix-agent")
	assert.NotContains(t, log, "systemctl --user restart contextmatrix-chat")
	assert.Contains(t, out, "not checked", "the health budget env turns the probes off")
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

	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, out)
	assert.Equal(t, 1, exit.ExitCode(), "a config that blocks a restart is a problem")
	assert.NotContains(t, out, "error: update finished", "the final line already reports it")
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

	assert.Empty(t, skillsSteps(out), "unchanged skills are not copied")

	e.commit("contextmatrix", "workflow-skills/create-plan.md", "v2\n")

	out, err = e.run("update", "--yes")
	require.NoError(t, err, out)

	assert.Equal(t, "v2\n", e.read(".contextmatrix/workflow-skills/create-plan.md"))
	assert.Len(t, skillsSteps(out), 1, "changed skills are copied in one step")
}

// skillsSteps returns the plain step lines whose verb is skills.
func skillsSteps(out string) []string {
	var lines []string

	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) > 1 && (f[0] == "✓" || f[0] == "✗") && f[1] == "skills" {
			lines = append(lines, l)
		}
	}

	return lines
}

func TestInstallWithoutDocker(t *testing.T) {
	e := newEnv(t)
	e.writeStub("docker", "#!/bin/sh\necho 'Cannot connect to the Docker daemon' >&2\nexit 1\n")

	out, err := e.run("install", "--yes", "--auth-mode", "none", "--github-mode", "pat", "--github-pat", "t")
	require.NoError(t, err, out)

	assert.Contains(t, out, "docker not available")
	assert.Contains(t, e.read(".config/contextmatrix/server.yaml"), "enabled: false")
	assert.NotContains(t, e.stubLog(), "docker-worker")
	assert.Equal(t, 2, strings.Count(e.stubLog(), "systemctl --user start "), "the server and the architect start; the worker backends need docker")
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

func TestMigrateCommandCarriesValuesWithoutAsking(t *testing.T) {
	e := newEnv(t)

	write := func(rel, content string) {
		p := filepath.Join(e.home, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}

	write(".config/contextmatrix/config.yaml", "port: 8080\nmcp_api_key: OLDMCP\nllm_endpoint:\n  api_key: OR\ngithub:\n  auth_mode: pat\n  pat:\n    token: T\nboards:\n  dir: ~/contextmatrix-boards\n  git_remote_url: https://example.test/contextmatrix-boards.git\ntask_skills:\n  dir: ~/skills\n  git_remote_url: https://example.test/skills.git\n")
	write(".config/contextmatrix-agent/serve.yaml", "port: 9092\napi_key: OLDAGENTOLDAGENTOLDAGENTOLDAGENT\n")
	write(".config/contextmatrix-chat/serve.yaml", "port: 9093\napi_key: OLDCHATOLDCHATOLDCHATOLDCHATOLDCH\n")
	write(".local/state/contextmatrix/master.key", "K")
	write("contextmatrix-boards/.git/HEAD", "ref: refs/heads/main\n")
	write("skills/.git/HEAD", "ref: refs/heads/main\n")

	out, err := e.run("migrate", "--yes")
	require.NoError(t, err, out)

	// The old values win over the installer's defaults: nothing is asked and
	// nothing is forced.
	server := e.read(".config/contextmatrix/server.yaml")
	assert.Contains(t, server, "port: 8080")
	assert.Contains(t, server, "api_key: OR")
	assert.Contains(t, server, "token: T")
	assert.Contains(t, server, "dir: ~/contextmatrix-boards")
	assert.Contains(t, server, "git_remote_url: https://example.test/contextmatrix-boards.git")
	assert.Contains(t, server, "dir: ~/skills")
	assert.Contains(t, server, "git_remote_url: https://example.test/skills.git")
	assert.Contains(t, e.read(".config/contextmatrix/agent.yaml"), "port: 9092")
	assert.Contains(t, e.read(".config/contextmatrix/agent.yaml"), "contextmatrix_url: http://localhost:8080")
}

// An install made before setup knew the architect, whose morning update
// already wrote CM's empty architect block, gets the architect at the next
// update: built, configured on both ends, its unit written and started.
func TestUpdateAddsTheArchitectToAnOlderInstall(t *testing.T) {
	e := newEnv(t)

	out, err := e.run("install", "--yes", "--auth-mode", "none", "--github-mode", "pat", "--github-pat", "t")
	require.NoError(t, err, out)

	stateFile := filepath.Join(e.home, ".contextmatrix", "setup", "state.yaml")
	serverFile := filepath.Join(e.home, ".config", "contextmatrix", "server.yaml")

	st, _, err := state.Load(stateFile)
	require.NoError(t, err)
	delete(st.Repos, "contextmatrix-architect")
	delete(st.Configs, "architect.yaml")
	require.NoError(t, st.Save(stateFile))

	require.NoError(t, os.Remove(filepath.Join(e.home, ".config", "contextmatrix", "architect.yaml")))
	require.NoError(t, os.Remove(filepath.Join(e.home, ".config", "systemd", "user", "contextmatrix-architect.service")))

	server, _, err := configsync.LoadFile(serverFile)
	require.NoError(t, err)
	configsync.Set(server, "backends.architect", configsync.Tree{"url": "", "api_key": "", "enabled": false, "default_model": ""})

	data, err := configsync.Encode(server, "test")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(serverFile, data, 0o600))
	require.NoError(t, os.Remove(e.log))

	out, err = e.run("update", "--yes")
	require.NoError(t, err, out)

	server, _, err = configsync.LoadFile(serverFile)
	require.NoError(t, err)

	architect, found, err := configsync.LoadFile(filepath.Join(e.home, ".config", "contextmatrix", "architect.yaml"))
	require.NoError(t, err)
	require.True(t, found)

	url, _ := configsync.Get(server, "backends.architect.url")
	enabled, _ := configsync.Get(server, "backends.architect.enabled")
	serverKey, _ := configsync.Get(server, "backends.architect.api_key")
	architectKey, _ := configsync.Get(architect, "api_key")

	assert.Equal(t, "http://localhost:19094", url)
	assert.Equal(t, true, enabled)
	assert.Equal(t, architectKey, serverKey)
	assert.Len(t, architectKey, 64)

	log := e.stubLog()
	assert.Contains(t, log, "make install contextmatrix-architect")
	assert.Contains(t, log, "systemctl --user restart contextmatrix-architect")
	assert.Contains(t, log, "systemctl --user restart contextmatrix\n", "the server rereads its backends")
	assert.NotContains(t, log, "make install contextmatrix-agent", "nothing else moved")
	assert.FileExists(t, filepath.Join(e.home, ".config", "systemd", "user", "contextmatrix-architect.service"))

	st, _, err = state.Load(stateFile)
	require.NoError(t, err)
	assert.NotEmpty(t, st.Repos["contextmatrix-architect"].Commit)
}

func TestUpdatePlainOutputHasNoEscapes(t *testing.T) {
	e := newEnv(t)

	out, err := e.run("install", "--yes", "--auth-mode", "multi", "--github-mode", "pat", "--github-pat", "tok", "--openrouter-key", "or")
	require.NoError(t, err, out)

	e.commit("contextmatrix-agent", "x.go", "package x\n")

	out, err = e.runWith([]string{"NO_COLOR=1", "CONTEXTMATRIX_SETUP_REEXEC="}, "update", "--yes", "--no-self-update")
	require.NoError(t, err, out)

	assert.NotContains(t, out, "\x1b")
	assert.Contains(t, out, "change x.go", "the changes view lists the new commit")
	assert.Contains(t, out, "fetch")
	assert.Contains(t, out, "restart")
	assert.Equal(t, 1, strings.Count(out, "ContextMatrix setup"), "one header")
}
