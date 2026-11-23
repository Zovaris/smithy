package main

import "github.com/Zovaris/smithy/internal/auto"

func init() {
	register("auto", "autonomous intake: poll issue sources, take queued issues, report back", auto.Usage, "", auto.Main)
	registerSub("auto", "poll", "--source=")
	registerSub("auto", "take", "--max=")
	registerSub("auto", "report", "--pr=")
	registerSub("auto", "status", "--json")
}
