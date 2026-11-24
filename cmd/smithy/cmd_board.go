//go:build harness

package main

import (
	"os"

	"github.com/Zovaris/smithy/internal/board"
)

func init() {
	register("board", "built-in tracker (harness build): add, list, show, set, claim, sync", board.Usage, "",
		func(args []string) int { return board.Main(args, os.Stdout, os.Stderr) })
	registerSub("board", "add", "--body= --priority= --labels= --blocked-by=")
	registerSub("board", "list", "--state= --json")
	registerSub("board", "show", "--json")
	registerSub("board", "set", "--state= --title= --body= --priority= --labels= --blocked-by= --branch= --pr=")
	registerSub("board", "claim", "--by= --ttl= --release")
	registerSub("board", "sync", "")
}
