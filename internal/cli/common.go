package cli

import (
	"fmt"
	"os"
	"regexp"
	"sort"
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

type activeTaskSummary struct {
	Slug    string `json:"slug"`
	Status  string `json:"status"`
	Origin  string `json:"origin,omitempty"`
	Summary string `json:"summary"`
}

type candidateCollision struct {
	Kind       string `json:"kind"` // "tracked" or "untracked"
	Slug       string `json:"slug"`
	WindowID   string `json:"window_id,omitempty"`
	Status     string `json:"status"`
	Origin     string `json:"origin,omitempty"`
	Summary    string `json:"summary"`
	ExactMatch bool   `json:"exact_match,omitempty"`
}

func isLiveOpenTaskStatus(status string) bool {
	switch status {
	case "running", "planning", "waiting_for_input", "ready_for_demo", "error":
		return true
	default:
		return false
	}
}

func isActivelyWorkingTaskStatus(status string) bool {
	switch status {
	case "running", "planning":
		return true
	default:
		return false
	}
}

// computeRecommendedWatch returns the smallest-scope buddy-ctl watch command
// covering all currently running/planning tasks in st.
func computeRecommendedWatch(st *state.State) string {
	if st == nil {
		return ""
	}
	var activeProjects []string
	var singleProj, singleTask string
	totalRunning := 0

	projNames := make([]string, 0, len(st.Projects))
	for pName := range st.Projects {
		projNames = append(projNames, pName)
	}
	sort.Strings(projNames)

	for _, pName := range projNames {
		pData := st.Projects[pName]
		projRunning := 0
		taskNames := make([]string, 0, len(pData.Tasks))
		for tSlug := range pData.Tasks {
			taskNames = append(taskNames, tSlug)
		}
		sort.Strings(taskNames)
		for _, tSlug := range taskNames {
			tData := pData.Tasks[tSlug]
			if isActivelyWorkingTaskStatus(tData.Status) {
				projRunning++
				totalRunning++
				singleProj = pName
				singleTask = tSlug
			}
		}
		if projRunning > 0 {
			activeProjects = append(activeProjects, pName)
		}
	}

	if totalRunning == 0 {
		return ""
	}
	if len(activeProjects) == 1 {
		if totalRunning == 1 {
			return fmt.Sprintf("buddy-ctl watch --project %s --task %s --interval 5 --timeout 600", singleProj, singleTask)
		}
		return fmt.Sprintf("buddy-ctl watch --project %s --interval 5 --timeout 600", activeProjects[0])
	}
	return "buddy-ctl watch --all --interval 5 --timeout 600"
}

// collectActiveTasksInProject returns sorted summaries of open tasks in projName.
func collectActiveTasksInProject(st *state.State, projName string) []activeTaskSummary {
	if st == nil {
		return nil
	}
	p, ok := st.Projects[projName]
	if !ok {
		return nil
	}
	slugs := make([]string, 0, len(p.Tasks))
	for slug := range p.Tasks {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	var out []activeTaskSummary
	for _, slug := range slugs {
		t := p.Tasks[slug]
		if !isLiveOpenTaskStatus(t.Status) {
			continue
		}
		out = append(out, activeTaskSummary{
			Slug:    t.TaskSlug,
			Status:  t.Status,
			Origin:  t.Origin,
			Summary: cleanTaskSummary(t),
		})
	}
	return out
}

func cleanTaskSummary(t state.TaskState) string {
	s := strings.TrimSpace(t.LastSummary)
	if (s == "" || s == "Started via CLI") && t.InitialPrompt != nil {
		s = strings.TrimSpace(*t.InitialPrompt)
		if len(s) > 80 {
			s = s[:77] + "..."
		}
	}
	return s
}

var domainTokenRe = regexp.MustCompile(`[a-z0-9]+`)
var hexSuffixRe = regexp.MustCompile(`^[0-9a-f]{4}$`)

var genericTaskStopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "for": true, "from": true, "has": true, "in": true,
	"is": true, "it": true, "its": true, "of": true, "on": true, "or": true,
	"that": true, "the": true, "to": true, "was": true, "were": true, "will": true,
	"with": true, "this": true, "please": true, "can": true, "you": true, "help": true,
	"add": true, "fix": true, "update": true, "implement": true, "change": true,
	"create": true, "make": true, "build": true, "refactor": true, "remove": true,
	"delete": true, "check": true, "test": true, "tests": true, "feat": true,
	"feature": true, "bug": true, "task": true, "issue": true, "work": true,
	"code": true, "file": true, "files": true, "project": true, "repo": true,
	"plan": true, "only": true, "step": true, "new": true, "use": true, "run": true,
}

func extractDomainTokens(slug, text, projectName string) map[string]bool {
	tokens := make(map[string]bool)
	projLower := strings.ToLower(strings.TrimSpace(projectName))
	combined := strings.ToLower(slug + " " + text)
	for _, tok := range domainTokenRe.FindAllString(combined, -1) {
		if len(tok) < 3 {
			continue
		}
		if tok == projLower || genericTaskStopWords[tok] || hexSuffixRe.MatchString(tok) {
			continue
		}
		tokens[tok] = true
	}
	return tokens
}

// findCandidateCollisions checks open tracked tasks and untracked windows in projName
// for an exact slug/label collision or meaningful domain-token overlap.
func findCandidateCollisions(st *state.State, projName, newSlug, newPrompt string) []candidateCollision {
	if st == nil {
		return nil
	}
	p, ok := st.Projects[projName]
	if !ok {
		return nil
	}

	newSlugLower := strings.ToLower(strings.TrimSpace(newSlug))
	newSlugTokens := extractDomainTokens(newSlug, "", projName)
	newAllTokens := extractDomainTokens(newSlug, newPrompt, projName)

	slugs := make([]string, 0, len(p.Tasks))
	for s := range p.Tasks {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)

	var candidates []candidateCollision
	for _, s := range slugs {
		t := p.Tasks[s]
		if !isLiveOpenTaskStatus(t.Status) {
			continue
		}
		exact := strings.ToLower(t.TaskSlug) == newSlugLower
		overlap := false
		if !exact {
			candSlugTokens := extractDomainTokens(t.TaskSlug, "", projName)
			candAllTokens := extractDomainTokens(t.TaskSlug, cleanTaskSummary(t), projName)

			sharedSlug := 0
			for tok := range newSlugTokens {
				if candSlugTokens[tok] {
					sharedSlug++
				}
			}
			sharedAll := 0
			for tok := range newAllTokens {
				if candAllTokens[tok] {
					sharedAll++
				}
			}
			// Trigger candidate match when slugs share a non-generic domain token,
			// or when slug+summary share >= 2 domain tokens.
			if sharedSlug >= 1 || sharedAll >= 2 {
				overlap = true
			}
		}

		if exact || overlap {
			candidates = append(candidates, candidateCollision{
				Kind:       "tracked",
				Slug:       t.TaskSlug,
				WindowID:   t.WindowID,
				Status:     t.Status,
				Origin:     t.Origin,
				Summary:    cleanTaskSummary(t),
				ExactMatch: exact,
			})
		}
	}

	for _, uw := range p.UntrackedWindows {
		labelLower := strings.ToLower(strings.TrimSpace(uw.Label))
		exact := labelLower != "" && labelLower == newSlugLower
		overlap := false
		if !exact && labelLower != "" {
			uwTokens := extractDomainTokens(uw.Label, uw.Summary, projName)
			shared := 0
			for tok := range newSlugTokens {
				if uwTokens[tok] {
					shared++
				}
			}
			if shared >= 1 {
				overlap = true
			}
		}
		if exact || overlap {
			candidates = append(candidates, candidateCollision{
				Kind:       "untracked",
				Slug:       uw.Label,
				WindowID:   uw.WindowID,
				Status:     uw.DetectedStatus,
				Origin:     "user",
				Summary:    uw.Summary,
				ExactMatch: exact,
			})
		}
	}

	return candidates
}

func formatCandidateCollisionError(projName, requestedSlug string, candidates []candidateCollision) error {
	var b strings.Builder
	fmt.Fprintf(&b, "candidate active tab(s) already open in project %q matching %q:\n", projName, requestedSlug)
	for _, c := range candidates {
		tag := c.Status
		if c.Kind == "untracked" {
			tag = fmt.Sprintf("untracked/%s, window=%s", c.Status, c.WindowID)
		} else if c.Origin == "user" {
			tag = fmt.Sprintf("%s, user-adopted", c.Status)
		}
		fmt.Fprintf(&b, "  • %s/%s [%s]: %s\n", projName, c.Slug, tag, c.Summary)
	}
	b.WriteString("ACTION REQUIRED (3-Tier Confidence Rule):\n")
	b.WriteString("  1. Strong/unambiguous match (same feature & intention): reuse via 'buddy-ctl send -p " + projName + " -t <task> --text ...' (or 'buddy-ctl task adopt' if untracked).\n")
	b.WriteString("  2. Uncertain/related match: DO NOT decide on the user's behalf. Surface the candidate slug(s), status, and summary above to the user and ask whether to reuse the existing tab or start a new one.\n")
	b.WriteString("  3. Confirmed new tab: re-run with '--force' only after explicit user confirmation to start a separate tab.")
	return fmt.Errorf("%s", b.String())
}
