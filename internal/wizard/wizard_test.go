package wizard

import (
	"strings"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/engine"
	"github.com/mhersson/contextmatrix-setup/internal/host"
)

func TestStepsAskEverythingOnAFreshInstall(t *testing.T) {
	info := host.Info{OS: "linux", ServiceManager: "systemd"}

	assert.Equal(t, []string{"login", "ports", "inference", "github", "aa", "claude-code", "task-skills", "boards", "services"}, Steps(engine.Known{}, info))
}

func TestStepsSkipWhatTheOldConfigProvided(t *testing.T) {
	info := host.Info{OS: "linux", ServiceManager: "systemd"}
	known := engine.Known{AuthMode: true, Ports: true, OpenRouterKey: true, DefaultModel: true, GitHub: true, TaskSkills: true, Boards: true}

	assert.Equal(t, []string{"aa", "claude-code", "services"}, Steps(known, info))

	known.DefaultModel = false
	assert.Contains(t, Steps(known, info), "inference", "inference is asked while the key or the model is missing")

	known.DefaultModel = true
	known.OpenRouterKey = false
	assert.Contains(t, Steps(known, info), "inference")
}

func TestStepsNeverAskForServicesWithoutAManager(t *testing.T) {
	info := host.Info{OS: "linux", ServiceManager: "none"}

	assert.NotContains(t, Steps(engine.Known{}, info), "services")
}

func TestNoteShowsMarkupCharactersAsWritten(t *testing.T) {
	text := "$DBUS_SESSION_BUS_ADDRESS is *not* set; see `journalctl` or C:\\path"

	view := huh.NewNote().Description(note(text)).View()

	assert.Contains(t, view, text)
}

func TestSteps_SkipsKnownClaudeCodeToken(t *testing.T) {
	info := host.Info{OS: "linux", ServiceManager: "systemd"}

	assert.Contains(t, Steps(engine.Known{}, info), "claude-code")
	assert.NotContains(t, Steps(engine.Known{ClaudeCodeToken: true}, info), "claude-code")
}

func TestSummary_MasksClaudeCodeToken(t *testing.T) {
	a := engine.DefaultAnswers()
	a.ClaudeCodeToken = "sk-ant-oat01-secret"

	s := Summary(a)

	assert.NotContains(t, s, "sk-ant-oat01-secret")
	assert.Equal(t, strings.Count(Summary(engine.DefaultAnswers()), "****")+1, strings.Count(s, "****"),
		"the token shows as one more masked value")
}

func TestClaudeCodeTokenInput(t *testing.T) {
	assert.NoError(t, claudeCodeTokenInput(""), "the screen is optional")
	assert.NoError(t, claudeCodeTokenInput("  "), "blank counts as no token")
	assert.NoError(t, claudeCodeTokenInput(" sk-ant-oat01-abc "))
	require.Error(t, claudeCodeTokenInput("sk-ant two"))
	require.Error(t, claudeCodeTokenInput("sk-ant\nsecond"))
}
