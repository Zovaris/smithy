package main

import (
	"os"

	"github.com/Zovaris/smithy/internal/proxy"
)

func init() {
	register("proxy", "egress proxy with bound secrets, run natively: serve, approve, deny", proxy.Usage, "",
		func(args []string) int { return proxy.Main(args, os.Stdout, os.Stderr) })
	registerSub("proxy", "serve", "--addr= --allow= --ask-timeout=")
	registerSub("proxy", "approve", "")
	registerSub("proxy", "deny", "")
}
