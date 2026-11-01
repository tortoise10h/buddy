package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/formatters"
	"github.com/tortoise10h/buddy/internal/notify"
)

func newNotifyCmd() *cobra.Command {
	var (
		flagTitle    string
		flagSubtitle string
		flagMessage  string
		flagSound    string
		flagNoSound  bool
	)

	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Send native macOS desktop notification",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagMessage == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("missing required --message argument")}
			}

			cfg := config.LoadConfig()

			var soundPtr *string
			var soundParam string
			if !flagNoSound {
				snd := flagSound
				if snd == "" {
					snd = "Glass"
				}
				soundPtr = &snd
				soundParam = snd
			}

			sent := notify.SendDesktopNotification(
				flagTitle,
				flagSubtitle,
				flagMessage,
				soundParam,
				&cfg,
			)

			status := "sent"
			if !sent {
				status = "failed_or_disabled"
			}

			outJSON, err := formatters.FormatJSON(map[string]any{
				"status":   status,
				"title":    flagTitle,
				"subtitle": flagSubtitle,
				"message":  flagMessage,
				"sound":    soundPtr,
			})
			if err != nil {
				return err
			}
			fmt.Println(outJSON)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagTitle, "title", "Buddy Orchestrator", "Notification title")
	cmd.Flags().StringVar(&flagSubtitle, "subtitle", "", "Notification subtitle")
	cmd.Flags().StringVarP(&flagMessage, "message", "m", "", "Notification message text")
	cmd.Flags().StringVar(&flagSound, "sound", "Glass", "System sound name (default: Glass)")
	cmd.Flags().BoolVar(&flagNoSound, "no-sound", false, "Send notification silently without sound")

	_ = cmd.MarkFlagRequired("message")

	return cmd
}
