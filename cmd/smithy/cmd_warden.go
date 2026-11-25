package main

import "github.com/Zovaris/smithy/internal/warden"

func init() {
	register("doctor", "check everything a warden batch needs (--quiet: problems only; --fix [--yes]: apply fixes)", warden.DoctorUsage,
		"--quiet --fix --yes", warden.Doctor)
	register("wait-turn", "a launched warden worker waits for its prereqs and a free slot", warden.WaitTurnUsage,
		"--max=", warden.WaitTurn)
	register("brief", "render a warden worker's brief from batch.json", warden.BriefUsage,
		"--to= --subagent", warden.Brief)
	register("batch", "write, set or extend a warden batch.json (init, set, add)", warden.BatchUsage,
		"--repo= --base= --merge= --max= --risky= --cleanup= --mode= --id= --title= --intent= --speed=", warden.Batch)
}
