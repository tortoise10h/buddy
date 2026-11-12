package cli

import (
	"fmt"

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

		// Register T3 stub engine
		defaultRegistry.Register("t3", engine.NewStubT3Engine())
	}
	return defaultRegistry
}

func getEngine(name string) (engine.Engine, error) {
	return getRegistry().Get(name)
}

func resolveTarget(project, task, flagSession, flagWindow, flagBlock string) (sessionID, windowID, blockID string, projName, taskSlug, engineName string, err error) {
	sessionID = flagSession
	windowID = flagWindow
	blockID = flagBlock
	projName = project
	taskSlug = task
	engineName = "rex"

	if sessionID != "" && (blockID != "" || windowID != "") {
		return sessionID, windowID, blockID, projName, taskSlug, engineName, nil
	}

	if project == "" {
		return "", "", "", "", "", "", fmt.Errorf("missing --project (-p) or explicit -s/-w/-b flags")
	}

	st, err := state.LoadState()
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
