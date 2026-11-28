package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/text"
)

// SlackAPI is the Web API origin (a var for tests only, never a setting, so a config edit can't
// send the tokens elsewhere).
var SlackAPI = "https://slack.com/api"

// slackWSOK checks the Socket Mode URL Slack hands back (a var for tests only).
var slackWSOK = wsURLOK

// Slack is the Socket Mode connector: one thread per session in Channel (rooted at a message
// naming the session), Block Kit buttons for decisions, and a Socket Mode websocket for what
// comes back. Token is the bot token (xoxb-), AppToken the app-level token (xapp-). Only events
// from Channel (and ChatChan) and from a user id in Users count: with Users empty it only sends.
// ChatChan ("" for none) is the chat channel: a new message there starts a session in its thread.
type Slack struct {
	Base, Token, AppToken, Channel string
	ChatChan                       string
	Users                          map[string]bool
	// Poll is how long Receive waits for an event (default 25 s).
	Poll   time.Duration
	Client *http.Client

	mu   sync.Mutex
	ws   *wsConn
	msgs chan []byte
	errs chan error
	done chan struct{}
}

func (s *Slack) Name() string { return "slack" }

// MultiSelect: a multi-select decision is one multi_static_select menu.
func (s *Slack) MultiSelect() bool { return true }

// HasChat reports whether a chat channel is set.
func (s *Slack) HasChat() bool { return s.ChatChan != "" }

// split maps a thread or ref to its channel and ts: a ChatRoot prefix means ChatChan.
func (s *Slack) split(v string) (string, string) {
	if rest, ok := strings.CutPrefix(v, ChatRoot); ok && s.ChatChan != "" {
		return s.ChatChan, rest
	}
	return s.Channel, v
}

// tag is the thread or ref Receive reports for ts in channel.
func (s *Slack) tag(channel, ts string) string {
	if channel == s.ChatChan && channel != s.Channel {
		return ChatRoot + ts
	}
	return ts
}

func (s *Slack) scrub(v string) string {
	for _, t := range []string{s.Token, s.AppToken} {
		if t != "" {
			v = strings.ReplaceAll(v, t, "<token>")
		}
	}
	return text.Clean(v)
}

func (s *Slack) call(ctx context.Context, method, token string, body any, out any) error {
	for try := 0; ; try++ {
		wait, err := s.call1(ctx, method, token, body, out)
		if wait == 0 || !rateLimited(ctx, wait, try) {
			return err
		}
	}
}

