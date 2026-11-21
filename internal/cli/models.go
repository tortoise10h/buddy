package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backend/opencode"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
)

// ModelEntry represents one discoverable or configured model option.
type ModelEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Provider string `json:"provider,omitempty"`
	Context  string `json:"context,omitempty"`
	MaxOut   string `json:"max_out,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	Images   string `json:"images,omitempty"`
}

// ModelsResponse is the structured output of `buddy-ctl models`.
type ModelsResponse struct {
	Engine       string       `json:"engine"`
	Agent        string       `json:"agent,omitempty"`
	DefaultModel string       `json:"default_model,omitempty"`
	Source       string       `json:"source"`
	Models       []ModelEntry `json:"models"`
	Note         string       `json:"note,omitempty"`
}

var runModelsCommandFn = defaultRunModelsCommand

func defaultRunModelsCommand(ctx context.Context, argv []string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("empty models command")
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, argv[0], argv[1:]...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("running %q failed: %w", strings.Join(argv, " "), err)
	}
	return string(out), nil
}

func newModelsCmd() *cobra.Command {
	var (
		flagAgent  string
		flagEngine string
		query      string
		formatStr  string
	)

	cmd := &cobra.Command{
		Use:   "models",
		Short: "List available models for the target agent or execution engine",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				cfg = config.DefaultConfig()
			}

			resEngine := engine.ResolveEngine(flagEngine, os.Getenv, cfg)
			engineName := resEngine.EngineName
			agentName := strings.TrimSpace(flagAgent)

			resp, err := discoverModels(cmd.Context(), engineName, agentName, cfg)
			if err != nil {
				return err
			}

			if q := strings.TrimSpace(query); q != "" {
				resp.Models = filterModelEntries(resp.Models, q)
			}
			if resp.Models == nil {
				resp.Models = []ModelEntry{}
			}

			format := strings.ToLower(strings.TrimSpace(formatStr))
			out := cmd.OutOrStdout()
			switch format {
			case "", "json":
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)

			case "brief":
				header := fmt.Sprintf("Engine: %s", resp.Engine)
				if resp.Agent != "" {
					header += fmt.Sprintf(" | Agent: %s", resp.Agent)
				}
				if resp.DefaultModel != "" {
					header += fmt.Sprintf(" | Default: %s", resp.DefaultModel)
				}
				header += fmt.Sprintf(" (source: %s)", resp.Source)
				fmt.Fprintln(out, header)
				if resp.Note != "" {
					fmt.Fprintf(out, "Note: %s\n", resp.Note)
				}
				if len(resp.Models) == 0 {
					fmt.Fprintln(out, "No models found.")
					return nil
				}
				for _, m := range resp.Models {
					if m.Name != "" && m.Name != m.ID {
						fmt.Fprintf(out, "• %s (%s)\n", m.ID, m.Name)
					} else {
						fmt.Fprintf(out, "• %s\n", m.ID)
					}
				}
				return nil

			case "table":
				header := fmt.Sprintf("Engine: %s", resp.Engine)
				if resp.Agent != "" {
					header += fmt.Sprintf(" | Agent: %s", resp.Agent)
				}
				fmt.Fprintln(out, header)
				if resp.Note != "" {
					fmt.Fprintf(out, "Note: %s\n", resp.Note)
				}
				fmt.Fprintf(out, "%-40s %-16s %s\n", "MODEL", "PROVIDER", "NAME")
				fmt.Fprintln(out, strings.Repeat("-", 80))
				for _, m := range resp.Models {
					prov := m.Provider
					if prov == "" {
						prov = "-"
					}
					name := m.Name
					if name == "" {
						name = "-"
					}
					fmt.Fprintf(out, "%-40s %-16s %s\n", m.ID, prov, name)
				}
				return nil

			default:
				return fmt.Errorf("invalid --format %q: expected json, table, or brief", formatStr)
			}
		},
	}

	cmd.Flags().StringVarP(&flagAgent, "agent", "a", "", "Agent CLI to inspect (agy, pi, opencode, claude, codex, etc.)")
	cmd.Flags().StringVar(&flagEngine, "engine", "", "Execution engine (rex, t3, opencode, wezterm)")
	cmd.Flags().StringVarP(&query, "query", "q", "", "Case-insensitive filter across model id, name, and provider")
	cmd.Flags().StringVarP(&formatStr, "format", "f", "json", "Output format: json, table, brief")

	return cmd
}

func discoverModels(ctx context.Context, engineName, agentName string, cfg *config.Config) (ModelsResponse, error) {
	if engineName == "t3" && agentName == "" {
		return discoverT3Models(cfg), nil
	}

	if engineName == "opencode" && (agentName == "" || agentName == "opencode") {
		return discoverOpenCodeModels(ctx, engineName, agentName, cfg)
	}

	// Terminal engines or explicit --agent override.
	if agentName == "" {
		agentName = strings.TrimSpace(os.Getenv("BUDDY_WORKER_AGENT"))
	}
	if agentName == "" && cfg != nil {
		agentName = cfg.GetEffectiveWorkerAgent(engineName)
	}
	if agentName == "" {
		agentName = "agy"
	}

	if agentName == "opencode" {
		return discoverOpenCodeModels(ctx, engineName, "opencode", cfg)
	}

	var agentCfg config.AgentConfig
	if cfg != nil && cfg.Agents != nil {
		agentCfg = cfg.Agents[agentName]
	}

	resp := ModelsResponse{
		Engine: engineName,
		Agent:  agentName,
	}

	if len(agentCfg.ModelsCmd) > 0 {
		raw, err := runModelsCommandFn(ctx, agentCfg.ModelsCmd)
		if err == nil {
			resp.Source = strings.Join(agentCfg.ModelsCmd, " ")
			resp.Models = parseModelsForAgent(agentName, raw)
			return resp, nil
		}
		if len(agentCfg.Models) > 0 {
			resp.Source = "config"
			resp.Models = staticModelEntries(agentCfg.Models)
			resp.Note = fmt.Sprintf("live discovery failed (%v); showing configured models", err)
			return resp, nil
		}
		return ModelsResponse{}, err
	}

	resp.Source = "config"
	resp.Models = staticModelEntries(agentCfg.Models)
	if len(resp.Models) == 0 {
		switch strings.ToLower(agentName) {
		case "claude":
			resp.Source = "builtin"
			resp.Models = []ModelEntry{
				{ID: "opus", Name: "Claude Opus (latest)", Provider: "anthropic"},
				{ID: "sonnet", Name: "Claude Sonnet (latest)", Provider: "anthropic"},
				{ID: "haiku", Name: "Claude Haiku (latest)", Provider: "anthropic"},
			}
			resp.Note = "claude CLI does not expose a live models subcommand; showing standard Claude Code model aliases (configure agents.claude.models or agents.claude.models_cmd in ~/.buddy/config.json to customize)"
		case "codex":
			resp.Note = "codex CLI does not expose a live models subcommand; set agents.codex.models or agents.codex.models_cmd in ~/.buddy/config.json"
		default:
			resp.Note = fmt.Sprintf("no models_cmd or static models configured for agent %q; set agents.%s.models_cmd or agents.%s.models in ~/.buddy/config.json", agentName, agentName, agentName)
		}
	}
	return resp, nil
}

func discoverT3Models(cfg *config.Config) ModelsResponse {
	var ec config.EngineConfig
	var defModel string
	if cfg != nil {
		ec = cfg.GetEngineConfig("t3")
		if p, m := cfg.GetT3ProviderModel(); p != "" && m != "" {
			defModel = p + "/" + m
		}
	}
	if env := strings.TrimSpace(os.Getenv("BUDDY_T3_MODEL")); env != "" && defModel == "" {
		defModel = env
	}

	var candidates []string
	if defModel != "" {
		candidates = append(candidates, defModel)
	}
	candidates = append(candidates, ec.Models...)

	return ModelsResponse{
		Engine:       "t3",
		DefaultModel: defModel,
		Source:       "config",
		Models:       staticModelEntries(candidates),
		Note:         "t3 CLI does not expose a live models subcommand; showing configured/env models from engines.t3",
	}
}

func discoverOpenCodeModels(ctx context.Context, engineName, agentName string, cfg *config.Config) (ModelsResponse, error) {
	var ec config.EngineConfig
	var defModel string
	var staticModels []string
	if cfg != nil {
		ec = cfg.GetEngineConfig("opencode")
		_, _, _, defModel, _ = cfg.GetOpenCodeConfig()
		staticModels = append(staticModels, ec.Models...)
		if a, ok := cfg.Agents["opencode"]; ok {
			if len(staticModels) == 0 {
				staticModels = append(staticModels, a.Models...)
			}
		}
	}
	if env := strings.TrimSpace(os.Getenv("BUDDY_OPENCODE_MODEL")); env != "" && defModel == "" {
		defModel = env
	}

	argv := []string{"opencode", "models"}
	if bin, err := opencode.FindOpenCodeBinary(); err == nil && bin != "" {
		argv[0] = bin
	}
	if cfg != nil {
		if a, ok := cfg.Agents["opencode"]; ok && len(a.ModelsCmd) > 0 {
			argv = append([]string(nil), a.ModelsCmd...)
			if argv[0] == "opencode" {
				if bin, err := opencode.FindOpenCodeBinary(); err == nil && bin != "" {
					argv[0] = bin
				}
			}
		}
	}

	resp := ModelsResponse{
		Engine:       engineName,
		Agent:        agentName,
		DefaultModel: defModel,
	}

	raw, err := runModelsCommandFn(ctx, argv)
	if err == nil {
		resp.Source = "opencode models"
		resp.Models = parseOpenCodeModelsOutput(raw)
		return resp, nil
	}

	if len(staticModels) > 0 || defModel != "" {
		var all []string
		if defModel != "" {
			all = append(all, defModel)
		}
		all = append(all, staticModels...)
		resp.Source = "config"
		resp.Models = staticModelEntries(all)
		resp.Note = fmt.Sprintf("live opencode models discovery failed (%v); showing configured models", err)
		return resp, nil
	}

	return ModelsResponse{}, err
}

func parseModelsForAgent(agentName, raw string) []ModelEntry {
	switch strings.ToLower(strings.TrimSpace(agentName)) {
	case "agy":
		return parseAgyModelsOutput(raw)
	case "pi":
		return parsePiModelsOutput(raw)
	case "opencode":
		return parseOpenCodeModelsOutput(raw)
	case "claude":
		return parseClaudeModelsOutput(raw)
	case "codex":
		return parseCodexModelsOutput(raw)
	default:
		return parseGenericModelsOutput(raw)
	}
}

func parseClaudeModelsOutput(raw string) []ModelEntry {
	return parseProviderModelsOutput(raw, "anthropic")
}

func parseCodexModelsOutput(raw string) []ModelEntry {
	return parseProviderModelsOutput(raw, "openai")
}

func parseProviderModelsOutput(raw, defaultProvider string) []ModelEntry {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	// Support JSON payloads from custom wrappers or API-backed models_cmd scripts:
	// - ["model-a", "model-b"]
	// - {"data": [{"id": "...", "display_name": "..."}]}
	// - {"models": [{"slug": "...", "id": "...", "display_name": "...", "name": "..."}]}
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		if entries := parseJSONModelsOutput(trimmed, defaultProvider); len(entries) > 0 {
			return entries
		}
	}

	var out []ModelEntry
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimLeft(line, "•*- ")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var entry ModelEntry
		if id, name, ok := strings.Cut(line, "\t"); ok {
			entry = ModelEntry{ID: strings.TrimSpace(id), Name: strings.TrimSpace(name)}
		} else if p, m, ok := strings.Cut(line, "/"); ok && p != "" && m != "" && !strings.Contains(line, " ") {
			entry = ModelEntry{ID: line, Provider: strings.TrimSpace(p), Name: strings.TrimSpace(m)}
		} else {
			fields := strings.Fields(line)
			if len(fields) >= 2 && (strings.HasPrefix(fields[1], "(") || strings.HasPrefix(fields[1], "-") || strings.HasPrefix(fields[1], "—")) {
				name := strings.TrimSpace(strings.TrimLeft(strings.Join(fields[1:], " "), "-— "))
				name = strings.Trim(name, "()")
				entry = ModelEntry{ID: fields[0], Name: strings.TrimSpace(name)}
			} else if len(fields) >= 1 {
				entry = ModelEntry{ID: fields[0]}
			}
		}

		if entry.ID == "" {
			continue
		}
		if entry.Provider == "" {
			entry.Provider = defaultProvider
		}
		out = append(out, entry)
	}
	return out
}

func parseJSONModelsOutput(trimmed, defaultProvider string) []ModelEntry {
	var strList []string
	if err := json.Unmarshal([]byte(trimmed), &strList); err == nil && len(strList) > 0 {
		out := make([]ModelEntry, 0, len(strList))
		for _, id := range strList {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			prov := defaultProvider
			name := ""
			if p, m, ok := strings.Cut(id, "/"); ok && p != "" && m != "" {
				prov = p
				name = m
			}
			out = append(out, ModelEntry{ID: id, Name: name, Provider: prov})
		}
		return out
	}

	type jsonModelItem struct {
		ID          string `json:"id"`
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Provider    string `json:"provider"`
	}
	var obj struct {
		Data   []jsonModelItem `json:"data"`
		Models []jsonModelItem `json:"models"`
	}
	if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
		items := obj.Data
		if len(items) == 0 {
			items = obj.Models
		}
		if len(items) > 0 {
			out := make([]ModelEntry, 0, len(items))
			for _, it := range items {
				id := strings.TrimSpace(it.ID)
				if id == "" {
					id = strings.TrimSpace(it.Slug)
				}
				if id == "" {
					continue
				}
				name := strings.TrimSpace(it.DisplayName)
				if name == "" {
					name = strings.TrimSpace(it.Name)
				}
				prov := strings.TrimSpace(it.Provider)
				if prov == "" {
					prov = defaultProvider
				}
				out = append(out, ModelEntry{ID: id, Name: name, Provider: prov})
			}
			return out
		}
	}
	return nil
}

func parseAgyModelsOutput(raw string) []ModelEntry {
	var out []ModelEntry
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if id, name, ok := strings.Cut(line, "\t"); ok {
			id = strings.TrimSpace(id)
			name = strings.TrimSpace(name)
			if id != "" {
				out = append(out, ModelEntry{ID: id, Name: name})
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			out = append(out, ModelEntry{ID: fields[0], Name: strings.Join(fields[1:], " ")})
		} else if len(fields) == 1 {
			out = append(out, ModelEntry{ID: fields[0]})
		}
	}
	return out
}

func parsePiModelsOutput(raw string) []ModelEntry {
	var out []ModelEntry
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if strings.EqualFold(fields[0], "provider") && strings.EqualFold(fields[1], "model") {
			continue
		}
		entry := ModelEntry{
			ID:       fields[0] + "/" + fields[1],
			Provider: fields[0],
			Name:     fields[1],
		}
		if len(fields) > 2 {
			entry.Context = fields[2]
		}
		if len(fields) > 3 {
			entry.MaxOut = fields[3]
		}
		if len(fields) > 4 {
			entry.Thinking = fields[4]
		}
		if len(fields) > 5 {
			entry.Images = fields[5]
		}
		out = append(out, entry)
	}
	return out
}

func parseOpenCodeModelsOutput(raw string) []ModelEntry {
	var out []ModelEntry
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if p, m, ok := strings.Cut(line, "/"); ok && p != "" && m != "" {
			out = append(out, ModelEntry{
				ID:       line,
				Provider: strings.TrimSpace(p),
				Name:     strings.TrimSpace(m),
			})
		} else {
			out = append(out, ModelEntry{ID: line})
		}
	}
	return out
}

func parseGenericModelsOutput(raw string) []ModelEntry {
	var out []ModelEntry
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if id, name, ok := strings.Cut(line, "\t"); ok {
			out = append(out, ModelEntry{ID: strings.TrimSpace(id), Name: strings.TrimSpace(name)})
		} else if p, m, ok := strings.Cut(line, "/"); ok && p != "" && m != "" && !strings.Contains(line, " ") {
			out = append(out, ModelEntry{ID: line, Provider: p, Name: m})
		} else {
			out = append(out, ModelEntry{ID: line})
		}
	}
	return out
}

func staticModelEntries(models []string) []ModelEntry {
	seen := make(map[string]bool)
	var out []ModelEntry
	for _, raw := range models {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			continue
		}
		seen[raw] = true
		entry := ModelEntry{ID: raw}
		if p, m, ok := strings.Cut(raw, "/"); ok && p != "" && m != "" {
			entry.Provider = p
			entry.Name = m
		}
		out = append(out, entry)
	}
	return out
}

func filterModelEntries(entries []ModelEntry, query string) []ModelEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return entries
	}
	var out []ModelEntry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.ID), q) ||
			strings.Contains(strings.ToLower(e.Name), q) ||
			strings.Contains(strings.ToLower(e.Provider), q) {
			out = append(out, e)
		}
	}
	return out
}
