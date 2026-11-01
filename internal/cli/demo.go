package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDemoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "demo",
		Short: "Focus task tab in Rex and optionally launch dev server split",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] demo: skeleton called with args %v\n", args)
		},
	}
}
