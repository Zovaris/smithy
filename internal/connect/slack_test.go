package connect

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type slackAPI struct {
	mu     sync.Mutex
	calls  []string
	bodies []map[string]any
	auths  []string
	wsURL  string
	fail   string
}

func (a *slackAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := strings.TrimPrefix(r.URL.Path, "/")
	var b map[string]any
	json.NewDecoder(r.Body).Decode(&b)
	a.calls, a.bodies, a.auths = append(a.calls, m), append(a.bodies, b), append(a.auths, r.Header.Get("Authorization"))
	if m == a.fail {
		w.Write([]byte(`{"ok":false,"error":"channel_not_found xoxb-1-SECRET"}`))
		return
	}
	switch m {
	case "chat.postMessage":
		ts := "100.1"
		if b["thread_ts"] != nil {
			ts = "100.2"
		}
		w.Write([]byte(`{"ok":true,"ts":"` + ts + `"}`))
	case "apps.connections.open":
		w.Write([]byte(`{"ok":true,"url":"` + a.wsURL + `"}`))
	default:
		w.Write([]byte(`{"ok":true}`))
	}
}

// socket is a test Socket Mode server: it sends frames and records the client's acks.
type socket struct {
	send chan string
	acks chan string
}

func (s *socket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	c, rw, _ := w.(http.Hijacker).Hijack()
	defer c.Close()
	rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
		base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n")
	rw.Flush()
	go func() {
		for {
			p, err := readMasked(rw.Reader)
			if err != nil {
				return
			}
			s.acks <- p
		}
	}()
	for m := range s.send {
		c.Write(append([]byte{0x81, 126, byte(len(m) >> 8), byte(len(m))}, m...))
	}
}

func readMasked(br *bufio.Reader) (string, error) {
	h := make([]byte, 2)
	if _, err := io.ReadFull(br, h); err != nil {
		return "", err
	}
	n := int(h[1] & 0x7f)
	if n == 126 {
		e := make([]byte, 2)
		io.ReadFull(br, e)
		n = int(e[0])<<8 | int(e[1])
	}
	mask := make([]byte, 4)
	io.ReadFull(br, mask)
	p := make([]byte, n)
	if _, err := io.ReadFull(br, p); err != nil {
		return "", err
	}
	for i := range p {
		p[i] ^= mask[i%4]
	}
	return string(p), nil
}

func slackTest(t *testing.T) (*Slack, *slackAPI, *socket) {
	sock := &socket{send: make(chan string, 8), acks: make(chan string, 8)}
	ws := httptest.NewServer(sock)
	api := &slackAPI{wsURL: "ws" + strings.TrimPrefix(ws.URL, "http") + "/link?ticket=1"}
	srv := httptest.NewServer(api)
	t.Cleanup(func() { close(sock.send); ws.Close(); srv.Close() })
	old := slackWSOK
	slackWSOK = func(u *url.URL) bool { return u.Scheme == "ws" }
	t.Cleanup(func() { slackWSOK = old })
	return &Slack{Base: srv.URL, Token: "xoxb-1-SECRET", AppToken: "xapp-1-APPSECRET", Channel: "C123456",
		Users: map[string]bool{"U1": true}, Poll: 2 * time.Second}, api, sock
}

func TestSlackThreadSendUpdate(t *testing.T) {
	s, api, _ := slackTest(t)
	ctx := context.Background()
	th, err := s.Thread(ctx, "batch/v1", "batch/v1")
	if err != nil || th != "100.1" {
		t.Fatalf("%q %v", th, err)
	}
	ref, err := s.Send(ctx, th, Note{Kind: "decision", Title: "Pick <one>", Text: "a & b", URL: "https://x.test/p"},
		[]Button{{Label: "yes", Data: "ab:0"}, {Label: "no", Data: "ab:1"}})
	if err != nil || ref != "100.2" {
		t.Fatalf("%q %v", ref, err)
	}
	b, _ := json.Marshal(api.bodies[1])
	for _, want := range []string{`"thread_ts":"100.1"`, `"value":"ab:1"`, `"type":"actions"`, `Pick \u0026lt;one\u0026gt;`, `a \u0026amp; b`, "DECIDE"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("send body lacks %s: %s", want, b)
		}
	}
	if api.auths[1] != "Bearer xoxb-1-SECRET" {
		t.Fatalf("bot token not used: %q", api.auths[1])
	}
	if err := s.Update(ctx, ref, Note{Title: "Pick", Text: "Answered: yes"}); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(api.bodies[2])
	if api.calls[2] != "chat.update" || strings.Contains(string(b), "actions") || !strings.Contains(string(b), `"ts":"100.2"`) {
		t.Fatalf("update: %s %s", api.calls[2], b)
	}
	api.fail = "chat.postMessage"
	if _, err := s.Send(ctx, "", Note{Title: "x"}, nil); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error must not leak the token: %v", err)
	}
}

