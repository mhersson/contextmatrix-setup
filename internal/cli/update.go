package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/mhersson/contextmatrix-setup/internal/engine"
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

			c := ui.NewConsole(e.Out, ui.NewTheme(ui.Mode{Plain: true, Width: 100}), nil, "")
			defer c.Close()

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

			return e.Update(ctx, engine.UpdateOptions{Yes: yes, Confirm: confirmPrompt(cmd), UI: c})
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
