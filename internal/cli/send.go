package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backend"
)

func newSendCmd() *cobra.Command {
	var (
		project string
		task    string
		session string
		block   string
		text    string
		key     string
		noEnter bool
	)

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send text or keys to worker agent pane",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, _, blkID, projName, taskSlug, engName, err := resolveTarget(project, task, session, "", block)
			if err != nil {
				return err
			}
			if sessID == "" || blkID == "" {
				return fmt.Errorf("missing session_id or agent_block_id (specify --project/--task or -s/-b)")
			}

			eng, err := getEngine(engName)
			if err != nil {
				return err
			}
			input := backend.Input{
				Text:  text,
				Key:   key,
				Enter: !noEnter && text != "",
			}

			if err := eng.Send(cmd.Context(), sessID, blkID, input); err != nil {
				return err
			}

			sentKey := key
			if sentKey == "" && input.Enter {
				sentKey = "enter"
			}

			resp := map[string]any{
				"status":    "ok",
				"project":   projName,
				"task":      taskSlug,
				"sent_text": text != "",
				"sent_key":  sentKey,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&task, "task", "t", "", "Task slug")
	cmd.Flags().StringVarP(&session, "session", "s", "", "Direct session ID")
	cmd.Flags().StringVarP(&block, "block", "b", "", "Direct block ID")
	cmd.Flags().StringVar(&text, "text", "", "Text to send to the pane")
	cmd.Flags().StringVar(&key, "key", "", "Key or shortcut to send (e.g. enter, ctrl+c, esc)")
	cmd.Flags().BoolVar(&noEnter, "no-enter", false, "Do not press enter after typing text")

	return cmd
}
