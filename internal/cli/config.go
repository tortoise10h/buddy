package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Get, set, or show configuration",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("[buddy-ctl] config: skeleton called")
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show full configuration",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("[buddy-ctl] config show: skeleton called")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get [key]",
		Short: "Get configuration value for key",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] config get: skeleton called with args %v\n", args)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set configuration key to value",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] config set: skeleton called with args %v\n", args)
		},
	})

	return cmd
}
