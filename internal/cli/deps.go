package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mhersson/contextmatrix-setup/internal/engine"
	"github.com/mhersson/contextmatrix-setup/internal/host"
	"github.com/mhersson/contextmatrix-setup/internal/images"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/run"
	"github.com/mhersson/contextmatrix-setup/internal/services"
)

// newEngine wires the real implementations. Tests construct Engine directly.
func newEngine(ctx context.Context, out io.Writer) (*engine.Engine, error) {
	l, err := layout.FromEnv(os.Getenv)
	if err != nil {
		return nil, err
	}

	r := run.Exec{}

	info, err := host.Detect(ctx, r, runtime.GOOS, os.Getenv)
	if err != nil {
		return nil, err
	}

	eng := &engine.Engine{
		L:        l,
		Host:     info,
		R:        r,
		Git:      repos.Git{R: r},
		Images:   images.Docker{R: r},
		Services: services.New(info.ServiceManager, r, l, info.UID),
		Out:      out,
		Browser: func(ctx context.Context, url string) error {
			return host.OpenBrowser(ctx, r, runtime.GOOS, url)
		},
	}

	// Test-only: point the clones at local bare repos so the end-to-end
	// tests never reach GitHub.
	if base := os.Getenv("CONTEXTMATRIX_SETUP_REPO_BASE"); base != "" {
		eng.RepoURL = func(name string) string { return "file://" + filepath.Join(base, name+".git") }
	}

	return eng, nil
}
