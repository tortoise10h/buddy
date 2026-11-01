package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newContextInjectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "context-inject",
		Short: "PreInvocation hook handler (internal)",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] context-inject: skeleton called with args %v\n", args)
		},
	}
}
