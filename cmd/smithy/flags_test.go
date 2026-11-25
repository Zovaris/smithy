package main

import (
	"regexp"
	"sort"
	"testing"
)

// Every --flag a command's usage documents must be accepted by its flag table (or a
// subcommand's), else entry rejects it as "unknown option".
func TestUsageFlagsAccepted(t *testing.T) {
	re := regexp.MustCompile(`(?:^|[\s\[(|,` + "`" + `])(--[a-z][a-z0-9-]*)`)
	for name, c := range commands {
		known := map[string]bool{"--help": true}
		for f := range c.flags {
			known[f] = true
		}
		for _, sub := range c.subs {
			for f := range sub {
				known[f] = true
			}
		}
		var missing []string
		for _, m := range re.FindAllStringSubmatch(c.usage, -1) {
			if !known[m[1]] && !foreign[name+" "+m[1]] {
				missing = append(missing, m[1])
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("smithy %s: usage documents %v but the flag table rejects them", name, missing)
		}
	}
}

// foreign flags belong to other commands or tools a usage text mentions, keyed "<cmd> <flag>".
var foreign = map[string]bool{
	"agent --approval-mode": true, "agent --approval-policy": true, "agent --auto": true,
	"agent --experimental-acp": true, "agent --force": true, "agent --permission-mode": true, "agent --sandbox": true,
	"agent --standalone": true, "agent --tools": true,
	"changed --mutate": true, "route --repo": true, "status --all": true,
	"task-dir --repo": true, "version --version": true,
}
