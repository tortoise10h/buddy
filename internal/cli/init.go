package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backends/rex"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/formatters"
	"github.com/tortoise10h/buddy/internal/state"
)

const defaultAgentsMD = `# Buddy Control Tower — System Instructions

You are **Buddy**, the central multi-project terminal orchestrator running in Rex.
Your purpose is to manage and coordinate 5–10+ concurrent software projects and feature tasks for the user from this single control session.

---

## The Golden Orchestrator Rules (CRITICAL)

> [!CAUTION]
> ### 1. ZERO-TOLERANCE DIRECT EDITING POLICY (RULE #1)
> Buddy is the **Central Control Tower and Dispatcher**, NOT a worker bee.
>
> - **NEVER edit, create, or delete project files, documentation, or code directly inside this Buddy session (` + "`~/.buddy/`" + `).**
> - **NEVER use ` + "`edit_file`" + `, ` + "`write_file`" + `, or file-modifying shell commands (` + "`sed`" + `, ` + "`echo >`" + `, ` + "`cat <<EOF`" + `) targeting project repositories.**
> - **MANDATORY DISPATCH**: For **ANY** editing, implementation, bug fix, or file change request, Buddy **MUST** dispatch a dedicated task tab via:
>   ` + "```sh\n   buddy-ctl task start --project <project> --task <slug> --prompt \"<exact instructions>\"\n   ```" + `
`

