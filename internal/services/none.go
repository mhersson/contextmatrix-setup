package services

import (
	"context"
	"io"
)

// none is used when neither systemd --user nor launchctl is available. The
// engine prints the start commands instead.
type none struct{}

func (none) Kind() string { return "none" }

func (none) UnitPath(string) string { return "" }

func (none) Install(context.Context, Service) (bool, error) { return false, nil }

func (none) Start(context.Context, string) error { return nil }

func (none) Stop(context.Context, string) error { return nil }

func (none) Restart(context.Context, string) error { return nil }

func (none) Active(context.Context, string) (bool, error) { return false, nil }

func (none) Remove(context.Context, string) error { return nil }

func (none) Logs(context.Context, string, int) (string, error) { return "", nil }

func (none) Follow(ctx context.Context, _ string, _ io.Writer) error {
	<-ctx.Done()

	return nil
}
