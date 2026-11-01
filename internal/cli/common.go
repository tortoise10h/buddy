package cli

import (
	"fmt"

	"github.com/tortoise10h/buddy/internal/backend/rex"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

func getDefaultEngine() engine.Engine {
	rexBackend := rex.NewRexBackend()
	return engine.NewTerminalEngine(rexBackend)
}

func resolveTarget(project, task, flagSession, flagWindow, flagBlock string) (sessionID, windowID, blockID string, projName, taskSlug string, err error) {
	sessionID = flagSession
	windowID = flagWindow
	blockID = flagBlock
	projName = project
	taskSlug = task

	if sessionID != "" && (blockID != "" || windowID != "") {
		return sessionID, windowID, blockID, projName, taskSlug, nil
	}

	if project == "" {
		return "", "", "", "", "", fmt.Errorf("missing --project (-p) or explicit -s/-w/-b flags")
	}

	st, err := state.LoadState()
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("could not load state: %w", err)
	}

	proj, err := st.FindProject(project)
	if err != nil {
		return "", "", "", "", "", err
	}
	projName = proj.Name

	if sessionID == "" {
		sessionID = proj.RexSessionID
	}

	if task != "" {
		t, err := proj.FindTask(task)
		if err != nil {
			return "", "", "", "", "", err
		}
		taskSlug = t.TaskSlug
		if windowID == "" {
			windowID = t.WindowID
		}
		if blockID == "" {
			blockID = t.AgentBlockID
		}
	}

	return sessionID, windowID, blockID, projName, taskSlug, nil
}
