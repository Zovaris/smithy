package main

import (
	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/decisions"
)

func init() {
	register("agent", "run one headless agent CLI on a prompt file", agents.AgentUsage,
		"--bg --readonly --cwd= --log= --model= --effort= --timeout=", agents.AgentMain)
	register("consult", "ask the strongest model to settle one decision", agents.ConsultUsage,
		"--context=", agents.ConsultMain)
	register("decisions", "the log of the user's decisions taken without them", decisions.Usage,
		"--why= --by= --ref= --all", decisions.Main)
	register("route", "pick the model tier for a task", agents.RouteUsage,
		"--text= --file= --kind= --agent= --speed= --domain= --board= --batch --json", agents.RouteMain)
	register("skills", "find installed skills (smithy skills --list)", agents.SkillsUsage,
		"--list", agents.SkillsMain)
	register("setup", "use-smithy setup: installed skills, settings, instructions block", agents.SetupUsage,
		"", agents.SetupMain)
}
