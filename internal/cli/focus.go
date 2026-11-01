package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newFocusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "focus",
		Short: "Focus a project tab, task tab, or window in Rex",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] focus: skeleton called with args %v\n", args)
		},
	}
}
