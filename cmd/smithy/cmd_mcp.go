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
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/guard"
	"github.com/Zovaris/smithy/internal/mcp"
	"github.com/Zovaris/smithy/internal/proxy"
	"github.com/Zovaris/smithy/internal/store"
)

const mcpUsage = `usage: smithy mcp [--readonly]
An MCP server on stdin/stdout (harness build) that smithy agent gives every driven CLI:
  search       search the working folder (smithy search's engines and caps)
  board_*      list, show, claim and set tasks of the built-in tracker
  secret_call  call an API with secret:<name> through the egress proxy; the key never returns
  ask          ask the user a decision through smithy daemon (on the run's card in warden) and wait
  send, inbox, start_session
               in a daemon session (SMITHY_SESSION_KEY): message its parent, children or siblings,
               read what they sent, start a child session in the same project
--readonly keeps search, board_list and board_show. Inputs are validated and output is capped.`

func init() {
	if exe, err := os.Executable(); err == nil {
		agents.KitMCP = []string{exe, "mcp"}
		agents.KitGuard = guard.HookSettings
	}
	guard.HookGuard = func() guard.Guard {
		return guard.Guard{Connect: func() (guard.Asker, error) {
			a, err := daemon.LocalAsker()
			if err != nil {
				return nil, err
			}
			return a, nil
		}}
	}
	register("mcp", harnessSummary["mcp"], mcpUsage, "--readonly", func(args []string) int {
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
						_ = events.AppendProxy(events.Event{Kind: "proxy_denied", Detail: fmt.Sprintf("%s %s %s %s %d", e.Kind, e.Name, e.Method, e.Host, e.Status)})
					}
				}}
			tools = append(tools, mcp.SecretTool(p), mcp.AskTool(card, 2*time.Second, func() (mcp.Asker, error) {
				a, err := daemon.LocalAsker()
				if err != nil {
					return nil, err
				}
				return a, nil
			}))
		}
		if key := os.Getenv("SMITHY_SESSION_KEY"); key != "" && !ro {
			tools = append(tools, mcp.BusTools(os.Getenv("SMITHY_SESSION"), func() (mcp.Bus, error) {
				a, err := daemon.LocalAsker()
				if err != nil {
					return nil, err
				}
				return busAdapter{&daemon.BusClient{C: a.C, Key: key}}, nil
			})...)
		}
		s := &mcp.Server{Name: "smithy", Version: version, Tools: tools}
		if err := s.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "smithy mcp:", err)
			return 1
		}
		return 0
	})
}

type busAdapter struct{ b *daemon.BusClient }

func (a busAdapter) Send(ctx context.Context, to, text string) (any, error) {
	return a.b.Send(ctx, to, text)
}

func (a busAdapter) Inbox(ctx context.Context, since int64) (any, error) {
	return a.b.Inbox(ctx, since)
}

func (a busAdapter) Start(ctx context.Context, agent, dir, msg string) (any, error) {
	return a.b.Start(ctx, daemon.BusStartReq{Agent: agent, Dir: dir, Message: msg})
}