func TestSlackReceive(t *testing.T) {
	s, api, sock := slackTest(t)
	ctx := context.Background()
	reply := func(env, user, channel, thread, ts string) string {
		return `{"envelope_id":"` + env + `","type":"events_api","payload":{"event":{"type":"message","user":"` + user +
			`","channel":"` + channel + `","text":"go on","ts":"` + ts + `","thread_ts":"` + thread + `"}}}`
	}
	sock.send <- `{"type":"hello"}`
	sock.send <- reply("e1", "U2", "C123456", "100.1", "100.5")
	sock.send <- reply("e2", "U1", "C999999", "100.1", "100.5")
	sock.send <- reply("e3", "U1", "C123456", "100.1", "100.1")
	sock.send <- reply("e4", "U1", "C123456", "100.1", "100.6")
	ins, err := s.Receive(ctx)
	if err != nil || len(ins) != 1 || ins[0] != (Inbound{Thread: "100.1", Text: "go on", By: "U1"}) {
		t.Fatalf("%+v %v", ins, err)
	}
	if api.auths[0] != "Bearer xapp-1-APPSECRET" {
		t.Fatalf("socket must open with the app token: %q", api.auths[0])
	}
	for _, e := range []string{"e1", "e2", "e3", "e4"} {
		if a := <-sock.acks; a != `{"envelope_id":"`+e+`"}` {
			t.Fatalf("ack %s: %s", e, a)
		}
	}
	sock.send <- `{"envelope_id":"i1","type":"interactive","payload":{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"C123456"},"message":{"ts":"100.2"},"actions":[{"value":"ab:0"}]}}`
	ins, err = s.Receive(ctx)
	if err != nil || len(ins) != 1 || ins[0] != (Inbound{Button: "ab:0", Ref: "100.2", By: "U1"}) {
		t.Fatalf("%+v %v", ins, err)
	}
	sock.send <- `{"type":"disconnect"}`
	if ins, err = s.Receive(ctx); err != nil || len(ins) != 0 || s.ws != nil {
		t.Fatalf("disconnect drops the socket: %+v %v", ins, err)
	}
}

func TestSlackReplyReachesSession(t *testing.T) {
	s, _, sock := slackTest(t)
	ctx := context.Background()
	var got []string
	h := &Hub{Conns: []Connector{s}, File: t.TempDir() + "/c.json",
		Input:  func(session, text, by string) error { got = append(got, session+"|"+text+"|"+by); return nil },
		Answer: func(id, answer, by string) error { got = append(got, id+"="+answer+"|"+by); return nil }}
	h.Notify(ctx, Note{Key: "k", Kind: "decision", Session: "batch/v1", Title: "Pick", Decision: "d1", Options: []string{"a", "b"}})
	sock.send <- `{"envelope_id":"1","type":"events_api","payload":{"event":{"type":"message","user":"U1","channel":"C123456","text":"a &lt;b&gt; &amp;amp;","ts":"100.9","thread_ts":"100.1"}}}`
	sock.send <- `{"envelope_id":"2","type":"interactive","payload":{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"C123456"},"message":{"ts":"100.2"},"actions":[{"value":"` + handle("d1") + `:1"}]}}`
	for len(got) < 2 {
		ins, err := s.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, in := range ins {
			h.Handle(ctx, s, in)
		}
	}
	if got[0] != "batch/v1|a <b> &amp;|slack:U1" || got[1] != "d1=b|slack:U1" {
		t.Fatalf("%v", got)
	}
}

func TestSlackSocketURLCheck(t *testing.T) {
	for u, ok := range map[string]bool{"wss://wss-primary.slack.com/link": true, "wss://slack.com/x": true,
		"ws://wss-primary.slack.com/link": false, "wss://evil.com/slack.com": false, "wss://slack.com.evil.com/": false} {
		p, _ := url.Parse(u)
		if wsURLOK(p) != ok {
			t.Errorf("%s: want %v", u, ok)
		}
	}
}
