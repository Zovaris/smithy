package warden

// Usage texts for `smithy <command> --help`, printed by the dispatcher.
var (
	WaitTurnUsage = waitUsage
	BriefUsage    = briefUsage
	DoctorUsage   = "usage: smithy doctor [--quiet]\nChecks everything a warden batch needs, one line per check; --quiet prints problems only.\nExit 1 when a check failed."
)
