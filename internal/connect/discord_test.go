package connect

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/config"
)

const dTok = "MTAxMDEwMTAxMDEwMTAxMDEw.GabcDE.SECRETsecretSECRETsecret_xyz"

type restAPI struct {
	mu     sync.Mutex
	calls  []string
	bodies []map[string]any
	auth   []string
	fail   bool
}

func (a *restAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	a.calls = append(a.calls, r.Method+" "+r.URL.Path)
	a.bodies = append(a.bodies, body)
	a.auth = append(a.auth, r.Header.Get("Authorization"))
	if a.fail {
		w.WriteHeader(403)
		w.Write([]byte(`{"message":"Missing Access ` + dTok + `"}`))
		return
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/threads"):
		w.Write([]byte(`{"id":"700"}`))
	case strings.HasSuffix(r.URL.Path, "/messages") && r.Method == "POST":
		w.Write([]byte(`{"id":"900"}`))
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/channels/"):
		w.Write([]byte(`{"id":"500","guild_id":"42"}`))
	default:
		w.WriteHeader(204)
	}
}

func dc(t *testing.T, a *restAPI) *Discord {
	srv := httptest.NewServer(a)
	t.Cleanup(srv.Close)
	return &Discord{API: srv.URL, Token: dTok, Channel: "500", Users: map[string]bool{"5": true}}
}

func TestDiscordThreadSendUpdate(t *testing.T) {
	a := &restAPI{}
	d := dc(t, a)
	ctx := context.Background()
	th, err := d.Thread(ctx, "batch/v1", "batch/v1")
	if err != nil || th != "700" {
		t.Fatalf("%q %v", th, err)
	}
	n := Note{Kind: "decision", Title: "Merge *now*? @everyone", Text: "line one\n[x](javascript:1)", URL: "https://github.com/o/r/pull/1"}
	ref, err := d.Send(ctx, th, n, []Button{{"yes", "h:0"}, {"no", "h:1"}, {"a", "h:2"}, {"b", "h:3"}, {"c", "h:4"}, {"d", "h:5"}})
	if err != nil || ref != "700/900" {
		t.Fatalf("%q %v", ref, err)
	}
	if err := d.Update(ctx, ref, n); err != nil {
		t.Fatal(err)
	}
	if a.calls[0] != "POST /channels/500/threads" || a.calls[1] != "POST /channels/700/messages" || a.calls[2] != "PATCH /channels/700/messages/900" {
		t.Fatalf("%v", a.calls)
	}
	if a.auth[1] != "Bot "+dTok {
		t.Fatalf("auth %q", a.auth[1])
	}
	msg := a.bodies[1]
	c := msg["content"].(string)
	if !strings.Contains(c, `\*now\*`) || strings.Contains(c, "@everyone") || strings.Contains(c, "](javascript") || !strings.Contains(c, "<https://github.com/o/r/pull/1>") || !strings.HasPrefix(c, "`DECIDE`") {
		t.Fatalf("content %q", c)
	}
	if am := msg["allowed_mentions"].(map[string]any); len(am["parse"].([]any)) != 0 {
		t.Fatalf("mentions %v", am)
	}
	rows := msg["components"].([]any)
	if len(rows) != 2 || len(rows[0].(map[string]any)["components"].([]any)) != 5 {
		t.Fatalf("rows %v", rows)
	}
	if b := rows[1].(map[string]any)["components"].([]any)[0].(map[string]any); b["custom_id"] != "h:5" || b["label"] != "d" {
		t.Fatalf("button %v", b)
	}
	if rows := a.bodies[2]["components"].([]any); len(rows) != 0 {
		t.Fatalf("update keeps buttons %v", rows)
	}
	if err := d.Update(ctx, "../x/1", n); err == nil {
		t.Fatal("bad ref accepted")
	}
	a.fail = true
	_, err = d.Send(ctx, "", n, nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "403") {
		t.Fatalf("error %v", err)
	}
}

