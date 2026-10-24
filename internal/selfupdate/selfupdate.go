// Package selfupdate keeps the installer itself current: it is one more
// repo in the cache, rebuilt with go install and re-executed when the
// binary changed.
package selfupdate

import (
	"bytes"
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
	"github.com/mhersson/contextmatrix-setup/internal/ui"
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
	UI     *ui.Console
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
	r := &reporter{out: o.Out, c: o.UI}

	head, err := o.Git.Sync(ctx, dir, o.URL, r.syncOut())
	if err != nil {
		r.skipped(err)

		return nil
	}

	old := st.Repos[repos.Setup].Commit
	if head == old {
		return nil
	}

	exe, err := o.Self()
	if err != nil {
		return err
	}

	before, _ := hashFile(exe)

	out := r.rebuilding(head)

	if err := o.R.Stream(ctx, run.Cmd{Name: "go", Args: []string{"install", "."}, Dir: dir}, out); err != nil {
		r.failed(err)

		return nil
	}

	st.Repos[repos.Setup] = state.Repo{Commit: head}
	if err := st.Save(o.L.StateFile()); err != nil {
		r.fail(err.Error())

		return err
	}

	after, _ := hashFile(exe)
	if after == before {
		r.done(old, head, false)

		return nil
	}

	r.done(old, head, true)

	env := append(os.Environ(), EnvMarker+"=1")

	return o.Exec(exe, o.Args, env)
}

// reporter prints the self-update as one console step when a console is
// set, and as plain lines on Out otherwise.
type reporter struct {
	out  io.Writer
	c    *ui.Console
	step *ui.Step
	sync bytes.Buffer
}

// syncOut holds back git's output on a console: a sync that changes nothing
// prints nothing, and one that fails replays it into a failed step.
func (r *reporter) syncOut() io.Writer {
	if r.c == nil {
		return r.out
	}

	return &r.sync
}

func (r *reporter) skipped(err error) {
	if r.c == nil {
		fmt.Fprintf(r.out, "self-update skipped: %v\n", err)

		return
	}

	r.step = r.c.Start("update", repos.Setup, "")
	_, _ = r.step.Writer().Write(r.sync.Bytes())
	r.fail("skipped: " + err.Error())
}

func (r *reporter) rebuilding(head string) io.Writer {
	if r.c == nil {
		fmt.Fprintf(r.out, "%-22s rebuilding %s\n", repos.Setup, repos.Short(head))

		return r.out
	}

	r.step = r.c.Start("update", repos.Setup, "go install")

	return r.step.Writer()
}

func (r *reporter) failed(err error) {
	if r.c == nil {
		fmt.Fprintf(r.out, "self-update failed, continuing with the current binary: %v\n", err)

		return
	}

	r.fail("failed, continuing with the current binary: " + err.Error())
}

func (r *reporter) fail(detail string) {
	if r.step != nil {
		r.step.Fail(detail)
	}
}

func (r *reporter) done(old, head string, restarting bool) {
	if r.c == nil {
		if restarting {
			fmt.Fprintf(r.out, "%-22s restarting with the new binary\n", repos.Setup)
		}

		return
	}

	from := repos.Short(old)
	if from == "" {
		from = "–"
	}

	detail := from + " → " + repos.Short(head)
	if restarting {
		detail += ", restarting"
	}

	r.step.Done(detail)
}

func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(data)

	return fmt.Sprintf("%x", sum), nil
}
