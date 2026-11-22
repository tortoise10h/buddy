package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/setup"
)

func newSetupCmd() *cobra.Command {
	var (
		nonInteractive    bool
		upgrade           bool
		dryRun            bool
		force             bool
		jsonOutput        bool
		accessible        bool
		only              []string
		devLink           string
		skipSkills        bool
		skipHooks         bool
		buddyCtlPath      string
		projectsRoots     []string
		replaceRoots      bool
		defaultEngine     string
		orchestratorAgent string
		workerAgent       string
		t3Provider        string
		t3Model           string
		opencodeURL       string
		opencodeAgent     string
		opencodeModel     string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive onboarding, reconfiguration, and asset provisioning for Buddy",
		Long: `Configure Buddy project roots, execution engines, agent harnesses, ~/.buddy/ control tower assets, and agent skills.

Runs an interactive TUI wizard when attached to a terminal, or non-interactively with --yes / --non-interactive / --upgrade / BUDDY_NONINTERACTIVE=1.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			baseCtx := cmd.Context()
			if baseCtx == nil {
				baseCtx = context.Background()
			}
			ctx, stop := signal.NotifyContext(baseCtx, os.Interrupt, syscall.SIGTERM)
			defer stop()

			isTTY := isTerminal(os.Stdin) && isTerminal(os.Stdout)
			envNonInteractive := os.Getenv("BUDDY_NONINTERACTIVE") == "1" ||
				strings.EqualFold(os.Getenv("BUDDY_NONINTERACTIVE"), "true") ||
				strings.EqualFold(os.Getenv("CI"), "true")

			effectiveNonInteractive := nonInteractive || upgrade || jsonOutput || envNonInteractive || !isTTY

			effectiveAccessible := accessible ||
				os.Getenv("ACCESSIBLE") == "1" ||
				strings.EqualFold(os.Getenv("ACCESSIBLE"), "true") ||
				os.Getenv("TERM") == "dumb"

			var prompter setup.Prompter
			if effectiveNonInteractive {
				prompter = setup.NewNonInteractivePrompter()
			} else {
				prompter = setup.NewHuhPrompter(os.Stdin, cmd.OutOrStdout(), effectiveAccessible)
			}

			res, err := setup.Run(ctx, setup.Options{
				Version:           Version,
				NonInteractive:    effectiveNonInteractive,
				Upgrade:           upgrade,
				DryRun:            dryRun,
				Force:             force,
				Only:              only,
				DevLink:           devLink,
				SkipSkills:        skipSkills,
				SkipHooks:         skipHooks,
				BuddyCtlPath:      buddyCtlPath,
				ProjectsRoots:     projectsRoots,
				ReplaceRoots:      replaceRoots,
				DefaultEngine:     defaultEngine,
				OrchestratorAgent: orchestratorAgent,
				WorkerAgent:       workerAgent,
				T3Provider:        t3Provider,
				T3Model:           t3Model,
				OpenCodeURL:       opencodeURL,
				OpenCodeAgent:     opencodeAgent,
				OpenCodeModel:     opencodeModel,
				Out:               cmd.OutOrStdout(),
				Prompter:          prompter,
			})
			if err != nil {
				if errors.Is(err, setup.ErrAborted) {
					fmt.Fprintln(cmd.ErrOrStderr(), "\nℹ Setup cancelled — no configuration changes were made.")
					return nil
				}
				return err
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			}

			printSetupSummary(cmd.OutOrStdout(), res)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&nonInteractive, "yes", "y", false, "Run without interactive prompts (uses flags, env vars, existing config, or defaults)")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Alias for --yes")
	cmd.Flags().BoolVar(&upgrade, "upgrade", false, "Non-interactive asset and schema refresh for existing installs (preserves user choices)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview planned changes without writing any files")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite user-modified managed files (after creating .bak-<timestamp> backup)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output machine-readable JSON summary")
	cmd.Flags().BoolVar(&accessible, "accessible", false, "Enable screen-reader / plain-text accessible prompt mode")
	cmd.Flags().StringSliceVar(&only, "only", nil, "Run only specific sections: projects, engines, agents, skills, assets")

	cmd.Flags().StringVar(&devLink, "dev-link", "", "Symlink AGENTS.md and skills/buddy to a local Buddy git checkout instead of copying embedded assets")
	cmd.Flags().BoolVar(&skipSkills, "skip-skills", false, "Skip installing or updating agent skill files")
	cmd.Flags().BoolVar(&skipHooks, "skip-hooks", false, "Skip generating ~/.buddy/.agents/hooks.json")
	cmd.Flags().StringVar(&buddyCtlPath, "buddy-ctl-path", "", "Override buddy-ctl path written into hooks.json")

	cmd.Flags().StringArrayVar(&projectsRoots, "projects-root", nil, "Project root directory (repeatable)")
	cmd.Flags().BoolVar(&replaceRoots, "replace-roots", false, "Replace existing projects_roots instead of merging")
	cmd.Flags().StringVar(&defaultEngine, "default-engine", "", "Default fallback execution engine (rex, wezterm, t3, opencode)")
	cmd.Flags().StringVar(&orchestratorAgent, "orchestrator-agent", "", "Default terminal orchestrator harness (e.g. agy, opencode)")
	cmd.Flags().StringVar(&workerAgent, "worker-agent", "", "Default terminal worker harness (e.g. agy, pi)")
	cmd.Flags().StringVar(&t3Provider, "t3-provider", "", "Default T3 provider")
	cmd.Flags().StringVar(&t3Model, "t3-model", "", "Default T3 model (or <provider>/<model>)")
	cmd.Flags().StringVar(&opencodeURL, "opencode-url", "", "Optional OpenCode server URL override")
	cmd.Flags().StringVar(&opencodeAgent, "opencode-agent", "", "Optional OpenCode default agent mode")
	cmd.Flags().StringVar(&opencodeModel, "opencode-model", "", "Optional OpenCode default model (<provider>/<model>)")

	return cmd
}

func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func printSetupSummary(w interface{ Write([]byte) (int, error) }, res *setup.Result) {
	home, _ := os.UserHomeDir()
	buddyDirDisplay := setup.FormatHomePath(res.BuddyDir, home)
	configPathDisplay := setup.FormatHomePath(res.ConfigPath, home)
	if configPathDisplay == "" {
		configPathDisplay = buddyDirDisplay + "/config.json"
	}

	if res.DryRun {
		fmt.Fprintln(w, "ℹ Dry run complete (no files were written):")
	} else if res.Mode == "upgrade" {
		fmt.Fprintln(w, "✔ Buddy assets refreshed!")
	} else {
		fmt.Fprintln(w, "✔ Buddy setup complete!")
	}

	if len(res.SelectedEngines) > 0 {
		fmt.Fprintf(w, "  • Platforms:          %s (default: %s)\n", strings.Join(res.SelectedEngines, ", "), res.DefaultEngine)
	} else {
		fmt.Fprintf(w, "  • Default engine:     %s\n", res.DefaultEngine)
	}
	fmt.Fprintf(w, "  • Orchestrator agent: %s\n", res.OrchestratorAgent)
	fmt.Fprintf(w, "  • Worker agent:       %s\n", res.WorkerAgent)
	if res.T3Provider != "" && res.T3Model != "" {
		fmt.Fprintf(w, "  • T3 worker model:    %s/%s\n", res.T3Provider, res.T3Model)
	}
	if res.OpenCodeModel != "" {
		fmt.Fprintf(w, "  • OpenCode model:     %s\n", res.OpenCodeModel)
	}
	notifLabel := "disabled"
	if res.NotificationsEnabled {
		notifLabel = "enabled"
	}
	fmt.Fprintf(w, "  • Notifications:      %s\n", notifLabel)
	fmt.Fprintf(w, "  • Project roots:      %d root(s), %d repo(s) discovered\n", len(res.ProjectsRoots), res.DiscoveredRepos)

	fmt.Fprintln(w, "\nFiles:")
	fmt.Fprintf(w, "  • Buddy directory:    %s\n", buddyDirDisplay)
	fmt.Fprintf(w, "  • Config file:        %s (edit anytime)\n", configPathDisplay)

	if res.Provision != nil {
		for _, a := range res.Provision.Actions {
			icon := "✔"
			if a.Status == "preserved" || a.Status == "skipped" {
				icon = "ℹ"
			}
			line := fmt.Sprintf("  %s %s (%s)", icon, setup.FormatHomePath(a.Path, home), a.Status)
			if a.Detail != "" {
				line += " — " + a.Detail
			}
			fmt.Fprintln(w, line)
		}
	}

	if res.PathWarning != "" {
		fmt.Fprintf(w, "\n⚠ %s\n", res.PathWarning)
	}

	fmt.Fprintln(w, "\nNext steps:")
	fmt.Fprintln(w, "  • Start Buddy:        buddy")
	fmt.Fprintln(w, "  • Reconfigure setup:  buddy-ctl setup")
	fmt.Fprintln(w, "  • Verify environment: buddy-ctl doctor")
}
