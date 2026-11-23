package main

import (
	"os"

	"github.com/Zovaris/smithy/internal/auth"
)

func init() {
	register("auth", "source API keys in ~/.smithy/auth.json: set (stdin only), list, remove", auth.Usage, "",
		func(args []string) int { return auth.Main(args, os.Stdin, os.Stdout, os.Stderr) })
	registerSub("auth", "set", "")
	registerSub("auth", "list", "")
	registerSub("auth", "remove", "")
}
