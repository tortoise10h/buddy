package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newNotifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "notify",
		Short: "Send native macOS desktop notification",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] notify: skeleton called with args %v\n", args)
		},
	}
}
