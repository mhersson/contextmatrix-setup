package cli

import "github.com/spf13/cobra"

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove services; keep configs, state and binaries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := newEngine(cmd.Context(), cmd.OutOrStdout())
			if err != nil {
				return err
			}

			return e.Uninstall(cmd.Context())
		},
	}
}
