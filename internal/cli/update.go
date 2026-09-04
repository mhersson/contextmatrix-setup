package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/mhersson/contextmatrix-setup/internal/engine"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/selfupdate"
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

			if !noSelf {
				opts := selfupdate.Options{L: e.L, R: e.R, Git: e.Git, Out: e.Out, Args: os.Args}

				// A repo base override applies to the installer's own repo too.
				if e.RepoURL != nil {
					opts.URL = e.RepoURL(repos.Setup)
				}

				if err := selfupdate.Run(ctx, opts); err != nil {
					return err
				}
			}

			return e.Update(ctx, engine.UpdateOptions{Yes: yes, Confirm: confirmPrompt(cmd)})
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "do not ask before applying updates")
	cmd.Flags().BoolVar(&noSelf, "no-self-update", false, "skip rebuilding contextmatrix-setup itself")

	return cmd
}