// gateway is a fake Discord Gateway: hello, then it records sent frames and sends events.
type gateway struct {
	mu   sync.Mutex
	sent []string
	evs  chan string
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	c, rw, _ := w.(http.Hijacker).Hijack()
	defer c.Close()
	rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
		base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n")
	rw.Flush()
	send := func(s string) {
		h := []byte{0x81}
		if len(s) < 126 {
			h = append(h, byte(len(s)))
		} else {
			h = append(h, 126, byte(len(s)>>8), byte(len(s)))
		}
		c.Write(append(h, s...))
	}
	send(`{"op":10,"d":{"heartbeat_interval":60000}}`)
	go func() {
		for s := range g.evs {
			send(s)
		}
	}()
	for {
		m, err := srvRead(rw.Reader)
		if err != nil {
			return
		}
		g.mu.Lock()
		g.sent = append(g.sent, m)
		g.mu.Unlock()
	}
}

func srvRead(r *bufio.Reader) (string, error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return "", err
	}
	n := uint64(h[1] & 0x7f)
	if n == 126 {
		var b [2]byte
		io.ReadFull(r, b[:])
		n = uint64(binary.BigEndian.Uint16(b[:]))
	}
	var m [4]byte
	io.ReadFull(r, m[:])
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		return "", err
	}
	for i := range p {
		p[i] ^= m[i%4]
	}
	return string(p), nil
}

