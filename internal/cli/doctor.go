package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/doctor"
)

func newDoctorCmd() *cobra.Command {
	var (
		fix        bool
		force      bool
		jsonOutput bool
		strict     bool
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose Buddy installation, configuration, engines, and agent harnesses",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			report := doctor.Run(cmd.Context(), doctor.Options{
				Version: Version,
				Fix:     fix,
				Force:   force,
			})

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(report); err != nil {
					return err
				}
			} else {
				doctor.FormatHuman(cmd.OutOrStdout(), report)
			}

			if report.Summary.Fail > 0 {
				cmd.SilenceErrors = true
				return fmt.Errorf("doctor found %d failing check(s)", report.Summary.Fail)
			}
			if strict && report.Summary.Warn > 0 {
				cmd.SilenceErrors = true
				return fmt.Errorf("doctor found %d warning(s) under --strict", report.Summary.Warn)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&fix, "fix", false, "Automatically repair safe deterministic issues")
	cmd.Flags().BoolVar(&force, "force", false, "Allow --fix to overwrite customized managed files (with backup)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output diagnostic report as JSON")
	cmd.Flags().BoolVar(&strict, "strict", false, "Exit non-zero if any warnings are present")

	return cmd
}
