package cli

import (
	"github.com/spf13/cobra"
)

// NewRootCmd builds the CLI. Subcommands are attached by their own files.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "contextmatrix-setup",
		Short:         "Install and update a local ContextMatrix stack",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newInstallCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newMigrateCmd())
	root.AddCommand(newUninstallCmd())

	return root
}
