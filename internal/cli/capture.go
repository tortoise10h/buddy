package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCaptureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "capture",
		Short: "Capture output from worker agent pane in O(1)",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] capture: skeleton called with args %v\n", args)
		},
	}
}
