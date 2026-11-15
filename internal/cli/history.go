package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/state"
)

func newTaskHistoryCmd() *cobra.Command {
	var (
		projectName string
		taskSlug    string
		engineName  string
		sinceSpec   string
		limit       int
		jsonOut     bool
	)

	cmd := &cobra.Command{
		Use:   "history",
		Short: "Query append-only task cleanup history",
		Long:  "Query ~/.buddy/history/tasks.jsonl for previously cleaned tasks, newest first.",
		RunE: func(cmd *cobra.Command, args []string) error {
			sinceTime, err := state.ParseSince(sinceSpec)
			if err != nil {
				return err
			}

			records, err := state.QueryHistory(state.HistoryFilter{
				Project:  projectName,
				TaskSlug: taskSlug,
				Engine:   engineName,
				Since:    sinceTime,
				Limit:    limit,
			})
			if err != nil {
				return err
			}

			if jsonOut {
				if records == nil {
					records = []state.HistoryRecord{}
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(records)
			}

			if len(records) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No task history records found.")
				return nil
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "TS\tPROJECT\tTASK\tENGINE\tSTATUS\tFORCED\tREASON")
			for _, r := range records {
				tsShort := r.Ts
				if len(tsShort) > 19 {
					tsShort = tsShort[:19]
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%v\t%s\n",
					tsShort, r.Project, r.TaskSlug, r.Engine, r.FinalStatus, r.Forced, r.Reason)
			}
			return tw.Flush()
		},
	}

	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Filter by project name")
	cmd.Flags().StringVarP(&taskSlug, "task", "t", "", "Filter by task slug")
	cmd.Flags().StringVar(&engineName, "engine", "", "Filter by execution engine (rex, t3, wezterm)")
	cmd.Flags().StringVar(&sinceSpec, "since", "", "Filter records since duration/date (e.g. 24h, 7d, 2006-01-02, RFC3339)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of records to return (-1 for all)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output JSON array")

	return cmd
}
