package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit code %d", e.Code)
}

func (e *ExitError) Unwrap() error {
	return e.Err
}

var rootCmd = &cobra.Command{
	Use:   "buddy-ctl",
	Short: "Command-line controller for Buddy, the multi-project Rex terminal orchestrator.",
	Long:  "buddy-ctl — Command-line controller for Buddy, the multi-project Rex terminal orchestrator.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return &ExitError{
				Code: 2,
				Err:  fmt.Errorf("unknown command '%s'", args[0]),
			}
		}
		return cmd.Help()
	},
}

func init() {
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &ExitError{Code: 2, Err: err}
	})

	rootCmd.AddCommand(newContextInjectCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newCaptureCmd())
	rootCmd.AddCommand(newDispatchCmd())
	rootCmd.AddCommand(newVersionCmd())
}

// Execute runs the root CLI command.
func Execute() error {
	err := rootCmd.Execute()
	if err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Code == 2 && exitErr.Err != nil {
				fmt.Fprintf(os.Stderr, "Error: %s\n\n%s", exitErr.Err.Error(), rootCmd.UsageString())
			}
			return exitErr
		}
		fmt.Fprintf(os.Stderr, "Error: %s\n\n%s", err.Error(), rootCmd.UsageString())
		return &ExitError{Code: 2, Err: err}
	}
	return nil
}

// GetRootCmd returns the cobra.Command for testing.
func GetRootCmd() *cobra.Command {
	return rootCmd
}
