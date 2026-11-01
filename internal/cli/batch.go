package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newBatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "batch",
		Short: "Batch dispatcher: Dispatches tasks to multiple projects concurrently or sequentially",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] batch: skeleton called with args %v\n", args)
		},
	}
}
