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
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/text"
)

// DiscordAPI and DiscordGateway are Discord's origins (vars for tests only, never settings, so a
// config edit can't send the token elsewhere).
var (
	DiscordAPI     = "https://discord.com/api/v10"
	DiscordGateway = "wss://gateway.discord.gg/?v=10&encoding=json"
)

// Gateway intents: GUILD_MESSAGES and MESSAGE_CONTENT (privileged: turn it on in the bot's
// settings, or thread replies arrive empty and are dropped).
const discordIntents = 1<<9 | 1<<15

var (
	snowflakeRe = regexp.MustCompile(`^[0-9]{1,20}$`)
	itokenRe    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,500}$`)
)

// Discord is the bot connector: one public thread per session in Channel, button components for
// decisions, the Gateway for what comes back. Only messages and presses from a user id in Users
// count, in threads of Channel's guild: with Users empty it only sends.
type Discord struct {
	Token, Channel string
	// API and Gateway override DiscordAPI and DiscordGateway (tests only).
	API, Gateway string
	Users        map[string]bool
	// Poll is how long Receive waits for events (default 25 s).
	Poll   time.Duration
	Client *http.Client
	// BufferWait is how long the Gateway reader waits on a full event buffer before it drops
	// the event and logs it (default 10 s).
	BufferWait time.Duration

	mu    sync.Mutex
	gw    *dgw
	guild string
	log   func(string)
}

// SetLog sets where the connector reports what it had to drop; the hub sets it to its own Log.
func (d *Discord) SetLog(f func(string)) {
	d.mu.Lock()
	d.log = f
	d.mu.Unlock()
}

// deliver hands in to Receive, waiting up to BufferWait while the buffer is full; past that the
// event is dropped and logged rather than stalling the Gateway (heartbeats share this reader).
func (d *Discord) deliver(g *dgw, in Inbound) {
	select {
	case g.in <- in:
		return
	default:
	}
	wait := d.BufferWait
	if wait <= 0 {
		wait = 10 * time.Second
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case g.in <- in:
	case <-g.done:
	case <-t.C:
		d.mu.Lock()
		log := d.log
		d.mu.Unlock()
		if log != nil {
			log("connect discord: event buffer full for " + wait.String() + ", dropped one inbound event")
		}
	}
}

// dgw is one live Gateway connection; its reader fills in and ends with err.
type dgw struct {
	ws   *wsConn
	in   chan Inbound
	done chan struct{}
	err  error
}

func (d *Discord) Name() string { return "discord" }

func (d *Discord) scrub(s string) string {
	if d.Token != "" {
		s = strings.ReplaceAll(s, d.Token, "<token>")
	}
	return text.Clean(s)
}

func (d *Discord) poll() time.Duration {
	if d.Poll > 0 {
		return d.Poll
	}
	return 25 * time.Second
}

func (d *Discord) call(ctx context.Context, method, path string, body, out any) error {
	for try := 0; ; try++ {
		wait, err := d.call1(ctx, method, path, body, out)
		if wait == 0 || !rateLimited(ctx, wait, try) {
			return err
		}
	}
}

func (d *Discord) call1(ctx context.Context, method, path string, body, out any) (time.Duration, error) {
	cl := d.Client
	if cl == nil {
		cl = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	base := d.API
	if base == "" {
		base = DiscordAPI
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return 0, errors.New("discord: bad request")
	}
	req.Header.Set("Authorization", "Bot "+d.Token)
	req.Header.Set("User-Agent", "DiscordBot (https://github.com/Zovaris/smithy, 1)")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := cl.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, fmt.Errorf("discord %s: %s", routeOf(path), d.scrub(err.Error()))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		var r struct {
			Message    string  `json:"message"`
			RetryAfter float64 `json:"retry_after"`
		}
		_ = json.Unmarshal(b, &r)
		var wait time.Duration
		if resp.StatusCode == http.StatusTooManyRequests {
			wait = max(time.Duration(r.RetryAfter*float64(time.Second)), time.Second)
		}
		return wait, fmt.Errorf("discord %s %s: HTTP %d %s", method, routeOf(path), resp.StatusCode, d.scrub(r.Message))
	}
	if out != nil && len(b) > 0 {
		return 0, json.Unmarshal(b, out)
	}
	return 0, nil
}

// routeOf keeps a path's shape for errors without ids or interaction tokens.
func routeOf(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		if snowflakeRe.MatchString(s) || len(s) > 30 {
			parts[i] = ":id"
		}
	}
	return strings.Join(parts, "/")
}

// Thread starts a public thread named after the session in Channel.
func (d *Discord) Thread(ctx context.Context, session, title string) (string, error) {
	name := []rune(text.Clean(title))
	if len(name) > 100 {
		name = name[:100]
	}
	if len(name) == 0 {
		name = []rune("smithy")
	}
	var r struct {
		ID string `json:"id"`
	}
	body := map[string]any{"name": string(name), "type": 11, "auto_archive_duration": 10080}
	if err := d.call(ctx, "POST", "/channels/"+d.Channel+"/threads", body, &r); err != nil {
		return "", err
	}
	if !snowflakeRe.MatchString(r.ID) {
		return "", errors.New("discord: thread without an id")
	}
	return r.ID, nil
}

var mdEsc = strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "~", `\~`, "`", "\\`", "|", `\|`, ">", `\>`,
	"#", `\#`, "[", `\[`, "]", `\]`, "(", `\(`, ")", `\)`, "<", `\<`, "@", "@​")

