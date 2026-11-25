//go:build !harness

package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
)

// harnessBuild is false in the skills build (docs/adr/0009).
const harnessBuild = false

// harnessOnly are the commands only the harness build carries; here they say how to get it.
var harnessOnly = []string{"daemon", "attach", "board", "chat", "web", "mcp"}

func init() {
	for _, name := range harnessOnly {
		name := name
		register(name, "", "smithy "+name+" needs the harness build of smithy.", "--", func([]string) int {
			return needsHarness(os.Stderr, name)
		})
	}
}

func needsHarness(w io.Writer, name string) int {
	asset := fmt.Sprintf("smithy-harness-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}
	fmt.Fprintf(w, "smithy %s needs the harness build; this is the skills build.\n", name)
	fmt.Fprintf(w, "Get %s from https://github.com/Zovaris/smithy/releases, or build it: go build -tags harness ./cmd/smithy\n", asset)
	return 2
}
