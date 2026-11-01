package cli

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Get, set, or show configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow()
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show full configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow()
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get [key]",
		Short: "Get configuration value for key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return err
			}
			val, err := cfg.GetPath(args[0])
			if err != nil {
				return err
			}
			resp := map[string]any{
				"key":   args[0],
				"value": val,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set configuration key to value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return err
			}
			key := args[0]
			rawVal := args[1]

			var parsedVal any = rawVal
			if strings.EqualFold(rawVal, "true") {
				parsedVal = true
			} else if strings.EqualFold(rawVal, "false") {
				parsedVal = false
			} else if intVal, err := strconv.Atoi(rawVal); err == nil {
				parsedVal = intVal
			} else {
				var jsonVal any
				if err := json.Unmarshal([]byte(rawVal), &jsonVal); err == nil {
					parsedVal = jsonVal
				}
			}

			if err := cfg.SetPath(key, parsedVal); err != nil {
				return err
			}

			resp := map[string]any{
				"status": "ok",
				"key":    key,
				"value":  parsedVal,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	})

	return cmd
}

func runConfigShow() error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}
