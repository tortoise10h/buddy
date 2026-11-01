package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/notify"
)

func newNotifyCmd() *cobra.Command {
	var (
		title    string
		subtitle string
		message  string
		sound    string
		noSound  bool
	)

	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Send native macOS desktop notification",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				cfg = config.DefaultConfig()
			}

			soundName := sound
			if noSound {
				soundName = ""
			}

			sent := notify.SendDesktopNotification(title, subtitle, message, soundName, true)

			resp := map[string]any{
				"status":   "failed_or_disabled",
				"title":    title,
				"subtitle": subtitle,
				"message":  message,
				"sound":    soundName,
			}
			if sent {
				resp["status"] = "sent"
			}

			_ = cfg // cfg can be referenced if needed for config overrides

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVar(&title, "title", "Buddy Orchestrator", "Notification title")
	cmd.Flags().StringVar(&subtitle, "subtitle", "", "Notification subtitle")
	cmd.Flags().StringVarP(&message, "message", "m", "", "Notification message body")
	cmd.Flags().StringVar(&sound, "sound", "Glass", "Sound effect name")
	cmd.Flags().BoolVar(&noSound, "no-sound", false, "Disable audio chime")

	return cmd
}
