package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/tortoise10h/buddy/internal/backend/rex"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

var defaultRegistry *engine.Registry

func getRegistry() *engine.Registry {
	if defaultRegistry == nil {
		cfg, _ := config.LoadConfig()
		defaultEngine := "rex"
		if cfg != nil && cfg.DefaultEngine != "" {
			defaultEngine = cfg.DefaultEngine
		}
		defaultRegistry = engine.NewRegistry(defaultEngine)

		// Register Rex terminal engine
		rexBackend := rex.NewRexBackend()
		defaultRegistry.Register("rex", engine.NewTerminalEngine(rexBackend))

		// Register T3 engine
		defaultRegistry.Register("t3", engine.NewT3Engine())
	}
	return defaultRegistry
}

func getEngine(name string) (engine.Engine, error) {
	return getRegistry().Get(name)
}

// resolveWorkerForEngine resolves the worker label and command arguments for the target engine.
// For "t3", it resolves <provider>/<model> from --agent, BUDDY_T3_MODEL, or cfg.Engines["t3"].
// For terminal engines, it resolves the CLI harness from --agent, BUDDY_WORKER_AGENT, or cfg.Engines[engine].
func resolveWorkerForEngine(engineName, flagAgent string, cfg *config.Config) (string, []string, error) {
	if engineName == "t3" {
		if flagAgent != "" {
			p, m, ok := strings.Cut(strings.TrimSpace(flagAgent), "/")
			if !ok || p == "" || m == "" {
				return "", nil, fmt.Errorf("t3 engine requires <provider>/<model> for --agent (got %q), e.g. antigravity/gemini-3.8-flash-medium", flagAgent)
			}
			spec := p + "/" + m
			return spec, []string{spec}, nil
		}
		if env := strings.TrimSpace(os.Getenv("BUDDY_T3_MODEL")); env != "" {
			p, m, ok := strings.Cut(env, "/")
			if !ok || p == "" || m == "" {
				return "", nil, fmt.Errorf("invalid BUDDY_T3_MODEL %q: expected <provider>/<model>", env)
			}
			spec := p + "/" + m
			return spec, []string{spec}, nil
		}
		if cfg != nil {
			p, m := cfg.GetT3ProviderModel()
			if p != "" && m != "" {
				spec := p + "/" + m
				return spec, []string{spec}, nil
			}
		}
		return "", nil, fmt.Errorf("t3 dispatch: missing provider/model. Set engines.t3.provider and engines.t3.model in ~/.buddy/config.json (via 'buddy-ctl config set'), pass --agent <provider>/<model>, or set BUDDY_T3_MODEL=<provider>/<model>")
	}

	agentName := strings.TrimSpace(flagAgent)
	if agentName == "" {
		agentName = strings.TrimSpace(os.Getenv("BUDDY_WORKER_AGENT"))
	}
	if agentName == "" && cfg != nil {
		agentName = cfg.GetEffectiveWorkerAgent(engineName)
	}
	if agentName == "" {
		agentName = "agy"
	}
	if cfg != nil {
		agentCmd, _, _ := cfg.GetAgentCommand(agentName)
		return agentName, agentCmd, nil
	}
	return agentName, []string{agentName}, nil
}

func resolveActiveEngine(flagEngine string) string {
	cfg, _ := config.LoadConfig()
	return engine.ResolveEngine(flagEngine, os.Getenv, cfg).EngineName
}

func resolveTarget(project, task, flagSession, flagWindow, flagBlock string) (sessionID, windowID, blockID string, projName, taskSlug, engineName string, err error) {
	return resolveTargetWithEngine(project, task, flagSession, flagWindow, flagBlock, "")
}

func resolveTargetWithEngine(project, task, flagSession, flagWindow, flagBlock, flagEngine string) (sessionID, windowID, blockID string, projName, taskSlug, engineName string, err error) {
	sessionID = flagSession
	windowID = flagWindow
	blockID = flagBlock
	projName = project
	taskSlug = task
	engineName = resolveActiveEngine(flagEngine)

	if sessionID != "" && (blockID != "" || windowID != "") {
		return sessionID, windowID, blockID, projName, taskSlug, engineName, nil
	}

	if project == "" {
		return "", "", "", "", "", "", fmt.Errorf("missing --project (-p) or explicit -s/-w/-b flags")
	}

	st, err := state.LoadStateForEngine(engineName)
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("could not load state: %w", err)
	}

	proj, err := st.FindProject(project)
	if err != nil {
		return "", "", "", "", "", "", err
	}
	projName = proj.Name

	if sessionID == "" {
		sessionID = proj.RexSessionID
	}

	if task != "" {
		t, err := proj.FindTask(task)
		if err != nil {
			return "", "", "", "", "", "", err
		}
		taskSlug = t.TaskSlug
		engineName = t.GetEngine()
		if windowID == "" {
			windowID = t.WindowID
		}
		if blockID == "" {
			blockID = t.AgentBlockID
		}
	}

	return sessionID, windowID, blockID, projName, taskSlug, engineName, nil
}
