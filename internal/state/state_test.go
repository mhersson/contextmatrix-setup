package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMissingReturnsFresh(t *testing.T) {
	s, found, err := Load(filepath.Join(t.TempDir(), "setup", "state.yaml"))
	require.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, 1, s.Version)
	assert.NotNil(t, s.Repos)
	assert.False(t, s.Installed())
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup", "state.yaml")

	s := New()
	s.OS = "linux"
	s.ServiceManager = "systemd"
	s.Docker = true
	s.Repos["contextmatrix"] = Repo{Commit: "abc1234", InstalledAt: time.Date(2026, 9, 4, 7, 0, 0, 0, time.UTC)}
	s.Images["contextmatrix-agent-worker"] = Image{Tag: "contextmatrix-agent-worker:abc1234", ID: "sha256:deadbeef"}
	s.Configs["server.yaml"] = ConfigHash{SHA256: "00ff"}
	s.WorkflowSkills = WorkflowSkills{Commit: "abc1234", Files: map[string]string{"create-plan.md": "11aa"}}

	require.NoError(t, s.Save(path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	back, found, err := Load(path)
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, back.Installed())
	assert.Equal(t, s.Repos["contextmatrix"], back.Repos["contextmatrix"])
	assert.Equal(t, "sha256:deadbeef", back.Images["contextmatrix-agent-worker"].ID)
	assert.Equal(t, "11aa", back.WorkflowSkills.Files["create-plan.md"])
	assert.Nil(t, back.Migration)
}

func TestLoadRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: [oops"), 0o600))

	_, _, err := Load(path)
	require.Error(t, err)
}
