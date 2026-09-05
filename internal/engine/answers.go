// Package engine turns wizard answers and host facts into installed files,
// built binaries, images and running services.
package engine

import (
	"errors"
	"fmt"
	"strings"
)

const DefaultModel = "deepseek/deepseek-v4-flash"

type Answers struct {
	AuthMode        string
	ServerPort      int
	AgentPort       int
	ChatPort        int
	OpenRouterKey   string
	DefaultModel    string
	GitHubMode      string
	GitHubPAT       string
	GitHubAppID     int64
	GitHubInstallID int64
	GitHubKeyFile   string
	AAKey           string
	TaskSkillsURL   string
	BoardsURL       string
	BoardsName      string
	Services        bool
	Linger          bool
}

// Known records which answers a config tree actually held. A wizard run
// after a migration asks only for what the old install never had.
type Known struct {
	AuthMode      bool
	Ports         bool
	OpenRouterKey bool
	DefaultModel  bool
	GitHub        bool
	AAKey         bool
	TaskSkills    bool
	Boards        bool
}

func DefaultAnswers() Answers {
	return Answers{
		AuthMode:     "multi",
		ServerPort:   18080,
		AgentPort:    19092,
		ChatPort:     19093,
		DefaultModel: DefaultModel,
		GitHubMode:   "skip",
		BoardsName:   "boards",
		Services:     true,
	}
}

// RepoName returns the last path segment of a git URL without .git, for
// both https and scp-style URLs.
func RepoName(url string) string {
	url = strings.TrimSuffix(strings.TrimSpace(url), "/")
	if url == "" {
		return ""
	}

	if i := strings.LastIndexAny(url, "/:"); i >= 0 {
		url = url[i+1:]
	}

	return strings.TrimSuffix(url, ".git")
}

func (a *Answers) Normalize() {
	// The wizard prefills BoardsName with "boards" before a URL is known, so
	// that value counts as unset once a boards URL is given.
	if a.BoardsURL != "" && (a.BoardsName == "" || a.BoardsName == "boards") {
		a.BoardsName = RepoName(a.BoardsURL)
	}

	if a.BoardsName == "" {
		a.BoardsName = "boards"
	}

	if a.DefaultModel == "" {
		a.DefaultModel = DefaultModel
	}
}

func (a Answers) Validate() error {
	if a.AuthMode != "multi" && a.AuthMode != "none" {
		return fmt.Errorf("auth mode must be multi or none, got %q", a.AuthMode)
	}

	ports := map[int]string{}

	for name, p := range map[string]int{"server": a.ServerPort, "agent": a.AgentPort, "chat": a.ChatPort} {
		if p < 1024 || p > 65535 {
			return fmt.Errorf("%s port %d must be between 1024 and 65535", name, p)
		}

		if other, dup := ports[p]; dup {
			return fmt.Errorf("%s and %s ports are both %d", other, name, p)
		}

		ports[p] = name
	}

	switch a.GitHubMode {
	case "skip":
	case "pat":
		if !a.githubComplete() {
			return errors.New("github pat mode needs a token")
		}
	case "app":
		if !a.githubComplete() {
			return errors.New("github app mode needs app id, installation id and a private key file")
		}
	default:
		return fmt.Errorf("github mode must be pat, app or skip, got %q", a.GitHubMode)
	}

	if a.BoardsName == "" || strings.ContainsAny(a.BoardsName, "/\\") || strings.HasPrefix(a.BoardsName, ".") {
		return fmt.Errorf("boards name %q must be a plain directory name", a.BoardsName)
	}

	return nil
}

func (a Answers) GitHubConfigured() bool {
	return a.GitHubMode == "pat" || a.GitHubMode == "app"
}

// githubComplete reports whether the chosen mode has every credential it
// needs.
func (a Answers) githubComplete() bool {
	switch a.GitHubMode {
	case "pat":
		return a.GitHubPAT != ""
	case "app":
		return a.GitHubAppID != 0 && a.GitHubInstallID != 0 && a.GitHubKeyFile != ""
	default:
		return false
	}
}
