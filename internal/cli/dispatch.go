package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDispatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dispatch",
		Short: "Fast-path atomic task dispatch",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] dispatch: skeleton called with args %v\n", args)
		},
	}
}
