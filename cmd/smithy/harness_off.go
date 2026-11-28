//go:build !harness

package main

import (
	"fmt"
	"io"
	"os"
)

// harnessBuild is false in the skills build (docs/adr/0009).
const harnessBuild = false

// harnessOnly are the commands only the harness build carries; here they say how to get it.
var harnessOnly = []string{"daemon", "attach", "board", "chat", "web", "mcp", "stop", "pair", "servers", "welcome", "workflow"}

func init() {
	for _, name := range harnessOnly {
		name := name
		register(name, harnessSummary[name], "usage: smithy "+name+"\nsmithy "+name+" needs the harness build of smithy.", "--", func([]string) int {
			return needsHarness(os.Stderr, name)
		})
	}
}

func needsHarness(w io.Writer, name string) int {
	fmt.Fprintf(w, "smithy %s needs the harness build; this is the skills build.\n", name)
	fmt.Fprintln(w, "Install the release binary with: smithy update, or build it: go build -tags harness ./cmd/smithy")
	return 2
}
