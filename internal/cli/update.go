package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/mhersson/contextmatrix-setup/internal/engine"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/selfupdate"
	"github.com/mhersson/contextmatrix-setup/internal/ui"
)

func newUpdateCmd() *cobra.Command {
	var yes, noSelf bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Pull, rebuild, sync config, rebuild images, restart what changed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			e, err := newEngine(ctx, cmd.OutOrStdout())
			if err != nil {
				return err
			}

			if err := healthFromEnv(e, os.Getenv); err != nil {
				return err
			}

			mode := ui.Mode{Plain: true, Width: 100}
			if cmd.OutOrStdout() == io.Writer(os.Stdout) {
				mode = ui.Detect(os.Stdout, os.Getenv)
			}

			c, closeConsole, err := newUpdateConsole(e.Out, mode, e.L, os.Getenv, time.Now())
			if err != nil {
				return err
			}

			defer closeConsole()

			if os.Getenv(selfupdate.EnvMarker) == "" {
				c.Println(c.Theme().Brand(" ContextMatrix setup") + c.Theme().Dim(" · update"))
			}

			if !noSelf {
				opts := selfupdate.Options{L: e.L, R: e.R, Git: e.Git, Out: e.Out, UI: c, Args: os.Args}

				// A repo base override applies to the installer's own repo too.
				if e.RepoURL != nil {
					opts.URL = e.RepoURL(repos.Setup)
				}

				if err := selfupdate.Run(ctx, opts); err != nil {
					return err
				}
			}

			return e.Update(ctx, engine.UpdateOptions{Yes: yes, Confirm: updateConfirm(c, os.Stdin), UI: c})
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "do not ask before applying updates")
	cmd.Flags().BoolVar(&noSelf, "no-self-update", false, "skip rebuilding contextmatrix-setup itself")

	return cmd
}

// healthFromEnv applies the test-only CONTEXTMATRIX_SETUP_HEALTH_BUDGET, a Go
// duration: 0 skips the health check, a positive value bounds it.
func healthFromEnv(e *engine.Engine, getenv func(string) string) error {
	v := getenv("CONTEXTMATRIX_SETUP_HEALTH_BUDGET")
	if v == "" {
		return nil
	}

	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return fmt.Errorf("CONTEXTMATRIX_SETUP_HEALTH_BUDGET=%q: want a duration such as 30s, or 0", v)
	}

	if d == 0 {
		e.SkipHealth = true
	} else {
		e.HealthBudget = d
	}

	return nil
}

// newUpdateConsole builds the update console. Only a styled terminal run gets
// a log; a re-exec child appends to the parent's via CONTEXTMATRIX_SETUP_LOG.
func newUpdateConsole(out io.Writer, m ui.Mode, l layout.Layout, getenv func(string) string, now time.Time) (*ui.Console, func(), error) {
	th := ui.NewTheme(m)

	if !m.TTY || m.Plain {
		c := ui.NewConsole(out, th, nil, "")

		return c, c.Close, nil
	}

	var (
		f   *os.File
		err error
	)

	path := getenv(logEnv)
	if path != "" {
		f, err = ui.AppendLog(path)
	} else {
		f, err = ui.OpenLog(l.LogsDir(), now, 10)
		if err == nil {
			path = f.Name()
			// The re-exec child inherits this.
			err = os.Setenv(logEnv, path)
		}
	}

	if err != nil {
		if f != nil {
			_ = f.Close()
		}

		return nil, nil, fmt.Errorf("open update log: %w", err)
	}

	c := ui.NewConsole(out, th, f, layout.Tilde(l, path))

	return c, func() {
		c.Close()
		_ = f.Close()
	}, nil
}

const logEnv = "CONTEXTMATRIX_SETUP_LOG"

// updateConfirm asks the update question on the console; an empty or
// unreadable answer means yes.
func updateConfirm(c *ui.Console, in io.Reader) func(string) bool {
	return func(string) bool {
		c.Prompt(" " + c.Theme().Accent("?") + " Apply these updates? [Y/n] ")

		var answer string

		if _, err := fmt.Fscanln(in, &answer); err != nil && !errors.Is(err, os.ErrClosed) {
			return true
		}

		return answer == "" || answer == "y" || answer == "Y"
	}
}
