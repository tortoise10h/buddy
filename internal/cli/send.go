package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
		answers []string
	)

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send text, answers, or keys to worker agent pane/thread",
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

			var keyedAnswers map[string]any
			var answersList []string
			for _, a := range answers {
				trimmed := strings.TrimSpace(a)
				if trimmed == "" {
					continue
				}
				if strings.Contains(trimmed, "=") {
					parts := strings.SplitN(trimmed, "=", 2)
					keyName := strings.TrimSpace(parts[0])
					if keyName != "" {
						if keyedAnswers == nil {
							keyedAnswers = make(map[string]any)
						}
						keyedAnswers[keyName] = strings.TrimSpace(parts[1])
						continue
					}
				}
				answersList = append(answersList, trimmed)
			}

			input := backend.Input{
				Text:         text,
				Key:          key,
				Enter:        !noEnter && text != "",
				Answers:      answersList,
				KeyedAnswers: keyedAnswers,
			}

			if err := eng.Send(cmd.Context(), sessID, blkID, input); err != nil {
				return err
			}

			sentKey := key
			if sentKey == "" && input.Enter {
				sentKey = "enter"
			}

			resp := map[string]any{
				"status":       "ok",
				"project":      projName,
				"task":         taskSlug,
				"sent_text":    text != "",
				"sent_key":     sentKey,
				"sent_answers": len(answersList),
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&task, "task", "t", "", "Task slug")
	cmd.Flags().StringVarP(&session, "session", "s", "", "Direct session ID")
	cmd.Flags().StringVarP(&block, "block", "b", "", "Direct block ID / thread ID")
	cmd.Flags().StringVar(&text, "text", "", "Text to send to the pane or thread")
	cmd.Flags().StringVar(&key, "key", "", "Key or shortcut to send (e.g. enter, ctrl+c, esc)")
	cmd.Flags().BoolVar(&noEnter, "no-enter", false, "Do not press enter after typing text")
	cmd.Flags().StringArrayVarP(&answers, "answer", "A", []string{}, "Answer to a worker prompt (repeatable for multi-question prompts, or key=value)")

	return cmd
}