func (s *Slack) call1(ctx context.Context, method, token string, body any, out any) (time.Duration, error) {
	base := s.Base
	if base == "" {
		base = SlackAPI
	}
	cl := s.Client
	if cl == nil {
		cl = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/"+method, bytes.NewReader(b))
	if err != nil {
		return 0, errors.New("slack: bad request")
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := cl.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, fmt.Errorf("slack %s: %s", method, s.scrub(err.Error()))
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var r struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(rb, &r) != nil || !r.OK {
		var wait time.Duration
		if resp.StatusCode == http.StatusTooManyRequests {
			n, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			wait = max(time.Duration(n)*time.Second, time.Second)
		}
		return wait, fmt.Errorf("slack %s: HTTP %d %s", method, resp.StatusCode, s.scrub(r.Error))
	}
	if out != nil {
		return 0, json.Unmarshal(rb, out)
	}
	return 0, nil
}

var slackEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace

var slackUnesc = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace

func slackText(n Note) string {
	head := "*" + slackEsc(clip(strings.ReplaceAll(text.Clean(n.Title), "*", ""), 200)) + "*"
	if tag := tgTag[n.Kind]; tag != "" {
		head = "`" + tag + "`  " + head
	}
	var lines []string
	if n.Title != "" {
		lines = append(lines, head)
	}
	for _, l := range strings.Split(n.Text, "\n") {
		if l = strings.TrimRight(text.Clean(l), " \t"); strings.TrimSpace(l) != "" || n.Kind == KindChat {
			lines = append(lines, slackEsc(clip(l, lineCap(n))))
		}
	}
	if u, err := url.Parse(n.URL); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
		lines = append(lines, "<"+strings.NewReplacer("|", "%7C", ">", "%3E", "<", "%3C").Replace(u.String())+"|"+slackEsc(clip(u.String(), 120))+">")
	}
	return clip(strings.Join(lines, "\n"), 2900)
}

func slackBlocks(n Note, buttons []Button) []any {
	blocks := []any{map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": slackText(n)}}}
	if n.Multi && len(buttons) > 1 {
		var opts []any
		for i, b := range buttons {
			if i == 100 {
				break
			}
			label := clip(text.Clean(b.Label), 75)
			if label == "" {
				label = "-"
			}
			opts = append(opts, map[string]any{"value": b.Data, "text": map[string]any{"type": "plain_text", "text": label}})
		}
		sel := map[string]any{"type": "multi_static_select", "action_id": "smithy_multi", "options": opts,
			"max_selected_items": len(opts), "placeholder": map[string]any{"type": "plain_text", "text": "Pick any"}}
		return append(blocks, map[string]any{"type": "actions", "elements": []any{sel}})
	}
	if len(buttons) > 0 {
		var els []any
		for i, b := range buttons {
			if i == 25 {
				break
			}
			label := clip(text.Clean(b.Label), 70)
			if label == "" {
				label = "-"
			}
			els = append(els, map[string]any{"type": "button", "action_id": fmt.Sprintf("smithy_%d", i),
				"value": b.Data, "text": map[string]any{"type": "plain_text", "text": label}})
		}
		blocks = append(blocks, map[string]any{"type": "actions", "elements": els})
	}
	return blocks
}

// Thread posts a root message naming the session; its ts is the thread.
func (s *Slack) Thread(ctx context.Context, session, title string) (string, error) {
	name := clip(text.Clean(title), 120)
	if name == "" {
		name = "smithy"
	}
	var r struct {
		TS string `json:"ts"`
	}
	if err := s.call(ctx, "chat.postMessage", s.Token, map[string]any{"channel": s.Channel, "text": "Session: " + slackEsc(name),
		"unfurl_links": false, "unfurl_media": false}, &r); err != nil {
		return "", err
	}
	return r.TS, nil
}

// Send posts n in thread ("" is the channel itself) with one button per option.
func (s *Slack) Send(ctx context.Context, thread string, n Note, buttons []Button) (string, error) {
	channel, thread := s.split(thread)
	body := map[string]any{"channel": channel, "text": clip(text.Clean(n.Title+" "+line(n.Text, 120)), 200), "blocks": slackBlocks(n, buttons),
		"unfurl_links": false, "unfurl_media": false}
	if thread != "" {
		body["thread_ts"] = thread
	}
	var r struct {
		TS string `json:"ts"`
	}
	if err := s.call(ctx, "chat.postMessage", s.Token, body, &r); err != nil {
		return "", err
	}
	return s.tag(channel, r.TS), nil
}

// Update replaces message ref and removes its buttons.
func (s *Slack) Update(ctx context.Context, ref string, n Note) error {
	channel, ref := s.split(ref)
	return s.call(ctx, "chat.update", s.Token, map[string]any{"channel": channel, "ts": ref,
		"text": clip(text.Clean(n.Title), 200), "blocks": slackBlocks(n, nil)}, nil)
}

func (s *Slack) poll() time.Duration {
	if s.Poll > 0 {
		return s.Poll
	}
	return 25 * time.Second
}

func (s *Slack) connect(ctx context.Context) error {
	var r struct {
		URL string `json:"url"`
	}
	if err := s.call(ctx, "apps.connections.open", s.AppToken, map[string]any{}, &r); err != nil {
		return err
	}
	u, err := url.Parse(r.URL)
	if err != nil || !slackWSOK(u) {
		return errors.New("slack: Socket Mode returned an unexpected URL")
	}
	ws, err := wsDial(ctx, u)
	if err != nil {
		return fmt.Errorf("slack socket: %s", s.scrub(err.Error()))
	}
	msgs, errs, done := make(chan []byte, 16), make(chan error, 1), make(chan struct{})
	go func() {
		for {
			m, err := ws.Read(time.Now().Add(2 * time.Minute))
			if err != nil {
				errs <- err
				return
			}
			select {
			case msgs <- m:
			case <-done:
				return
			}
		}
	}()
	s.ws, s.msgs, s.errs, s.done = ws, msgs, errs, done
	return nil
}

func (s *Slack) drop() {
	if s.ws != nil {
		close(s.done)
		s.ws.Close()
	}
	s.ws, s.msgs, s.errs, s.done = nil, nil, nil, nil
}

type slackEnvelope struct {
	ID      string `json:"envelope_id"`
	Type    string `json:"type"`
	Payload struct {
		Event struct {
			Type     string `json:"type"`
			Subtype  string `json:"subtype"`
			BotID    string `json:"bot_id"`
			User     string `json:"user"`
			Channel  string `json:"channel"`
			Text     string `json:"text"`
			TS       string `json:"ts"`
			ThreadTS string `json:"thread_ts"`
		} `json:"event"`
		Type    string              `json:"type"`
		User    struct{ ID string } `json:"user"`
		Channel struct{ ID string } `json:"channel"`
		Message struct {
			TS string `json:"ts"`
		} `json:"message"`
		Actions []struct {
			Type     string `json:"type"`
			Value    string `json:"value"`
			Selected []struct {
				Value string `json:"value"`
			} `json:"selected_options"`
		} `json:"actions"`
	} `json:"payload"`
}

func (s *Slack) allowed(channel, user string) bool {
	return (channel == s.Channel || (s.ChatChan != "" && channel == s.ChatChan)) && user != "" && s.Users[user]
}

func (s *Slack) inbound(e slackEnvelope) []Inbound {
	p := e.Payload
	switch e.Type {
	case "events_api":
		ev := p.Event
		if ev.Type != "message" || ev.Subtype != "" || ev.BotID != "" || ev.Text == "" || !s.allowed(ev.Channel, ev.User) {
			return nil
		}
		chat := s.ChatChan != "" && ev.Channel == s.ChatChan
		if ev.ThreadTS != "" && ev.ThreadTS != ev.TS {
			return []Inbound{{Thread: s.tag(ev.Channel, ev.ThreadTS), Text: clip(slackUnesc(ev.Text), 4000), By: ev.User, Chat: chat}}
		}
		if chat {
			return []Inbound{{Thread: s.tag(ev.Channel, ev.TS), Ref: s.tag(ev.Channel, ev.TS), Text: clip(slackUnesc(ev.Text), 4000), By: ev.User, Chat: true}}
		}
	case "interactive":
		if p.Type == "block_actions" && len(p.Actions) == 1 && p.Actions[0].Type == "multi_static_select" && s.allowed(p.Channel.ID, p.User.ID) {
			var picks []string
			for _, o := range p.Actions[0].Selected {
				if len(o.Value) > 64 || strings.Contains(o.Value, ",") {
					return nil
				}
				picks = append(picks, o.Value)
			}
			if len(picks) == 0 || len(picks) > 100 {
				return nil
			}
			return []Inbound{{Picks: strings.Join(picks, ","), Ref: s.tag(p.Channel.ID, p.Message.TS), By: p.User.ID}}
		}
		if p.Type == "block_actions" && len(p.Actions) == 1 && len(p.Actions[0].Value) <= 64 && s.allowed(p.Channel.ID, p.User.ID) {
			return []Inbound{{Button: p.Actions[0].Value, Ref: s.tag(p.Channel.ID, p.Message.TS), By: p.User.ID}}
		}
	}
	return nil
}

// Receive waits up to the poll timeout for Socket Mode envelopes (connecting first if needed),
// acknowledges each one, and returns the thread replies and button presses that count.
func (s *Slack) Receive(ctx context.Context) ([]Inbound, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ws == nil {
		if err := s.connect(ctx); err != nil {
			return nil, err
		}
	}
	timer := time.NewTimer(s.poll())
	defer timer.Stop()
	var out []Inbound
	for {
		select {
		case <-ctx.Done():
			s.drop()
			return out, nil
		case <-timer.C:
			return out, nil
		case err := <-s.errs:
			s.drop()
			if errors.Is(err, errWSClosed) {
				return out, nil
			}
			return out, fmt.Errorf("slack socket: %s", s.scrub(err.Error()))
		case m := <-s.msgs:
			var e slackEnvelope
			if json.Unmarshal(m, &e) != nil {
				continue
			}
			if e.ID != "" {
				ack, _ := json.Marshal(map[string]string{"envelope_id": e.ID})
				if err := s.ws.WriteText(ack); err != nil {
					s.drop()
					return out, fmt.Errorf("slack socket: %s", s.scrub(err.Error()))
				}
			}
			if e.Type == "disconnect" {
				s.drop()
				return out, nil
			}
			out = append(out, s.inbound(e)...)
			if len(out) > 0 {
				return out, nil
			}
		}
	}
}