func TestDiscordMultiSelect(t *testing.T) {
	a := &restAPI{}
	d := dc(t, a)
	if _, err := d.Send(context.Background(), "", Note{Kind: "decision", Title: "Which?", Multi: true},
		[]Button{{"a", "h:0"}, {"b", "h:1"}, {"c", "h:2"}}); err != nil {
		t.Fatal(err)
	}
	rows := a.bodies[0]["components"].([]any)
	sel := rows[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	if len(rows) != 1 || sel["type"] != float64(3) || sel["max_values"] != float64(3) || sel["min_values"] != float64(1) ||
		len(sel["options"].([]any)) != 3 || sel["options"].([]any)[2].(map[string]any)["value"] != "h:2" {
		t.Fatalf("select %v", rows)
	}
	pick := func(user string) dEvent {
		return dEvent{Op: 0, T: "INTERACTION_CREATE", D: json.RawMessage(`{"id":"11","token":"tok_A","type":3,"guild_id":"42","member":{"user":{"id":"` + user +
			`"}},"data":{"custom_id":"h:multi","component_type":3,"values":["h:2","h:0"]},"message":{"id":"900","channel_id":"700"}}`)}
	}
	d.guild = "42"
	if _, ok := d.event(pick("6")); ok {
		t.Fatal("a user outside connect.discord.users counted")
	}
	in, ok := d.event(pick("5"))
	if !ok || in != (Inbound{Picks: "h:2,h:0", Ref: "700/900", By: "5"}) {
		t.Fatalf("%+v %v", in, ok)
	}
}

func TestDiscordReceive(t *testing.T) {
	a := &restAPI{}
	d := dc(t, a)
	d.Poll = 300 * time.Millisecond
	g := &gateway{evs: make(chan string, 8)}
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	d.Gateway = "ws" + strings.TrimPrefix(srv.URL, "http") + "/?v=10"
	for _, e := range []string{
		`{"op":0,"s":1,"t":"MESSAGE_CREATE","d":{"channel_id":"700","guild_id":"42","author":{"id":"5"},"content":"go on"}}`,
		`{"op":0,"s":2,"t":"MESSAGE_CREATE","d":{"channel_id":"700","guild_id":"42","author":{"id":"6"},"content":"stranger"}}`,
		`{"op":0,"s":3,"t":"MESSAGE_CREATE","d":{"channel_id":"700","guild_id":"99","author":{"id":"5"},"content":"other guild"}}`,
		`{"op":0,"s":4,"t":"MESSAGE_CREATE","d":{"channel_id":"700","guild_id":"42","author":{"id":"5","bot":true},"content":"bot"}}`,
		`{"op":0,"s":5,"t":"INTERACTION_CREATE","d":{"id":"11","token":"tok_A","type":3,"guild_id":"42","member":{"user":{"id":"5"}},"data":{"custom_id":"h:0"},"message":{"id":"900","channel_id":"700"}}}`,
		`{"op":0,"s":6,"t":"INTERACTION_CREATE","d":{"id":"12","token":"tok_B","type":3,"guild_id":"42","member":{"user":{"id":"6"}},"data":{"custom_id":"h:1"},"message":{"id":"900","channel_id":"700"}}}`,
	} {
		g.evs <- e
	}
	var got []Inbound
	deadline := time.Now().Add(3 * time.Second)
	for len(got) < 2 && time.Now().Before(deadline) {
		ins, err := d.Receive(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ins...)
	}
	if len(got) != 2 || got[0] != (Inbound{Thread: "700", Text: "go on", By: "5"}) || got[1] != (Inbound{Button: "h:0", Ref: "700/900", By: "5"}) {
		t.Fatalf("%+v", got)
	}
	ident := ""
	for i := 0; i < 100 && ident == ""; i++ {
		g.mu.Lock()
		if len(g.sent) > 0 {
			ident = g.sent[0]
		}
		g.mu.Unlock()
		if ident == "" {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !strings.Contains(ident, `"op":2`) || !strings.Contains(ident, dTok) {
		t.Fatalf("identify %q", ident)
	}
	acks := 0
	for i := 0; i < 50 && acks != 2; i++ {
		time.Sleep(20 * time.Millisecond)
		a.mu.Lock()
		acks = 0
		for _, c := range a.calls {
			if strings.HasSuffix(c, "/callback") {
				acks++
			}
		}
		a.mu.Unlock()
	}
	if acks != 2 {
		t.Fatalf("every press is acknowledged: %v", a.calls)
	}
}

func TestWSDialRefusesBadHandshake(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	if _, err := wsDial(context.Background(), &url.URL{Scheme: "ws", Host: strings.TrimPrefix(srv.URL, "http://"), Path: "/"}); err == nil {
		t.Fatal("non-101 accepted")
	}
}

func TestDiscordFullBufferWaitsThenLogsADrop(t *testing.T) {
	var logged []string
	d := &Discord{BufferWait: 200 * time.Millisecond}
	d.SetLog(func(s string) { logged = append(logged, s) })
	g := &dgw{in: make(chan Inbound, 1), done: make(chan struct{})}
	d.deliver(g, Inbound{Text: "a"})
	go func() { time.Sleep(50 * time.Millisecond); <-g.in }()
	d.deliver(g, Inbound{Text: "b"})
	if len(logged) != 0 || (<-g.in).Text != "b" {
		t.Fatalf("a slow reader lost an event: %v", logged)
	}
	d.deliver(g, Inbound{Text: "c"})
	d.deliver(g, Inbound{Text: "d"})
	if len(logged) != 1 || !strings.Contains(logged[0], "dropped") {
		t.Fatalf("a drop was not logged: %v", logged)
	}
}

func TestDiscordFromConfig(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("SMITHY_CONFIG", cfg)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TELEGRAM_API_KEY", "")
	t.Setenv("DISCORD_API_KEY", "")
	config.Set(cfg, "connect.discord.channel", "500")
	if cs, err := FromConfig(); len(cs) != 0 || err == nil {
		t.Fatalf("a channel without a token: %v %v", cs, err)
	}
	t.Setenv("DISCORD_API_KEY", "not-a-token")
	if cs, err := FromConfig(); len(cs) != 0 || err == nil {
		t.Fatalf("bad token: %v %v", cs, err)
	}
	t.Setenv("DISCORD_API_KEY", dTok)
	config.Set(cfg, "connect.discord.channel", "general")
	if cs, err := FromConfig(); len(cs) != 0 || err == nil {
		t.Fatalf("a channel name: %v %v", cs, err)
	}
	config.Set(cfg, "connect.discord.channel", "500")
	config.Set(cfg, "connect.discord.users", "5")
	cs, err := FromConfig()
	if err != nil || len(cs) != 1 {
		t.Fatalf("%v %v", cs, err)
	}
	if d := cs[0].(*Discord); d.Channel != "500" || !d.Users["5"] || d.Token != dTok {
		t.Fatalf("%+v", d)
	}
}
