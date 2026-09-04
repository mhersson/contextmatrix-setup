package layout

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFromEnvDefaults(t *testing.T) {
	l, err := FromEnv(env(map[string]string{"HOME": "/home/u"}))
	require.NoError(t, err)

	assert.Equal(t, "/home/u/.config/contextmatrix", l.ConfigDir)
	assert.Equal(t, "/home/u/.contextmatrix", l.StateDir)
	assert.Equal(t, "/home/u/.cache/contextmatrix-setup", l.CacheDir)
	assert.Equal(t, "/home/u/.local/state/contextmatrix", l.OldStateDir)

	assert.Equal(t, "/home/u/.config/contextmatrix/server.yaml", l.ServerConfig())
	assert.Equal(t, "/home/u/.config/contextmatrix/agent.yaml", l.ConfigFor("contextmatrix-agent"))
	assert.Equal(t, "/home/u/.contextmatrix/setup/state.yaml", l.StateFile())
	assert.Equal(t, "/home/u/.contextmatrix/boards/team", l.BoardsDir("team"))
	assert.Equal(t, "/home/u/.cache/contextmatrix-setup/src/contextmatrix", l.SrcDir("contextmatrix"))
	assert.Equal(t, "/home/u/.config/systemd/user", l.SystemdUserDir())
	assert.Equal(t, "/home/u/Library/LaunchAgents", l.LaunchAgentsDir())
	assert.Equal(t, "/home/u/.config/contextmatrix-agent/serve.yaml", l.OldAgentConfig())
}

func TestFromEnvHonoursXDG(t *testing.T) {
	l, err := FromEnv(env(map[string]string{
		"HOME": "/home/u", "XDG_CONFIG_HOME": "/cfg", "XDG_CACHE_HOME": "/cache", "XDG_STATE_HOME": "/st",
	}))
	require.NoError(t, err)

	assert.Equal(t, "/cfg/contextmatrix", l.ConfigDir)
	assert.Equal(t, "/cache/contextmatrix-setup", l.CacheDir)
	assert.Equal(t, "/st/contextmatrix", l.OldStateDir)
	assert.Equal(t, "/cfg/systemd/user", l.SystemdUserDir())
	assert.Equal(t, "/home/u/.contextmatrix", l.StateDir, "state dir is fixed")
}

func TestFromEnvRequiresHome(t *testing.T) {
	_, err := FromEnv(env(map[string]string{}))
	require.Error(t, err)
}

func TestRuntimeDirsAndTilde(t *testing.T) {
	l := New("/home/u", env(nil))

	dirs := l.RuntimeDirs()
	for _, want := range []string{
		"/home/u/.config/contextmatrix", "/home/u/.contextmatrix/setup", "/home/u/.contextmatrix/server",
		"/home/u/.contextmatrix/workflow-skills", "/home/u/.contextmatrix/task-skills", "/home/u/.contextmatrix/boards",
		"/home/u/.contextmatrix/agent/secrets", "/home/u/.contextmatrix/agent/logs",
		"/home/u/.contextmatrix/chat/secrets", "/home/u/.contextmatrix/chat/sessions",
	} {
		assert.Contains(t, dirs, want)
	}

	assert.Equal(t, "~/.contextmatrix/server", Tilde(l, "/home/u/.contextmatrix/server"))
	assert.Equal(t, "/opt/x", Tilde(l, "/opt/x"))
	assert.Equal(t, filepath.Join("/home/u", "Library", "Logs", "contextmatrix"), l.MacLogsDir())
}
