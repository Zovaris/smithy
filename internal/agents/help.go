package agents

import "github.com/Zovaris/smithy/internal/agents/skills"

// Usage texts for `smithy <command> --help`, printed by the dispatcher.
var (
	AgentUsage   = agentUsage
	ConsultUsage = consultUsage
	RouteUsage   = routeUsage
	SkillsUsage  = skills.Usage
	SetupUsage   = setupUsage
)
