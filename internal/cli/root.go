package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	Version = "v0.1.0-dev"
	Commit  = "none"
	Date    = "unknown"

	globalWorkerBackend string
)

var rootCmd = &cobra.Command{
	Use:   "buddy-ctl",
	Short: "Controller CLI for Buddy, the multi-project Rex terminal orchestrator.",
	Long:  "Controller CLI for Buddy, the multi-project Rex terminal orchestrator.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.Version = fmt.Sprintf("%s (commit: %s, date: %s)", Version, Commit, Date)
	rootCmd.SetVersionTemplate("buddy-ctl {{.Version}}\n")

	rootCmd.PersistentFlags().StringVar(&globalWorkerBackend, "worker-backend", "", "Worker execution backend (rex, t3code)")

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
	rootCmd.AddCommand(newBatchCmd())
	rootCmd.AddCommand(newContextInjectCmd())
}
