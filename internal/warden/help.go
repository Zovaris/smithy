package warden

// Usage texts for `smithy <command> --help`, printed by the dispatcher.
var (
	WaitTurnUsage = waitUsage
	BriefUsage    = briefUsage
	BatchUsage    = batchUsage
	DoctorUsage   = "usage: smithy doctor [--quiet] [--fix [--yes]]\nChecks everything a warden batch needs, one line per check; --quiet prints problems only.\n--fix lists each fix that changes only your own files (exact change shown) and applies it on y;\n--yes applies all but permission grants (those need y); without a terminal and without --yes it applies none.\nExit 1 when a check failed (the checks run before any fix)."
)
