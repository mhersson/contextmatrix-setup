package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/mhersson/contextmatrix-setup/internal/migrate"
	"github.com/mhersson/contextmatrix-setup/internal/wizard"
)

func newMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Move an existing default-layout install under ~/.contextmatrix, then install",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := newEngine(cmd.Context(), cmd.OutOrStdout())
			if err != nil {
				return err
			}

			found := migrate.Detect(e.L)
			if !found.Any() {
				return errors.New("no default-layout install found")
			}

			plan, err := migrate.Build(e.L, found, nil)
			if err != nil {
				return err
			}

			moves, err := wizard.AskMoveRepos(plan.RepoDirs)
			if err != nil {
				return err
			}

			if plan, err = migrate.Build(e.L, found, moves); err != nil {
				return err
			}

			if err := e.Migrate(cmd.Context(), plan); err != nil {
				return err
			}

			return chainedInstall().ExecuteContext(cmd.Context())
		},
	}
}

// chainedInstall prepares the install command for a run inside another
// command. Without an explicit empty argument list cobra falls back to
// os.Args[1:], which still says "migrate", and install rejects it.
func chainedInstall() *cobra.Command {
	sub := newInstallCmd()
	sub.SetArgs([]string{})
	sub.SilenceUsage, sub.SilenceErrors = true, true

	return sub
}
