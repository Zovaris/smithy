package main

import (
	"os"

	"github.com/Zovaris/smithy/internal/pool"
)

func init() {
	register("pool", "your own agent accounts for smithy agent: add, login, list, remove, clear", pool.Usage, "",
		func(args []string) int { return pool.Main(args, os.Stdout, os.Stderr) })
	for _, s := range []string{"add", "login", "list", "remove", "clear"} {
		registerSub("pool", s, "")
	}
}
