package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize ~/.buddy control tower directory",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("[buddy-ctl] init: skeleton called")
		},
	}
}
