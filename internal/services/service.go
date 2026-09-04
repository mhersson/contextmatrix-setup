// Package services writes and drives the per-user service units on Linux
// (systemd --user) and macOS (LaunchAgents).
package services

import (
	"context"
	"io"

	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/run"
)

const (
	Server = "contextmatrix"
	Agent  = "contextmatrix-agent"
	Chat   = "contextmatrix-chat"
)

var All = []string{Server, Agent, Chat}

type Service struct {
	Name           string
	Description    string
	Binary         string
	Args           []string
	Env            map[string]string
	WorkingDir     string
	ReadWritePaths []string
	LogFile        string
}

func Label(name string) string {
	return "com.github.mhersson." + name
}

type Manager interface {
	Kind() string
	UnitPath(name string) string
	Install(ctx context.Context, s Service) (bool, error)
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Active(ctx context.Context, name string) (bool, error)
	Remove(ctx context.Context, name string) error
	Logs(ctx context.Context, name string, lines int) (string, error)
	Follow(ctx context.Context, name string, w io.Writer) error
}

func New(kind string, r run.Runner, l layout.Layout, uid int) Manager {
	switch kind {
	case "systemd":
		return &systemd{r: r, l: l}
	case "launchd":
		return &launchd{r: r, l: l, uid: uid}
	default:
		return none{}
	}
}
