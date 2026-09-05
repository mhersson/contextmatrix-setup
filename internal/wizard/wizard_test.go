package wizard

import (
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix-setup/internal/engine"
	"github.com/mhersson/contextmatrix-setup/internal/host"
)

func TestStepsAskEverythingOnAFreshInstall(t *testing.T) {
	info := host.Info{OS: "linux", ServiceManager: "systemd"}

	assert.Equal(t, []string{"login", "ports", "inference", "github", "aa", "task-skills", "boards", "services"}, Steps(engine.Known{}, info))
}

func TestStepsSkipWhatTheOldConfigProvided(t *testing.T) {
	info := host.Info{OS: "linux", ServiceManager: "systemd"}
	known := engine.Known{AuthMode: true, Ports: true, OpenRouterKey: true, DefaultModel: true, GitHub: true, TaskSkills: true, Boards: true}

	assert.Equal(t, []string{"aa", "services"}, Steps(known, info))

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
