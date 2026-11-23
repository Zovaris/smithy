package main

import (
	"os"

	"github.com/Zovaris/smithy/internal/notify"
)

func init() {
	register("notify", "Telegram notifications from smithy auto serve: test, status", notify.Usage, "",
		func(args []string) int { return notify.Main(args, os.Stdout, os.Stderr) })
	registerSub("notify", "test", "")
	registerSub("notify", "status", "")
}
