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

type systemd struct {
	r run.Runner
	l layout.Layout
}

func (systemd) Kind() string { return "systemd" }

func (m *systemd) UnitPath(name string) string {
	return filepath.Join(m.l.SystemdUserDir(), name+".service")
}

func (m *systemd) ctl(ctx context.Context, args ...string) error {
	res, err := m.r.Run(ctx, run.Cmd{Name: "systemctl", Args: append([]string{"--user"}, args...)})
	if err != nil {
		return err
	}

	if res.ExitCode != 0 {
		return fmt.Errorf("systemctl --user %s: %s", strings.Join(args, " "), strings.TrimSpace(res.Stderr))
	}

	return nil
}

// Install writes the unit without enabling it: Start and Restart enable, so
// a service setup holds back does not start at the next boot either.
func (m *systemd) Install(ctx context.Context, s Service) (bool, error) {
	changed, err := writeUnit(m.UnitPath(s.Name), RenderSystemd(s))
	if err != nil || !changed {
		return false, err
	}

	return true, m.ctl(ctx, "daemon-reload")
}

func (m *systemd) Start(ctx context.Context, name string) error {
	if err := m.ctl(ctx, "enable", name); err != nil {
		return err
	}

	return m.ctl(ctx, "start", name)
}

func (m *systemd) Stop(ctx context.Context, name string) error { return m.ctl(ctx, "stop", name) }

func (m *systemd) Restart(ctx context.Context, name string) error {
	if err := m.ctl(ctx, "enable", name); err != nil {
		return err
	}

	return m.ctl(ctx, "restart", name)
}

func (m *systemd) Disable(ctx context.Context, name string) error { return m.ctl(ctx, "disable", name) }

func (m *systemd) Active(ctx context.Context, name string) (bool, error) {
	res, err := m.r.Run(ctx, run.Cmd{Name: "systemctl", Args: []string{"--user", "is-active", name}})
	if err != nil {
		return false, err
	}

	return res.ExitCode == 0, nil
}

func (m *systemd) Remove(ctx context.Context, name string) error {
	_ = m.ctl(ctx, "stop", name)
	_ = m.ctl(ctx, "disable", name)

	if err := os.Remove(m.UnitPath(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return m.ctl(ctx, "daemon-reload")
}

func (m *systemd) Logs(ctx context.Context, name string, lines int) (string, error) {
	res, err := m.r.Run(ctx, run.Cmd{Name: "journalctl", Args: []string{"--user", "-u", name, "-n", strconv.Itoa(lines), "--no-pager", "-o", "cat"}})
	if err != nil {
		return "", err
	}

	return res.Stdout, nil
}

func (m *systemd) Follow(ctx context.Context, name string, w io.Writer) error {
	return m.r.Stream(ctx, run.Cmd{Name: "journalctl", Args: []string{"--user", "-u", name, "-f", "-n", "0", "-o", "cat"}}, w)
}

// writeUnit writes only when the rendered text differs; unit files are not
// secrets, so 0644 keeps them readable by systemd tooling and the user alike.
func writeUnit(path string, data []byte) (bool, error) {
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(data) {
		return false, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G301: the systemd user unit and LaunchAgents dir conventions
		return false, err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { //nolint:gosec // G306: units carry only HOME, PATH and DOCKER_HOST, no secret
		return false, err
	}

	return true, os.Rename(tmp, path)
}
