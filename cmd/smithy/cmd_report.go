package main

import (
	"os"

	"github.com/Zovaris/smithy/internal/report"
)

func init() {
	register("report", "weekly report: shipped, failed, time and cost per harness, open decisions", report.Usage, "",
		func(args []string) int { return report.Main(args, os.Stdout, os.Stderr) })
	registerSub("report", "week", "")
}
