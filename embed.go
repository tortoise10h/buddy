package buddy

import _ "embed"

//go:embed templates/orchestrator-AGENTS.md
var OrchestratorAgentsMD []byte

//go:embed templates/hooks.json
var HooksJSONTemplate []byte

//go:embed skills/buddy/SKILL.md
var SkillMD []byte
