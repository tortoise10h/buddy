package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDiagnoseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diagnose",
		Short: "Diagnostician helper: Analyzes full terminal log for errors and stack traces",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] diagnose: skeleton called with args %v\n", args)
		},
	}
}
