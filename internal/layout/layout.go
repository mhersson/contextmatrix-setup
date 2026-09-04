// Package layout is the single source of every path the installer touches.
package layout

import (
	"errors"
	"path/filepath"
	"strings"
)

const (
	appDir       = "contextmatrix"
	setupDir     = "contextmatrix-setup"
	stateDirName = ".contextmatrix"
)

type Layout struct {
	Home        string // absolute
	ConfigDir   string // $XDG_CONFIG_HOME/contextmatrix or ~/.config/contextmatrix
	StateDir    string // ~/.contextmatrix, fixed
	CacheDir    string // $XDG_CACHE_HOME/contextmatrix-setup or ~/.cache/contextmatrix-setup
	OldStateDir string // $XDG_STATE_HOME/contextmatrix or ~/.local/state/contextmatrix
	xdgConfig   string
}

// FromEnv builds a Layout from the process environment. HOME is required.
func FromEnv(getenv func(string) string) (Layout, error) {
	home := getenv("HOME")
	if home == "" {
		return Layout{}, errors.New("HOME is not set")
	}

	return New(home, getenv), nil
}

// New builds a Layout from an explicit home directory and env lookup.
func New(home string, getenv func(string) string) Layout {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	xdgConfig := getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		xdgConfig = filepath.Join(home, ".config")
	}

	xdgCache := getenv("XDG_CACHE_HOME")
	if xdgCache == "" {
		xdgCache = filepath.Join(home, ".cache")
	}

	xdgState := getenv("XDG_STATE_HOME")
	if xdgState == "" {
		xdgState = filepath.Join(home, ".local", "state")
	}

	return Layout{
		Home:        home,
		ConfigDir:   filepath.Join(xdgConfig, appDir),
		StateDir:    filepath.Join(home, stateDirName),
		CacheDir:    filepath.Join(xdgCache, setupDir),
		OldStateDir: filepath.Join(xdgState, appDir),
		xdgConfig:   xdgConfig,
	}
}

func (l Layout) ServerConfig() string { return filepath.Join(l.ConfigDir, "server.yaml") }
func (l Layout) AgentConfig() string  { return filepath.Join(l.ConfigDir, "agent.yaml") }
func (l Layout) ChatConfig() string   { return filepath.Join(l.ConfigDir, "chat.yaml") }

// ConfigFor maps a service name to its config file; unknown names yield "".
func (l Layout) ConfigFor(service string) string {
	switch service {
	case "contextmatrix":
		return l.ServerConfig()
	case "contextmatrix-agent":
		return l.AgentConfig()
	case "contextmatrix-chat":
		return l.ChatConfig()
	default:
		return ""
	}
}

func (l Layout) StateFile() string { return filepath.Join(l.StateDir, "setup", "state.yaml") }

func (l Layout) ServerStateDir() string { return filepath.Join(l.StateDir, "server") }

func (l Layout) WorkflowSkillsDir() string { return filepath.Join(l.StateDir, "workflow-skills") }

func (l Layout) TaskSkillsDir() string { return filepath.Join(l.StateDir, "task-skills") }

func (l Layout) BoardsRoot() string { return filepath.Join(l.StateDir, "boards") }

func (l Layout) BoardsDir(name string) string { return filepath.Join(l.BoardsRoot(), name) }

func (l Layout) AgentSecretsDir() string { return filepath.Join(l.StateDir, "agent", "secrets") }

func (l Layout) AgentLogsDir() string { return filepath.Join(l.StateDir, "agent", "logs") }

func (l Layout) ChatSecretsDir() string { return filepath.Join(l.StateDir, "chat", "secrets") }

func (l Layout) ChatSessionsDir() string { return filepath.Join(l.StateDir, "chat", "sessions") }

func (l Layout) SrcDir(repo string) string { return filepath.Join(l.CacheDir, "src", repo) }

func (l Layout) SystemdUserDir() string { return filepath.Join(l.xdgConfig, "systemd", "user") }

func (l Layout) LaunchAgentsDir() string { return filepath.Join(l.Home, "Library", "LaunchAgents") }

func (l Layout) MacLogsDir() string { return filepath.Join(l.Home, "Library", "Logs", appDir) }

func (l Layout) OldServerConfig() string { return filepath.Join(l.ConfigDir, "config.yaml") }

func (l Layout) OldWorkflowSkillsDir() string {
	return filepath.Join(l.ConfigDir, "workflow-skills")
}

func (l Layout) OldAgentConfig() string {
	return filepath.Join(l.xdgConfig, "contextmatrix-agent", "serve.yaml")
}

func (l Layout) OldChatConfig() string {
	return filepath.Join(l.xdgConfig, "contextmatrix-chat", "serve.yaml")
}

// RuntimeDirs lists every directory the installer creates before starting
// services. BoardsRoot is included; the named boards dir is created by the
// server itself on first start.
func (l Layout) RuntimeDirs() []string {
	return []string{
		l.ConfigDir,
		filepath.Join(l.StateDir, "setup"),
		l.ServerStateDir(),
		l.WorkflowSkillsDir(),
		l.TaskSkillsDir(),
		l.BoardsRoot(),
		l.AgentSecretsDir(),
		l.AgentLogsDir(),
		l.ChatSecretsDir(),
		l.ChatSessionsDir(),
	}
}

// Tilde shortens a path under Home to the ~ form used in config values and
// output; the apps expand ~ themselves.
func Tilde(l Layout, p string) string {
	if p == l.Home {
		return "~"
	}

	if strings.HasPrefix(p, l.Home+string(filepath.Separator)) {
		return "~" + p[len(l.Home):]
	}

	return p
}
