// Package selfupdate keeps the installer itself current: it is one more
// repo in the cache, rebuilt with go install and re-executed when the
// binary changed.
package selfupdate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/mhersson/contextmatrix-setup/internal/engine"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/run"
	"github.com/mhersson/contextmatrix-setup/internal/state"
)

const EnvMarker = "CONTEXTMATRIX_SETUP_REEXEC"

type Options struct {
	L      layout.Layout
	R      run.Runner
	Git    engine.Git
	Out    io.Writer
	Getenv func(string) string
	Exec   func(argv0 string, argv, env []string) error
	Self   func() (string, error)
	Args   []string
	URL    string
}

func Run(ctx context.Context, o Options) error {
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}

	if o.Getenv(EnvMarker) != "" {
		return nil
	}

	if o.Exec == nil {
		o.Exec = syscall.Exec
	}

	if o.Self == nil {
		o.Self = os.Executable
	}

	if o.Args == nil {
		o.Args = os.Args
	}

	if o.URL == "" {
		o.URL = repos.URL(repos.Setup)
	}

	st, _, err := state.Load(o.L.StateFile())
	if err != nil {
		return err
	}

	dir := o.L.SrcDir(repos.Setup)

	head, err := o.Git.Sync(ctx, dir, o.URL, o.Out)
	if err != nil {
		fmt.Fprintf(o.Out, "self-update skipped: %v\n", err)

		return nil
	}

	if head == st.Repos[repos.Setup].Commit {
		return nil
	}

	exe, err := o.Self()
	if err != nil {
		return err
	}

	before, _ := hashFile(exe)

	fmt.Fprintf(o.Out, "%-22s rebuilding %s\n", repos.Setup, repos.Short(head))

	if err := o.R.Stream(ctx, run.Cmd{Name: "go", Args: []string{"install", "."}, Dir: dir}, o.Out); err != nil {
		fmt.Fprintf(o.Out, "self-update failed, continuing with the current binary: %v\n", err)

		return nil
	}

	st.Repos[repos.Setup] = state.Repo{Commit: head}
	if err := st.Save(o.L.StateFile()); err != nil {
		return err
	}

	after, _ := hashFile(exe)
	if after == before {
		return nil
	}

	fmt.Fprintf(o.Out, "%-22s restarting with the new binary\n", repos.Setup)

	env := append(os.Environ(), EnvMarker+"=1")

	return o.Exec(exe, o.Args, env)
}

func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(data)

	return fmt.Sprintf("%x", sum), nil
}
