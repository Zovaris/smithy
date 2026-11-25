//go:build harness

package daemon

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/store"
)

func TestClientAskAndReadDecision(t *testing.T) {
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	c := &Client{Base: hs.URL, Token: testToken}
	ctx := context.Background()
	if err := c.Ask(ctx, "secret:x", "q", nil); err == nil {
		t.Fatal("ask accepted a non-decide id")
	}
	id := "decide:b/i/mcp-1"
	if err := c.Ask(ctx, id, "Which db?", []string{"sqlite", "postgres"}); err != nil {
		t.Fatal(err)
	}
	d, err := c.Decision(ctx, id)
	if err != nil || d.Question != "Which db?" || d.Outcome != nil {
		t.Fatalf("decision %+v %v", d, err)
	}
	if _, err := sv.Answer(id, "postgres", "me"); err != nil {
		t.Fatal(err)
	}
	d, err = c.Decision(ctx, id)
	if err != nil || d.Outcome == nil || d.Outcome.Answer != "postgres" {
		t.Fatalf("decision %+v %v", d, err)
	}
	if _, err := c.Decision(ctx, "decide:b/i/none"); err == nil {
		t.Fatal("unknown decision found")
	}
}

// restartable serves one store on a fixed address and can be stopped and started again,
// like a daemon restart.
type restartable struct {
	t    *testing.T
	path string
	addr string
	hs   *httptest.Server
	st   *store.Store
	sv   *Server
}

func (r *restartable) start() {
	st, err := store.Open(r.path)
	if err != nil {
		r.t.Fatal(err)
	}
	sv, err := New(st, testToken, nil, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	var ln net.Listener
	for i := 0; i < 50; i++ {
		if ln, err = net.Listen("tcp", r.addr); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		r.t.Fatal(err)
	}
	hs := httptest.NewUnstartedServer(sv.Handler())
	hs.Listener.Close()
	hs.Listener = ln
	hs.Start()
	r.addr, r.hs, r.st, r.sv = ln.Addr().String(), hs, st, sv
}

func (r *restartable) stop() {
	r.hs.CloseClientConnections()
	r.hs.Close()
	r.st.Close()
}

func TestFollowResumesAfterRestartWithoutDuplicates(t *testing.T) {
	r := &restartable{t: t, path: filepath.Join(t.TempDir(), "s.db"), addr: "127.0.0.1:0"}
	r.start()
	defer func() { r.stop() }()
	c := &Client{Base: "http://" + r.addr, Token: testToken, Retry: 20 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got := make(chan ClientMsg, 64)
	go c.Follow(ctx, 0, got)

	r.sv.Append("log", "", map[string]string{"line": "one"})
	r.sv.Append("log", "", map[string]string{"line": "two"})
	var seqs []int64
	next := func() ClientMsg {
		for {
			select {
			case m := <-got:
				if m.Event != nil {
					seqs = append(seqs, m.Event.Seq)
				}
				return m
			case <-ctx.Done():
				t.Fatalf("timeout; seqs %v", seqs)
			}
		}
	}
	for len(seqs) < 2 {
		next()
	}

	r.stop()
	for m := next(); m.Err == nil; m = next() {
	}
	r.start()
	r.sv.Append("log", "", map[string]string{"line": "three"})
	for len(seqs) < 3 {
		next()
	}
	select {
	case m := <-got:
		if m.Event != nil {
			t.Fatalf("extra event %d after resume", m.Event.Seq)
		}
	case <-time.After(150 * time.Millisecond):
	}
	if seqs[0] != 1 || seqs[1] != 2 || seqs[2] != 3 {
		t.Fatalf("seqs = %v, want [1 2 3]", seqs)
	}
}

func TestClientDecide(t *testing.T) {
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	if err := sv.Ask("d1", "ship?", []string{"yes", "no"}); err != nil {
		t.Fatal(err)
	}
	c := &Client{Base: hs.URL, Token: testToken}
	st, err := c.State(context.Background())
	if err != nil || len(st.Pending) != 1 {
		t.Fatalf("state %+v %v", st, err)
	}
	o, err := c.Decide(context.Background(), "d1", "yes")
	if err != nil || !o.Won || o.Answer != "yes" {
		t.Fatalf("first answer %+v %v", o, err)
	}
	o, err = c.Decide(context.Background(), "d1", "no")
	if err != nil || o.Won || o.Answer != "yes" {
		t.Fatalf("second answer %+v %v", o, err)
	}
	bad := &Client{Base: hs.URL, Token: "nope"}
	if _, err := bad.State(context.Background()); err == nil {
		t.Fatal("bad token: want error")
	}
	_ = http.StatusOK
}