func renderDiscord(n Note) string {
	head := "**" + mdEsc.Replace(clip(text.Clean(n.Title), 200)) + "**"
	if tag := tgTag[n.Kind]; tag != "" {
		head = "`" + tag + "`  " + head
	}
	lines := []string{head}
	for _, l := range strings.Split(n.Text, "\n") {
		if l = strings.TrimSpace(text.Clean(l)); l != "" {
			lines = append(lines, mdEsc.Replace(clip(l, 500)))
		}
	}
	if u, err := url.Parse(n.URL); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
		s := u.String()
		if !strings.ContainsAny(s, "<> ") && len(s) <= 500 {
			lines = append(lines, "<"+s+">")
		}
	}
	return clip(strings.Join(lines, "\n"), 2000)
}

func (d *Discord) message(n Note, buttons []Button) map[string]any {
	var rows []map[string]any
	var row []map[string]any
	for i, b := range buttons {
		if i == 25 {
			break
		}
		row = append(row, map[string]any{"type": 2, "style": 1, "label": clip(text.Clean(b.Label), 80), "custom_id": b.Data})
		if len(row) == 5 {
			rows = append(rows, map[string]any{"type": 1, "components": row})
			row = nil
		}
	}
	if row != nil {
		rows = append(rows, map[string]any{"type": 1, "components": row})
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	return map[string]any{"content": renderDiscord(n), "components": rows,
		"allowed_mentions": map[string]any{"parse": []string{}}, "flags": 1 << 2}
}

// Send posts n in thread ("" is Channel) with one button per option. The ref is channel/message.
func (d *Discord) Send(ctx context.Context, thread string, n Note, buttons []Button) (string, error) {
	ch := d.Channel
	if thread != "" {
		ch = thread
	}
	if !snowflakeRe.MatchString(ch) {
		return "", errors.New("discord: bad channel id")
	}
	var r struct {
		ID string `json:"id"`
	}
	if err := d.call(ctx, "POST", "/channels/"+ch+"/messages", d.message(n, buttons), &r); err != nil {
		return "", err
	}
	if !snowflakeRe.MatchString(r.ID) {
		return "", errors.New("discord: message without an id")
	}
	return ch + "/" + r.ID, nil
}

// Update replaces the text of message ref and removes its buttons.
func (d *Discord) Update(ctx context.Context, ref string, n Note) error {
	ch, id, ok := strings.Cut(ref, "/")
	if !ok || !snowflakeRe.MatchString(ch) || !snowflakeRe.MatchString(id) {
		return errors.New("discord: bad message ref")
	}
	return d.call(ctx, "PATCH", "/channels/"+ch+"/messages/"+id, d.message(n, nil), nil)
}

type dUser struct {
	ID  string `json:"id"`
	Bot bool   `json:"bot"`
}

type dEvent struct {
	Op int             `json:"op"`
	S  *int64          `json:"s"`
	T  string          `json:"t"`
	D  json.RawMessage `json:"d"`
}

func (d *Discord) allowed(guild string, u dUser) bool {
	if u.Bot || u.ID == "" || guild == "" {
		return false
	}
	d.mu.Lock()
	g := d.guild
	d.mu.Unlock()
	return g != "" && guild == g && d.Users[u.ID]
}

// connect opens the Gateway, identifies and starts the reader and the heartbeat.
func (d *Discord) connect(ctx context.Context) (*dgw, error) {
	d.mu.Lock()
	known := d.guild
	d.mu.Unlock()
	if known == "" {
		var ch struct {
			Guild string `json:"guild_id"`
		}
		if err := d.call(ctx, "GET", "/channels/"+d.Channel, nil, &ch); err != nil {
			return nil, err
		}
		if !snowflakeRe.MatchString(ch.Guild) {
			return nil, errors.New("discord: connect.discord.channel is not a server channel")
		}
		d.mu.Lock()
		d.guild = ch.Guild
		d.mu.Unlock()
	}
	gwURL := d.Gateway
	if gwURL == "" {
		gwURL = DiscordGateway
	}
	gu, err := url.Parse(gwURL)
	if err != nil || (gu.Scheme != "wss" && gu.Scheme != "ws") || gu.Host == "" {
		return nil, errors.New("discord gateway: bad url")
	}
	ws, err := wsDial(ctx, gu)
	if err != nil {
		return nil, fmt.Errorf("discord gateway: %s", d.scrub(err.Error()))
	}
	b, err := ws.Read(time.Now().Add(15 * time.Second))
	var hello struct {
		Op int `json:"op"`
		D  struct {
			Interval int64 `json:"heartbeat_interval"`
		} `json:"d"`
	}
	if err != nil || json.Unmarshal(b, &hello) != nil || hello.Op != 10 || hello.D.Interval <= 0 {
		ws.Close()
		return nil, errors.New("discord gateway: no hello")
	}
	id, _ := json.Marshal(map[string]any{"op": 2, "d": map[string]any{"token": d.Token, "intents": discordIntents,
		"properties": map[string]string{"os": "linux", "browser": "smithy", "device": "smithy"}}})
	if err := ws.WriteText(id); err != nil {
		ws.Close()
		return nil, errors.New("discord gateway: identify failed")
	}
	g := &dgw{ws: ws, in: make(chan Inbound, 64), done: make(chan struct{})}
	var seq struct {
		sync.Mutex
		n     *int64
		acked bool
	}
	seq.acked = true
	beat := func() error {
		seq.Lock()
		p, _ := json.Marshal(map[string]any{"op": 1, "d": seq.n})
		seq.acked = false
		seq.Unlock()
		return ws.WriteText(p)
	}
	go func() {
		t := time.NewTicker(time.Duration(hello.D.Interval) * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-g.done:
				return
			case <-t.C:
				seq.Lock()
				ok := seq.acked
				seq.Unlock()
				if !ok || beat() != nil {
					ws.Close()
					return
				}
			}
		}
	}()
	go func() {
		defer close(g.done)
		for {
			b, err := ws.Read(time.Time{})
			if err != nil {
				g.err = errors.New("discord gateway: " + d.scrub(err.Error()))
				ws.Close()
				return
			}
			var ev dEvent
			if json.Unmarshal(b, &ev) != nil {
				continue
			}
			switch ev.Op {
			case 0:
				if ev.S != nil {
					seq.Lock()
					seq.n = ev.S
					seq.Unlock()
				}
				if in, ok := d.event(ev); ok {
					d.deliver(g, in)
				}
			case 1:
				_ = beat()
			case 11:
				seq.Lock()
				seq.acked = true
				seq.Unlock()
			case 7, 9:
				g.err = errors.New("discord gateway: reconnect asked")
				ws.Close()
				return
			}
		}
	}()
	return g, nil
}

