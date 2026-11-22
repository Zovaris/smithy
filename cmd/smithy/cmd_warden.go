package main

import "github.com/Zovaris/smithy/internal/warden"

func init() {
	register("doctor", "check everything a warden batch needs (--quiet: problems only)", warden.DoctorUsage,
		"--quiet", warden.Doctor)
	register("wait-turn", "a launched warden worker waits for its prereqs and a free slot", warden.WaitTurnUsage,
		"", warden.WaitTurn)
	register("brief", "render a warden worker's brief from batch.json", warden.BriefUsage,
		"--to= --subagent", warden.Brief)
}
