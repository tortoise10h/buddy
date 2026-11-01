package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "List available project directories or manage project roots",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("[buddy-ctl] projects: skeleton called")
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "roots",
		Short: "List configured project roots",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("[buddy-ctl] projects roots: skeleton called")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "add-root [path]",
		Short: "Add a project root directory",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] projects add-root: skeleton called with args %v\n", args)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove-root [path]",
		Short: "Remove a project root directory",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] projects remove-root: skeleton called with args %v\n", args)
		},
	})

	return cmd
}
