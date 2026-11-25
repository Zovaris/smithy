//go:build harness

package mcp

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/board"
	"github.com/Zovaris/smithy/internal/store"
)

func TestBoardTools(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	st, err := store.Open(filepath.Join(t.TempDir(), "smithy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b := board.New(st)
	id, _ := b.Add(board.Task{Title: "fix \x1b[31mlogin"})
	call := session(t, BoardTools(b, "b1/i1"))
	s, isErr := reply(t, call(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"board_list","arguments":{"state":"todo"}}}`))
	if isErr || !strings.Contains(s, "fix") || strings.Contains(s, "\x1b") {
		t.Fatalf("list: %q", s)
	}
	s, isErr = reply(t, call(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"board_claim","arguments":{"id":"B-1"}}}`))
	if isErr {
		t.Fatalf("claim: %q", s)
	}
	got, _ := b.Get(id)
	if got.ClaimedBy != "b1/i1" {
		t.Fatalf("claimed by %q", got.ClaimedBy)
	}
	s, isErr = reply(t, call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"board_set","arguments":{"id":"1","state":"review","pr":"https://github.com/o/r/pull/1"}}}`))
	if isErr {
		t.Fatalf("set: %q", s)
	}
	s, isErr = reply(t, call(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"board_show","arguments":{"id":"1"}}}`))
	if isErr || !strings.Contains(s, `"state": "review"`) || !strings.Contains(s, "pull/1") {
		t.Fatalf("show: %q", s)
	}
	for _, bad := range []string{`"name":"board_set","arguments":{"id":"1","state":"gone"}`, `"name":"board_set","arguments":{"id":"1","pr":"javascript:x"}`,
		`"name":"board_show","arguments":{"id":"x"}`, `"name":"board_claim","arguments":{"id":"1","ttl_minutes":100000}`} {
		if s, isErr = reply(t, call(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{`+bad+`}}`)); !isErr {
			t.Fatalf("%s accepted: %q", bad, s)
		}
	}
	s, isErr = reply(t, call(`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"board_claim","arguments":{"id":"1","release":true}}}`))
	if got, _ = b.Get(id); isErr || got.ClaimedBy != "" {
		t.Fatalf("release: %q %q", s, got.ClaimedBy)
	}
}

func TestBoardSetRefusesOthersClaim(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	st, err := store.Open(filepath.Join(t.TempDir(), "smithy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b := board.New(st)
	id, _ := b.Add(board.Task{Title: "x"})
	b.Claim(id, "other", time.Hour)
	call := session(t, BoardTools(b, "me/i1"))
	for _, a := range []string{`{"id":"1","state":"done"}`, `{"id":"1","release":true}`} {
		name := "board_set"
		if strings.Contains(a, "release") {
			name = "board_claim"
		}
		if s, isErr := reply(t, call(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+a+`}}`)); !isErr {
			t.Fatalf("%s %s on another run's claim: %q", name, a, s)
		}
	}
	if s, isErr := reply(t, call(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"board_set","arguments":{"id":"1","branch":"--upload-pack=x"}}}`)); !isErr {
		t.Fatalf("bad branch accepted: %q", s)
	}
}
