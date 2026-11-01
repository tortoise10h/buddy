package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Watchdog monitor: Polls worker agent pane(s) until input needed, finished, or error",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] watch: skeleton called with args %v\n", args)
		},
	}
}
