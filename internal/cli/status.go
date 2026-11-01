package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show tracked projects and tasks from cache",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("[buddy-ctl] status: skeleton called")
		},
	}
}
