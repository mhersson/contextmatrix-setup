package cli

import "github.com/spf13/cobra"

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show installed versions, services, ports",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := newEngine(cmd.Context(), cmd.OutOrStdout())
			if err != nil {
				return err
			}

			s, err := e.Status(cmd.Context())
			if err != nil {
				return err
			}

			e.PrintStatus(s)

			return nil
		},
	}
}