func newInitCmd() *cobra.Command {
	var (
		flagForce             bool
		flagProjectsRoot      []string
		flagProjectsRoots     string
		flagOrchestratorAgent string
		flagWorkerAgent       string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize ~/.buddy control tower directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			buddyDir := config.GetBuddyDir()
			hooksDir := filepath.Join(buddyDir, ".agents")
			if err := os.MkdirAll(buddyDir, 0755); err != nil {
				return err
			}
			if err := os.MkdirAll(hooksDir, 0755); err != nil {
				return err
			}

			var created []string

			hasExplicitRoots := len(flagProjectsRoot) > 0 || flagProjectsRoots != ""
			flagOrch := flagOrchestratorAgent
			if flagOrch == "" {
				flagOrch = os.Getenv("BUDDY_ORCHESTRATOR_AGENT")
			}
			flagWorker := flagWorkerAgent
			if flagWorker == "" {
				flagWorker = os.Getenv("BUDDY_WORKER_AGENT")
			}

			// Parse custom roots
			var rawRoots []string
			for _, item := range flagProjectsRoot {
				parts := strings.FieldsFunc(item, func(r rune) bool {
					return r == ':' || r == ',' || r == '\n'
				})
				for _, p := range parts {
					if strings.TrimSpace(p) != "" {
						rawRoots = append(rawRoots, strings.TrimSpace(p))
					}
				}
			}
			if flagProjectsRoots != "" {
				parts := strings.FieldsFunc(flagProjectsRoots, func(r rune) bool {
					return r == ':' || r == ',' || r == '\n'
				})
				for _, p := range parts {
					if strings.TrimSpace(p) != "" {
						rawRoots = append(rawRoots, strings.TrimSpace(p))
					}
				}
			}
			if len(rawRoots) == 0 {
				if envRoots := os.Getenv("BUDDY_PROJECTS_ROOTS"); envRoots != "" {
					parts := strings.FieldsFunc(envRoots, func(r rune) bool {
						return r == ':' || r == ',' || r == '\n'
					})
					for _, p := range parts {
						if strings.TrimSpace(p) != "" {
							rawRoots = append(rawRoots, strings.TrimSpace(p))
						}
					}
				}
			}

			var customRoots []string
			seenRoots := make(map[string]bool)
			for _, r := range rawRoots {
				expanded := r
				if strings.HasPrefix(expanded, "~/") {
					home, _ := os.UserHomeDir()
					expanded = filepath.Join(home, expanded[2:])
				}
				abs, err := filepath.Abs(expanded)
				if err != nil {
					abs = expanded
				}
				if !seenRoots[abs] {
					seenRoots[abs] = true
					customRoots = append(customRoots, abs)
				}
			}

			// 1. config.json
			configPath := filepath.Join(buddyDir, "config.json")
			configExists := false
			if fi, err := os.Stat(configPath); err == nil && !fi.IsDir() {
				configExists = true
			}

			if !configExists || flagForce {
				rawCfg, _ := config.LoadRawConfig()
				if len(customRoots) > 0 {
					rawCfg["projects_roots"] = customRoots
				}
				if flagOrch != "" {
					rawCfg["orchestrator_agent"] = flagOrch
				}
				if flagWorker != "" {
					rawCfg["worker_agent"] = flagWorker
				}
				_ = config.SaveRawConfig(rawCfg)
				created = append(created, "config.json")
			} else {
				rawCfg, _ := config.LoadRawConfig()
				changed := false
				var updates []string
				if hasExplicitRoots && len(customRoots) > 0 {
					rawCfg["projects_roots"] = customRoots
					changed = true
					updates = append(updates, "projects_roots")
				}
				if flagOrch != "" && rawCfg["orchestrator_agent"] != flagOrch {
					rawCfg["orchestrator_agent"] = flagOrch
					changed = true
					updates = append(updates, "orchestrator_agent")
				}
				if flagWorker != "" && rawCfg["worker_agent"] != flagWorker {
					rawCfg["worker_agent"] = flagWorker
					changed = true
					updates = append(updates, "worker_agent")
				}
				if changed {
					_ = config.SaveRawConfig(rawCfg)
					created = append(created, fmt.Sprintf("config.json (updated %s)", strings.Join(updates, ", ")))
				}
			}

			// 2. state.json
			statePath := filepath.Join(buddyDir, "state.json")
			stateExists := false
			if fi, err := os.Stat(statePath); err == nil && !fi.IsDir() {
				stateExists = true
			}

			rawState, _ := state.LoadRawState()
			if !stateExists || flagForce {
				_ = state.SaveRawState(rawState)
				created = append(created, "state.json")
			}

			// 3. AGENTS.md
			repoRoot := FindBuddyRepoPath()
			agentsMDPath := filepath.Join(buddyDir, "AGENTS.md")
			var repoAgents string
			if repoRoot != "" {
				repoAgents = filepath.Join(repoRoot, "AGENTS.md")
			}

			if repoAgents != "" && fileExists(repoAgents) {
				isSymlink := isPathSymlink(agentsMDPath)
				if isSymlink || fileExists(agentsMDPath) {
					if flagForce || !isSymlink {
						_ = os.Remove(agentsMDPath)
						_ = os.Symlink(repoAgents, agentsMDPath)
						created = append(created, "AGENTS.md -> "+repoAgents)
					}
				} else {
					_ = os.Symlink(repoAgents, agentsMDPath)
					created = append(created, "AGENTS.md -> "+repoAgents)
				}
			} else if !fileExists(agentsMDPath) || flagForce {
				_ = os.WriteFile(agentsMDPath, []byte(defaultAgentsMD), 0644)
				created = append(created, "AGENTS.md")
			}

			// 4. .agents/hooks.json
			hooksPath := filepath.Join(hooksDir, "hooks.json")
			var repoHooks string
			if repoRoot != "" {
				repoHooks = filepath.Join(repoRoot, ".agents", "hooks.json")
			}

			if repoHooks != "" && fileExists(repoHooks) {
				isSymlink := isPathSymlink(hooksPath)
				if isSymlink || fileExists(hooksPath) {
					if flagForce || !isSymlink {
						_ = os.Remove(hooksPath)
						_ = os.Symlink(repoHooks, hooksPath)
						created = append(created, ".agents/hooks.json -> "+repoHooks)
					}
				} else {
					_ = os.Symlink(repoHooks, hooksPath)
					created = append(created, ".agents/hooks.json -> "+repoHooks)
				}
			} else if !fileExists(hooksPath) || flagForce {
				home, _ := os.UserHomeDir()
				hooksConfig := map[string]any{
					"buddy-state-reminder": map[string]any{
						"PreInvocation": []map[string]any{
							{
								"type":    "command",
								"command": fmt.Sprintf("python3 %s context-inject", filepath.Join(home, ".local", "bin", "buddy-ctl")),
								"timeout": 5,
							},
						},
					},
				}
				data, _ := json.MarshalIndent(hooksConfig, "", "  ")
				data = append(data, '\n')
				_ = os.WriteFile(hooksPath, data, 0644)
				created = append(created, ".agents/hooks.json")
			}

			// 5. Check if Rex Buddy session already exists
			rexBackend := rex.NewRexBackend()
			ctx := context.Background()
			sessions, err := rexBackend.ListSessions(ctx)
			if err == nil {
				cfg := config.LoadConfig()
				targetLabel := strings.ToLower(cfg.BuddySessionName)
				if targetLabel == "" {
					targetLabel = "buddy"
				}
				for _, s := range sessions {
					if strings.ToLower(s.Name) == targetLabel {
						rawState["buddy_session_id"] = s.SessionID
						_ = state.SaveRawState(rawState)
						break
					}
				}
			}

			type InitOutput struct {
				Status         string   `json:"status"`
				BuddyDir       string   `json:"buddy_dir"`
				CreatedFiles   []string `json:"created_files"`
				BuddySessionID any      `json:"buddy_session_id"`
			}

			out := InitOutput{
				Status:         "ok",
				BuddyDir:       buddyDir,
				CreatedFiles:   created,
				BuddySessionID: rawState["buddy_session_id"],
			}

			jsonStr, err := formatters.FormatJSON(out)
			if err != nil {
				return err
			}
			fmt.Println(jsonStr)
			return nil
		},
	}

	cmd.Flags().BoolVar(&flagForce, "force", false, "Force overwrite of configuration and state files")
	cmd.Flags().StringSliceVar(&flagProjectsRoot, "projects-root", nil, "Project root directory (can be specified multiple times)")
	cmd.Flags().StringVar(&flagProjectsRoots, "projects-roots", "", "Project root directories as colon-, comma-, or space-separated list")
	cmd.Flags().StringVar(&flagOrchestratorAgent, "orchestrator-agent", "", "Agent harness for orchestrator role")
	cmd.Flags().StringVar(&flagWorkerAgent, "worker-agent", "", "Agent harness for worker role")

	return cmd
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func isPathSymlink(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && (fi.Mode()&os.ModeSymlink != 0)
}
