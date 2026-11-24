package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
)

func newNotifyCmd() *cobra.Command {
	var (
		title    string
		subtitle string
		message  string
		sound    string
		noSound  bool
		force    bool
	)

	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Send native macOS desktop notification (when enabled in config or with --force)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				cfg = config.DefaultConfig()
			}

			soundName := cfg.NotificationSound()
			if cmd.Flags().Changed("sound") {
				soundName = sound
			}
			if noSound {
				soundName = ""
			}

			enabled := force || cfg.DesktopNotificationsEnabled()

			resp := map[string]any{
				"status":   "disabled",
				"title":    title,
				"subtitle": subtitle,
				"message":  message,
				"sound":    soundName,
			}
			if !enabled {
				resp["reason"] = "desktop notifications are disabled in config (enable via 'buddy-ctl config set notifications.desktop_enabled true' or pass --force)"
			} else if notifyFn(title, subtitle, message, soundName, true) {
				resp["status"] = "sent"
			} else {
				resp["status"] = "failed_or_disabled"
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVar(&title, "title", "Buddy Orchestrator", "Notification title")
	cmd.Flags().StringVar(&subtitle, "subtitle", "", "Notification subtitle")
	cmd.Flags().StringVarP(&message, "message", "m", "", "Notification message body")
	cmd.Flags().StringVar(&sound, "sound", "Glass", "Sound effect name (defaults to config notifications.sound)")
	cmd.Flags().BoolVar(&noSound, "no-sound", false, "Disable audio chime")
	cmd.Flags().BoolVar(&force, "force", false, "Send notification even when notifications.desktop_enabled is false")

	return cmd
}
