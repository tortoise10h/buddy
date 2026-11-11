package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
)

func newEngineCmd() *cobra.Command {
	var (
		flagEngine string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:     "engine",
		Aliases: []string{"host"},
		Short:   "Introspect host environment detection and engine resolution",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := config.LoadConfig()
			res := engine.ResolveEngine(flagEngine, os.Getenv, cfg)
			res.WarnUnknownHost(os.Stderr)

			if jsonOutput {
				resp := map[string]any{
					"detected_host": res.HostInfo.Kind,
					"host":          res.HostInfo.Kind,
					"engine":        res.EngineName,
					"source":        res.Source,
					"signals":       res.HostInfo.Signals,
				}
				if res.HostInfo.Details != "" {
					resp["details"] = res.HostInfo.Details
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			fmt.Printf("Detected Host: %s\n", res.HostInfo.Kind)
			fmt.Printf("Resolved Engine: %s (source: %s)\n", res.EngineName, res.Source)

			if len(res.HostInfo.Signals) == 0 {
				fmt.Println("Signals: none")
			} else {
				keys := make([]string, 0, len(res.HostInfo.Signals))
				for k := range res.HostInfo.Signals {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				pairs := make([]string, 0, len(keys))
				for _, k := range keys {
					pairs = append(pairs, fmt.Sprintf("%s=%s", k, res.HostInfo.Signals[k]))
				}
				fmt.Printf("Signals: %s\n", strings.Join(pairs, ", "))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&flagEngine, "engine", "", "Override engine (rex, t3)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")

	return cmd
}
