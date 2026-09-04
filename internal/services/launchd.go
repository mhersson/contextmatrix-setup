package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/run"
)

type launchd struct {
	r   run.Runner
	l   layout.Layout
	uid int
}

func (launchd) Kind() string { return "launchd" }

func (m *launchd) UnitPath(name string) string {
	return filepath.Join(m.l.LaunchAgentsDir(), Label(name)+".plist")
}

func (m *launchd) domain() string { return "gui/" + strconv.Itoa(m.uid) }

func (m *launchd) target(name string) string { return m.domain() + "/" + Label(name) }

func (m *launchd) ctl(ctx context.Context, args ...string) error {
	res, err := m.r.Run(ctx, run.Cmd{Name: "launchctl", Args: args})
	if err != nil {
		return err
	}

	if res.ExitCode != 0 {
		return fmt.Errorf("launchctl %s: %s", strings.Join(args, " "), strings.TrimSpace(res.Stderr))
	}

	return nil
}

func (m *launchd) loaded(ctx context.Context, name string) bool {
	res, err := m.r.Run(ctx, run.Cmd{Name: "launchctl", Args: []string{"print", m.target(name)}})

	return err == nil && res.ExitCode == 0
}

// Install writes the plist. launchd has no separate enable step and a
// loaded RunAtLoad agent starts at once, so an unloaded label is left for
// Start to bootstrap; a loaded one is reloaded only when the text changed,
// which is the only way a running agent picks up a new plist. The log
// directory is derived from the layout rather than s.LogFile, since the
// manager owns that path (see logFile) and a caller-supplied LogFile string
// must not dictate what gets created on disk.
func (m *launchd) Install(ctx context.Context, s Service) (bool, error) {
	if s.LogFile != "" {
		if err := os.MkdirAll(m.l.MacLogsDir(), 0o755); err != nil {
			return false, err
		}
	}

	changed, err := writeUnit(m.UnitPath(s.Name), RenderLaunchd(s))
	if err != nil {
		return false, err
	}

	if changed && m.loaded(ctx, s.Name) {
		if err := m.ctl(ctx, "bootout", m.target(s.Name)); err != nil {
			return true, err
		}

		if err := m.ctl(ctx, "bootstrap", m.domain(), m.UnitPath(s.Name)); err != nil {
			return true, err
		}
	}

	return changed, nil
}

func (m *launchd) Start(ctx context.Context, name string) error {
	if !m.loaded(ctx, name) {
		return m.ctl(ctx, "bootstrap", m.domain(), m.UnitPath(name))
	}

	return m.ctl(ctx, "kickstart", m.target(name))
}

func (m *launchd) Stop(ctx context.Context, name string) error {
	if !m.loaded(ctx, name) {
		return nil
	}

	return m.ctl(ctx, "bootout", m.target(name))
}

func (m *launchd) Restart(ctx context.Context, name string) error {
	if !m.loaded(ctx, name) {
		return m.ctl(ctx, "bootstrap", m.domain(), m.UnitPath(name))
	}

	return m.ctl(ctx, "kickstart", "-k", m.target(name))
}

func (m *launchd) Active(ctx context.Context, name string) (bool, error) {
	res, err := m.r.Run(ctx, run.Cmd{Name: "launchctl", Args: []string{"print", m.target(name)}})
	if err != nil {
		return false, err
	}

	return res.ExitCode == 0 && strings.Contains(res.Stdout, "state = running"), nil
}

func (m *launchd) Remove(ctx context.Context, name string) error {
	_ = m.Stop(ctx, name)

	if err := os.Remove(m.UnitPath(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

func (m *launchd) Logs(ctx context.Context, name string, lines int) (string, error) {
	res, err := m.r.Run(ctx, run.Cmd{Name: "tail", Args: []string{"-n", strconv.Itoa(lines), m.logFile(name)}})
	if err != nil {
		return "", err
	}

	return res.Stdout, nil
}

func (m *launchd) Follow(ctx context.Context, name string, w io.Writer) error {
	return m.r.Stream(ctx, run.Cmd{Name: "tail", Args: []string{"-F", "-n", "0", m.logFile(name)}}, w)
}

func (m *launchd) logFile(name string) string {
	return filepath.Join(m.l.MacLogsDir(), name+".log")
}
