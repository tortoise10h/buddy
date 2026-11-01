package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newEnsureSessionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ensure-session",
		Short: "Ensure a Rex session exists for a project",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] ensure-session: skeleton called with args %v\n", args)
		},
	}
}
