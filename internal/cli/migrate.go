package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/mhersson/contextmatrix-setup/internal/migrate"
)

func newMigrateCmd() *cobra.Command {
	var yes, moveRepos bool

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Move an existing default-layout install under ~/.contextmatrix, then install",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			e, err := newEngine(ctx, cmd.OutOrStdout())
			if err != nil {
				return err
			}

			if err := requireTools(e); err != nil {
				return err
			}

			found := migrate.Detect(e.L)
			if !found.Any() {
				return errors.New("no default-layout install found")
			}

			a, known, err := migrateInstall(ctx, e, found, yes, moveRepos)
			if err != nil {
				return err
			}

			installed, err := isInstalled(e)
			if err != nil {
				return err
			}

			if installed {
				return updateInstead(cmd, e, yes)
			}

			return finishInstall(cmd, e, a, known, yes)
		},
	}

	f := cmd.Flags()
	f.BoolVar(&yes, "yes", false, "no prompts; keep every carried value and take the defaults for the rest")
	f.BoolVar(&moveRepos, "move-repos", false, "move boards and task-skills checkouts under ~/.contextmatrix")

	return cmd
}
