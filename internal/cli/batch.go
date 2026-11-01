package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/formatters"
)

func newBatchCmd() *cobra.Command {
	var (
		flagJSON string
		flagFile string
	)

	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Batch dispatcher: Dispatches tasks to multiple projects concurrently or sequentially",
		RunE: func(cmd *cobra.Command, args []string) error {
			var rawBytes []byte

			if flagJSON != "" {
				rawBytes = []byte(flagJSON)
			} else if flagFile != "" {
				data, err := os.ReadFile(flagFile)
				if err != nil {
					errJSON, _ := formatters.FormatJSON(map[string]string{
						"error": fmt.Sprintf("Failed to read file: %v", err),
					})
					fmt.Fprintln(os.Stderr, errJSON)
					return &ExitError{Code: 1}
				}
				rawBytes = data
			} else {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": "Provide --json or --file with array of task specs",
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			var tasksRaw []map[string]any
			if err := json.Unmarshal(rawBytes, &tasksRaw); err != nil {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": "Batch payload must be a JSON array of task objects",
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			selfBin, err := os.Executable()
			if err != nil {
				selfBin = "buddy-ctl"
			}

			var results []any
			for _, tSpec := range tasksRaw {
				proj, _ := tSpec["project"].(string)
				taskSlug, _ := tSpec["task"].(string)
				prompt, _ := tSpec["prompt"].(string)
				agent, _ := tSpec["agent"].(string)

				planFirst := true
				if pfVal, ok := tSpec["plan_first"]; ok {
					if pfBool, ok := pfVal.(bool); ok {
						planFirst = pfBool
					}
				}

				worktree := false
				if wtVal, ok := tSpec["worktree"]; ok {
					if wtBool, ok := wtVal.(bool); ok {
						worktree = wtBool
					}
				}

				if proj == "" || taskSlug == "" {
					results = append(results, map[string]any{
						"status":  "error",
						"message": "Missing project or task in spec",
						"spec":    tSpec,
					})
					continue
				}

				subArgs := []string{
					"task", "start",
					"--project", proj,
					"--task", taskSlug,
				}
				if agent != "" {
					subArgs = append(subArgs, "--agent", agent)
				}
				if worktree {
					subArgs = append(subArgs, "--worktree")
				}
				if planFirst {
					subArgs = append(subArgs, "--plan-first")
				}
				if prompt != "" {
					subArgs = append(subArgs, "--prompt", prompt)
				}

				subCmd := exec.Command(selfBin, subArgs...)
				var stdoutBuf, stderrBuf bytes.Buffer
				subCmd.Stdout = &stdoutBuf
				subCmd.Stderr = &stderrBuf

				cmdErr := subCmd.Run()
				if cmdErr == nil {
					var itemRes any
					if errUnmarshal := json.Unmarshal(stdoutBuf.Bytes(), &itemRes); errUnmarshal == nil {
						results = append(results, itemRes)
					} else {
						results = append(results, stdoutBuf.String())
					}
				} else {
					results = append(results, map[string]any{
						"status":  "error",
						"project": proj,
						"task":    taskSlug,
						"error":   stderrBuf.String(),
					})
				}
			}

			if results == nil {
				results = []any{}
			}

			outJSON, err := formatters.FormatJSON(map[string]any{
				"status": "ok",
				"total":  len(results),
				"tasks":  results,
			})
			if err != nil {
				return err
			}
			fmt.Println(outJSON)
			return nil
		},
	}

	cmd.Flags().StringVarP(&flagJSON, "json", "j", "", "JSON string representing array of task specifications")
	cmd.Flags().StringVarP(&flagFile, "file", "f", "", "Path to JSON file containing array of task specifications")

	return cmd
}
