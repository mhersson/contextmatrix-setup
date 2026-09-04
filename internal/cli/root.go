package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

var errNotImplemented = errors.New("not implemented")

// NewRootCmd builds the CLI. Subcommands are attached by their own files;
// each starts as a stub and is replaced when its engine flow lands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "contextmatrix-setup",
		Short:         "Install and update a local ContextMatrix stack",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(stub("install", "Interactive first-time install"))
	root.AddCommand(stub("update", "Pull, rebuild, sync config, restart what changed"))
	root.AddCommand(stub("status", "Show installed versions, services, ports"))
	root.AddCommand(stub("migrate", "Move an existing default-layout install under ~/.contextmatrix"))
	root.AddCommand(stub("uninstall", "Remove services; keep configs, state and binaries"))

	return root
}

func stub(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(*cobra.Command, []string) error {
			return errNotImplemented
		},
	}
}
