// Command orbit creates or attaches to an isolated OrbStack VM for the current workspace directory.
package main

import (
	"os"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "orbit",
		Short:        "Create or attach to an isolated OrbStack VM for the current workspace",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
