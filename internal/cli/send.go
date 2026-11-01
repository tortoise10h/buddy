package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSendCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "send",
		Short: "Send text or keys to worker agent pane",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] send: skeleton called with args %v\n", args)
		},
	}
}
