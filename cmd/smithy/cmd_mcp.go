//go:build harness

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/board"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/mcp"
	"github.com/Zovaris/smithy/internal/proxy"
	"github.com/Zovaris/smithy/internal/store"
)

const mcpUsage = `usage: smithy mcp [--readonly]
An MCP server on stdin/stdout (harness build) that smithy agent gives every driven CLI:
  search       search the working folder (smithy search's engines and caps)
  board_*      list, show, claim and set tasks of the built-in tracker
  secret_call  call an API with secret:<name> through the egress proxy; the key never returns
  ask          ask the user a decision through the run's warden card and wait for the answer
--readonly keeps search, board_list and board_show. Inputs are validated and output is capped.`

func init() {
	if exe, err := os.Executable(); err == nil {
		agents.KitMCP = []string{exe, "mcp"}
	}
	register("mcp", "MCP server (stdio) for driven CLIs: board, ask, secret calls, search", mcpUsage, "--readonly", func(args []string) int {
		ro := len(args) == 1 && args[0] == "--readonly"
		if len(args) > 0 && !ro {
			fmt.Fprintln(os.Stderr, mcpUsage)
			return 2
		}
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "smithy mcp:", err)
			return 1
		}
		card := os.Getenv("SMITHY_MCP_CARD")
		tools := []mcp.Tool{mcp.SearchTool(cwd)}
		if st, err := store.Open(store.DefaultPath()); err == nil {
			defer st.Close()
			for _, t := range mcp.BoardTools(board.New(st), card) {
				if !ro || t.Name == "board_list" || t.Name == "board_show" {
					tools = append(tools, t)
				}
			}
		} else {
			fmt.Fprintln(os.Stderr, "smithy mcp: board off:", err)
		}
		if !ro {
			p := &proxy.Proxy{Secrets: auth.Secret, Approve: proxy.EventsApprover(5*time.Minute, time.Second),
				Log: func(e proxy.Entry) {
					if e.Status >= 400 {
						events.AppendProxy(events.Event{Kind: "proxy_denied", Detail: fmt.Sprintf("%s %s %s %s %d", e.Kind, e.Name, e.Method, e.Host, e.Status)})
					}
				}}
			tools = append(tools, mcp.SecretTool(p), mcp.AskTool(card, 2*time.Second))
		}
		s := &mcp.Server{Name: "smithy", Version: version, Tools: tools}
		if err := s.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "smithy mcp:", err)
			return 1
		}
		return 0
	})
}
