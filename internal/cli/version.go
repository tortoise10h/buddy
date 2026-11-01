package cli

import (
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print buddy-ctl version information",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("buddy-ctl %s (commit: %s, date: %s)\n", Version, Commit, Date)
		},
	}
}
