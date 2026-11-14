package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	Version = "v0.1.0-dev"
	Commit  = "none"
	Date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "buddy-ctl",
	Short: "Controller CLI for Buddy, the multi-project Rex terminal orchestrator.",
	Long:  "Controller CLI for Buddy, the multi-project Rex terminal orchestrator.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if !f.Changed {
				_ = f.Value.Set(f.DefValue)
			}
		})
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func resetCommandTreeFlags(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	for _, sub := range cmd.Commands() {
		resetCommandTreeFlags(sub)
	}
}

func wrapSubcommandsResetFlags(cmd *cobra.Command) {
	if cmd.RunE != nil {
		origRunE := cmd.RunE
		cmd.RunE = func(c *cobra.Command, args []string) error {
			defer resetCommandTreeFlags(rootCmd)
			return origRunE(c, args)
		}
	}
	for _, sub := range cmd.Commands() {
		wrapSubcommandsResetFlags(sub)
	}
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.Version = fmt.Sprintf("%s (commit: %s, date: %s)", Version, Commit, Date)
	rootCmd.SetVersionTemplate("buddy-ctl {{.Version}}\n")

	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newInitCmd())
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newProjectsCmd())
	rootCmd.AddCommand(newEnsureSessionCmd())
	rootCmd.AddCommand(newDispatchCmd())
	rootCmd.AddCommand(newTaskCmd())
	rootCmd.AddCommand(newCaptureCmd())
	rootCmd.AddCommand(newSendCmd())
	rootCmd.AddCommand(newDemoCmd())
	rootCmd.AddCommand(newFocusCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newWatchCmd())
	rootCmd.AddCommand(newNotifyCmd())
	rootCmd.AddCommand(newDiagnoseCmd())
	rootCmd.AddCommand(newEngineCmd())
	rootCmd.AddCommand(newBatchCmd())
	rootCmd.AddCommand(newContextInjectCmd())

	wrapSubcommandsResetFlags(rootCmd)
}
