package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/formatters"
)

func newConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config [show|get|set] [key] [value]",
		Short: "Get, set, or show configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			action := "show"
			if len(args) > 0 {
				action = args[0]
			}

			rawCfg, err := config.LoadRawConfig()
			if err != nil {
				return err
			}

			switch action {
			case "show":
				jsonStr, err := formatters.FormatJSON(rawCfg)
				if err != nil {
					return err
				}
				fmt.Println(jsonStr)
				return nil

			case "get":
				if len(args) < 2 || args[1] == "" {
					errJSON, _ := formatters.FormatJSON(map[string]string{
						"error": "Missing key",
					})
					fmt.Fprintln(os.Stderr, errJSON)
					return &ExitError{Code: 1}
				}
				key := args[1]
				val := config.GetDotPath(rawCfg, key)
				out := map[string]any{
					"key":   key,
					"value": val,
				}
				jsonStr, err := formatters.FormatJSON(out)
				if err != nil {
					return err
				}
				fmt.Println(jsonStr)
				return nil

			case "set":
				if len(args) < 3 || args[1] == "" {
					errJSON, _ := formatters.FormatJSON(map[string]string{
						"error": "Missing key or value",
					})
					fmt.Fprintln(os.Stderr, errJSON)
					return &ExitError{Code: 1}
				}
				key := args[1]
				valStr := args[2]
				parsedVal := config.ParseConfigValue(valStr)

				config.SetDotPath(rawCfg, key, parsedVal)
				if err := config.SaveRawConfig(rawCfg); err != nil {
					return err
				}

				out := map[string]any{
					"status": "ok",
					"key":    key,
					"value":  parsedVal,
				}
				jsonStr, err := formatters.FormatJSON(out)
				if err != nil {
					return err
				}
				fmt.Println(jsonStr)
				return nil

			default:
				return &ExitError{
					Code: 2,
					Err:  fmt.Errorf("invalid choice: '%s' (choose from 'show', 'get', 'set')", action),
				}
			}
		},
	}
}
