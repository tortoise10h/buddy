package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Task management",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("[buddy-ctl] task: skeleton called")
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "start",
		Short: "Start a new feature task tab in project session",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] task start: skeleton called with args %v\n", args)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "update",
		Short: "Update task status or summary in state cache",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] task update: skeleton called with args %v\n", args)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "close",
		Short: "Close task tab in Rex and update state",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] task close: skeleton called with args %v\n", args)
		},
	})

	return cmd
}