// event turns a dispatch into an Inbound: a thread reply or a button press. A press is
// acknowledged (deferred update) whether it counts or not, so the client stops its spinner.
func (d *Discord) event(ev dEvent) (Inbound, bool) {
	switch ev.T {
	case "MESSAGE_CREATE":
		var m struct {
			Channel string `json:"channel_id"`
			Guild   string `json:"guild_id"`
			Author  dUser  `json:"author"`
			Content string `json:"content"`
			Hook    string `json:"webhook_id"`
		}
		if json.Unmarshal(ev.D, &m) != nil || m.Hook != "" || m.Content == "" || !snowflakeRe.MatchString(m.Channel) || !d.allowed(m.Guild, m.Author) {
			return Inbound{}, false
		}
		return Inbound{Thread: m.Channel, Text: clip(m.Content, 4000), By: m.Author.ID}, true
	case "INTERACTION_CREATE":
		var it struct {
			ID     string `json:"id"`
			Token  string `json:"token"`
			Type   int    `json:"type"`
			Guild  string `json:"guild_id"`
			Member *struct {
				User dUser `json:"user"`
			} `json:"member"`
			Data struct {
				Custom string `json:"custom_id"`
			} `json:"data"`
			Message *struct {
				ID      string `json:"id"`
				Channel string `json:"channel_id"`
			} `json:"message"`
		}
		if json.Unmarshal(ev.D, &it) != nil || it.Type != 3 {
			return Inbound{}, false
		}
		if snowflakeRe.MatchString(it.ID) && itokenRe.MatchString(it.Token) {
			go func(path string) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				_ = d.call(ctx, "POST", path, map[string]int{"type": 6}, nil)
				cancel()
			}("/interactions/" + it.ID + "/" + it.Token + "/callback")
		}
		if it.Member == nil || it.Message == nil || len(it.Data.Custom) > 100 || !d.allowed(it.Guild, it.Member.User) {
			return Inbound{}, false
		}
		return Inbound{Button: it.Data.Custom, Ref: it.Message.Channel + "/" + it.Message.ID, By: it.Member.User.ID}, true
	}
	return Inbound{}, false
}

// Receive waits up to Poll for thread replies and button presses, (re)connecting the Gateway.
func (d *Discord) Receive(ctx context.Context) ([]Inbound, error) {
	d.mu.Lock()
	g := d.gw
	d.mu.Unlock()
	if g == nil {
		var err error
		if g, err = d.connect(ctx); err != nil {
			return nil, err
		}
		d.mu.Lock()
		d.gw = g
		d.mu.Unlock()
	}
	t := time.NewTimer(d.poll())
	defer t.Stop()
	var out []Inbound
	for {
		select {
		case in := <-g.in:
			out = append(out, in)
			if len(g.in) == 0 {
				return out, nil
			}
		case <-g.done:
			d.mu.Lock()
			d.gw = nil
			d.mu.Unlock()
			for len(g.in) > 0 {
				out = append(out, <-g.in)
			}
			if len(out) > 0 {
				return out, nil
			}
			return nil, g.err
		case <-ctx.Done():
			g.ws.Close()
			d.mu.Lock()
			d.gw = nil
			d.mu.Unlock()
			return out, nil
		case <-t.C:
			return out, nil
		}
	}
}
