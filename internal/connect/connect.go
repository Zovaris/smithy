// Package connect is the daemon's chat layer. Each connector has a notifications channel and,
// optionally, a chat channel. Every note worth a human goes to the notifications channel once
// (per-connector kinds), in a thread per session, with buttons for a decision's options. Inbound
// button presses answer the decision; a reply to a decision message answers it in the user's own
// words when the decision allows that; a reply in a session's thread goes to that session as user
// input; a message in the main chat that starts with "idea:" (or /idea) goes to Intake. A new
// message in the chat channel starts a session in a thread of its own, and the session's answers
// come back there. State (sent notes, threads, decision buttons) is a JSON file.
package connect

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Note is one notification. Key makes it unique (sent once per connector); Session groups it in
// a thread ("" is the main chat). Decision and Options make it answerable; Multi lets several
// options be picked together (the answer joins them with ", ").
type Note struct {
	Key, Kind, Session, Title, Text, URL string
	Decision                             string
	Options                              []string
	Multi                                bool
}

// Button is one decision option; Data is opaque to the connector and comes back in Inbound.Button.
type Button struct{ Label, Data string }

// Inbound is what a connector received: a button press (Button, on the message Ref), a
// multi-select submit (Picks: the picked buttons' Data, comma-separated, on Ref) or a text
// message (Thread, Text; Thread "" is the channel itself, Ref the message). Chat marks a message
// in the chat channel; ReplyTo is the ref of the message it replies to, if any. By is the
// sender's id on that connector.
type Inbound struct {
	Thread, Text, Button, Picks, Ref, By string
	ReplyTo                              string
	Chat                                 bool
}

// ChatRoot is the thread value that means the chat channel itself (not a thread in it).
const ChatRoot = "chat:"

// KindChat is a session's answer going back to the chat thread it was started from.
const KindChat = "chat"

// ChatStarter is a connector whose chat channel needs a thread made for a new conversation
// started by a top-level message (in.Ref); it returns the thread, as Receive reports it.
type ChatStarter interface {
	StartChat(ctx context.Context, in Inbound, title string) (string, error)
}

// MultiSelect is a Connector with a native multi-select (it renders a Note with Multi as one
// menu and reports the submit as Inbound.Picks).
type MultiSelect interface{ MultiSelect() bool }

// Rebuttoner is a Connector that can replace a sent message's buttons; without a native
// multi-select it gets one toggle button per option (☐/☑) and a Done button.
type Rebuttoner interface {
	Buttons(ctx context.Context, ref string, buttons []Button) error
}

// Connector is one chat service. Thread makes a thread for a session; Send posts a note (with
// buttons) and returns a ref Update can edit; Receive blocks up to its poll timeout.
type Connector interface {
	Name() string
	Thread(ctx context.Context, session, title string) (string, error)
	Send(ctx context.Context, thread string, n Note, buttons []Button) (ref string, err error)
	Update(ctx context.Context, ref string, n Note) error
	Receive(ctx context.Context) ([]Inbound, error)
}

type decision struct {
	ID      string            `json:"id"`
	Options []string          `json:"options,omitempty"`
	Refs    map[string]string `json:"refs,omitempty"`
	Multi   bool              `json:"multi,omitempty"`
	Picked  map[string][]int  `json:"picked,omitempty"` // toggled options per connector
	Note    Note              `json:"note"`
	At      time.Time         `json:"at"`
}

type state struct {
	Sent      map[string]time.Time `json:"sent"`
	Threads   map[string]string    `json:"threads"`
	Chats     map[string]bool      `json:"chats,omitempty"`
	Decisions map[string]*decision `json:"decisions"`
	Queue     []*queued            `json:"queue,omitempty"`
}

// queued is a send that failed, waiting for its next try.
type queued struct {
	Conn  string    `json:"conn"`
	Note  Note      `json:"note"`
	Tries int       `json:"tries"`
	Next  time.Time `json:"next"`
}

// Hub fans notes out to Conns and routes what they receive. Kinds gives a connector's enabled
// kinds; Answer, Input and Intake (any may be nil) take a decision answer, a session's user input
// and an idea from the main chat.
type Hub struct {
	Conns  []Connector
	File   string
	Kinds  func(name string) map[string]bool
	Answer func(id, answer, by string) error
	Input  func(session, text, by string) error
	Intake func(idea, by string) error
	// Start starts a session for a new chat-channel message and returns its id (nil: chat
	// messages that start nothing are dropped).
	Start func(text, by string) (string, error)
	// Free says whether decision id takes an answer in the user's own words.
	Free func(id string) bool
	Log  func(string)
	Now  func() time.Time // nil: time.Now; the retry backoff's clock

	mu     sync.Mutex
	st     *state
	logged map[string]string
}

// Keep is how long sent keys and decision buttons are remembered.
const Keep = 14 * 24 * time.Hour

func (h *Hub) load() *state {
	if h.st != nil {
		return h.st
	}
	s := &state{}
	if b, err := os.ReadFile(h.File); err == nil {
		_ = json.Unmarshal(b, s)
	}
	if s.Sent == nil {
		s.Sent = map[string]time.Time{}
	}
	if s.Threads == nil {
		s.Threads = map[string]string{}
	}
	if s.Decisions == nil {
		s.Decisions = map[string]*decision{}
	}
	if s.Chats == nil {
		s.Chats = map[string]bool{}
	}
	h.st = s
	return s
}

func (h *Hub) save() {
	now := time.Now()
	for k, t := range h.st.Sent {
		if now.Sub(t) > Keep {
			delete(h.st.Sent, k)
		}
	}
	for k, d := range h.st.Decisions {
		if now.Sub(d.At) > Keep {
			delete(h.st.Decisions, k)
		}
	}
	b, _ := json.MarshalIndent(h.st, "", "  ")
	if err := os.MkdirAll(filepath.Dir(h.File), 0o700); err != nil {
		return
	}
	tmp := h.File + ".tmp"
	if os.WriteFile(tmp, append(b, '\n'), 0o600) == nil {
		_ = os.Rename(tmp, h.File)
	}
}

func (h *Hub) logOnce(conn string, err error) {
	if h.logged == nil {
		h.logged = map[string]string{}
	}
	if err == nil {
		delete(h.logged, conn)
		return
	}
	if h.logged[conn] != err.Error() && h.Log != nil {
		h.Log("connect " + conn + ": " + err.Error())
	}
	h.logged[conn] = err.Error()
}

func handle(id string) string {
	s := sha256.Sum256([]byte(id))
	return hex.EncodeToString(s[:6])
}

// MaxQueued bounds the retry queue; past it the oldest failed send is dropped (and logged).
const MaxQueued = 256

func (h *Hub) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Queued is how many failed sends wait for a retry.
func (h *Hub) Queued() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.load().Queue)
}

// backoff is the wait after the tries-th failed send: 5s doubling, at most 10 minutes.
func backoff(tries int) time.Duration {
	d := 5 * time.Second
	for i := 1; i < tries && d < 10*time.Minute; i++ {
		d *= 2
	}
	return min(d, 10*time.Minute)
}

// Notify sends n to every connector that has n.Kind on and has not sent n.Key yet. A failed send
// is logged once and queued (kept in the state file, at most MaxQueued); Retry sends it again
// with backoff until it lands, so a chat outage loses no decision prompt.
func (h *Hub) Notify(ctx context.Context, n Note) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.load()
	for _, c := range h.Conns {
		name := c.Name()
		if n.Kind == KindChat {
			if !st.Chats[name+"|"+n.Session] {
				continue
			}
		} else if h.Kinds != nil && !h.Kinds(name)[n.Kind] {
			continue
		}
		key := name + "|" + n.Key
		if _, done := st.Sent[key]; done || slices.ContainsFunc(st.Queue, func(q *queued) bool { return q.Conn+"|"+q.Note.Key == key }) {
			continue
		}
		if err := h.send(ctx, c, n); err != nil {
			st.Queue = append(st.Queue, &queued{Conn: name, Note: n, Tries: 1, Next: h.now().Add(backoff(1))})
			if len(st.Queue) > MaxQueued {
				if h.Log != nil {
					h.Log("connect " + st.Queue[0].Conn + ": retry queue full, dropped " + st.Queue[0].Note.Key)
				}
				st.Queue = st.Queue[1:]
			}
		}
	}
	h.save()
}

// Retry sends every queued note whose backoff has passed; one that fails again waits longer.
func (h *Hub) Retry(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.load()
	if len(st.Queue) == 0 {
		return
	}
	now := h.now()
	keep := st.Queue[:0]
	for _, q := range st.Queue {
		i := slices.IndexFunc(h.Conns, func(c Connector) bool { return c.Name() == q.Conn })
		switch {
		case i < 0:
			continue
		case q.Next.After(now):
		case h.send(ctx, h.Conns[i], q.Note) == nil:
			continue
		default:
			q.Tries++
			q.Next = now.Add(backoff(q.Tries))
		}
		keep = append(keep, q)
	}
	st.Queue = keep
	h.save()
}

// send posts n on c (making its session thread first) and records it as sent; h.mu is held.
func (h *Hub) send(ctx context.Context, c Connector, n Note) error {
	st := h.st
	name := c.Name()
	var buttons []Button
	hd := ""
	if n.Decision != "" {
		hd = handle(n.Decision)
		if toggled(c, n) {
			buttons = toggles(hd, n.Options, nil)
		} else {
			for i, o := range n.Options {
				buttons = append(buttons, Button{Label: o, Data: hd + ":" + strconv.Itoa(i)})
			}
		}
		if len(n.Options) > 0 && h.Free != nil && h.Free(n.Decision) {
			n.Text = strings.TrimSpace(n.Text + "\nOr reply to this message to answer in your own words.")
		}
	}
	key := name + "|" + n.Key
	{
		thread := ""
		if n.Session != "" {
			tk := name + "|" + n.Session
			thread = st.Threads[tk]
			if thread == "" {
				t, err := c.Thread(ctx, n.Session, n.Session)
				h.logOnce(name+" thread", err)
				if err == nil && t != "" {
					thread = t
					st.Threads[tk] = t
				}
			}
		}
		ref, err := c.Send(ctx, thread, n, buttons)
		h.logOnce(name, err)
		if err != nil {
			return err
		}
		st.Sent[key] = time.Now()
		if hd != "" {
			d := st.Decisions[hd]
			if d == nil {
				d = &decision{ID: n.Decision, Options: n.Options, Note: n, At: time.Now(), Refs: map[string]string{}, Multi: n.Multi}
				st.Decisions[hd] = d
			}
			d.Refs[name] = ref
		}
	}
	return nil
}

// toggled reports whether c shows multi-select note n as toggle buttons plus Done.
func toggled(c Connector, n Note) bool {
	if !n.Multi || len(n.Options) < 2 {
		return false
	}
	if m, ok := c.(MultiSelect); ok && m.MultiSelect() {
		return false
	}
	_, ok := c.(Rebuttoner)
	return ok
}

// toggles is one ☐/☑ button per option (picked are checked) and a Done button.
func toggles(hd string, options []string, picked []int) []Button {
	var bs []Button
	for i, o := range options {
		box := "☐ "
		if slices.Contains(picked, i) {
			box = "☑ "
		}
		bs = append(bs, Button{Label: box + o, Data: hd + ":" + strconv.Itoa(i)})
	}
	return append(bs, Button{Label: "Done", Data: hd + ":done"})
}

// joinPicks is d's picked options in option order, joined with ", "; "" when none.
func joinPicks(d *decision, picked []int) string {
	var out []string
	for i, o := range d.Options {
		if slices.Contains(picked, i) {
			out = append(out, o)
		}
	}
	return strings.Join(out, ", ")
}

// Resolve edits every message that asked decision id to show its answer and drops its buttons.
func (h *Hub) Resolve(ctx context.Context, id, answer, by string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.load()
	hd := handle(id)
	d := st.Decisions[hd]
	if d == nil || d.ID != id {
		return
	}
	n := d.Note
	n.Options = nil
	n.Text = strings.TrimSpace(n.Text + "\nAnswered: " + answer + " (by " + by + ")")
	for _, c := range h.Conns {
		if ref := d.Refs[c.Name()]; ref != "" {
			h.logOnce(c.Name(), c.Update(ctx, ref, n))
		}
	}
	delete(st.Decisions, hd)
	h.save()
}

// Handle routes one inbound message from c: a known button answers its decision; a reply to a
// decision message answers it in the sender's words (when Free allows); a text in a known thread
// goes to that thread's session; a new chat-channel message starts a session in a thread of its
// own; an idea in the main chat goes to Intake. Anything else is dropped. Connectors only report
// what came from their allowed users, so nothing here runs for an unknown sender.
func (h *Hub) Handle(ctx context.Context, c Connector, in Inbound) {
	h.mu.Lock()
	st := h.load()
	name := c.Name()
	by := name + ":" + in.By
	var id, answer, session, idea, start string
	switch {
	case in.Picks != "":
		var d *decision
		var picked []int
		for _, p := range strings.Split(in.Picks, ",") {
			hd, idx, _ := strings.Cut(p, ":")
			i, err := strconv.Atoi(idx)
			pd := st.Decisions[hd]
			if pd == nil || err != nil || i < 0 || i >= len(pd.Options) || (d != nil && pd != d) {
				d = nil
				break
			}
			d, picked = pd, append(picked, i)
		}
		if d != nil && d.Multi && d.Refs[name] == in.Ref {
			id, answer = d.ID, joinPicks(d, picked)
		}
	case in.Button != "":
		hd, idx, _ := strings.Cut(in.Button, ":")
		i, err := strconv.Atoi(idx)
		d := st.Decisions[hd]
		switch {
		case d == nil || d.Refs[name] != in.Ref:
		case d.Multi && idx == "done":
			id, answer = d.ID, joinPicks(d, d.Picked[name])
		case err != nil || i < 0 || i >= len(d.Options):
		case d.Multi && toggled(c, d.Note):
			if d.Picked == nil {
				d.Picked = map[string][]int{}
			}
			p := d.Picked[name]
			if j := slices.Index(p, i); j >= 0 {
				p = slices.Delete(p, j, j+1)
			} else {
				p = append(p, i)
			}
			d.Picked[name] = p
			h.save()
			h.logOnce(name, c.(Rebuttoner).Buttons(ctx, in.Ref, toggles(hd, d.Options, p)))
		default:
			id, answer = d.ID, d.Options[i]
		}
		if answer == "" {
			id = ""
		}
	case strings.TrimSpace(in.Text) == "":
	case h.freeReply(name, cmp.Or(in.ReplyTo, in.Thread)) != "":
		id, answer = h.freeReply(name, cmp.Or(in.ReplyTo, in.Thread)), strings.TrimSpace(in.Text)
	case in.Thread != "" && in.Thread != ChatRoot && h.sessionOf(name, in.Thread) != "":
		session = h.sessionOf(name, in.Thread)
	case IdeaOf(in.Text) != "" && (in.Thread == "" || in.Thread == ChatRoot):
		idea = IdeaOf(in.Text)
	case in.Chat:
		start = strings.TrimSpace(in.Text)
	}
	h.mu.Unlock()
	var err error
	switch {
	case id != "" && h.Answer != nil:
		err = h.Answer(id, answer, by)
	case session != "" && h.Input != nil:
		err = h.Input(session, in.Text, by)
	case idea != "" && h.Intake != nil:
		err = h.Intake(idea, by)
	case start != "" && h.Start != nil:
		err = h.startChat(ctx, c, in, start, by)
	default:
		return
	}
	h.mu.Lock()
	h.logOnce(name+" inbound", err)
	h.mu.Unlock()
}

// sessionOf is the session whose thread on connector name is thread, or ""; h.mu is held.
func (h *Hub) sessionOf(name, thread string) string {
	prefix := name + "|"
	for k, t := range h.st.Threads {
		if t == thread && strings.HasPrefix(k, prefix) {
			return strings.TrimPrefix(k, prefix)
		}
	}
	return ""
}

// freeReply is the open decision whose message on connector name is ref, when it takes an answer
// in the user's own words; h.mu is held.
func (h *Hub) freeReply(name, ref string) string {
	if h.Free == nil || ref == "" {
		return ""
	}
	for _, d := range h.st.Decisions {
		if d.Refs[name] == ref && h.Free(d.ID) {
			return d.ID
		}
	}
	return ""
}

// startChat makes the conversation's thread (when the connector needs one made), starts its
// session with text and binds the two, so replies continue the session and its answers come back.
func (h *Hub) startChat(ctx context.Context, c Connector, in Inbound, text, by string) error {
	name := c.Name()
	thread := in.Thread
	if thread == "" || thread == ChatRoot {
		cs, ok := c.(ChatStarter)
		if !ok {
			return errors.New("chat: this connector cannot start a thread")
		}
		t, err := cs.StartChat(ctx, in, line(text, 80))
		if err != nil {
			return err
		}
		thread = t
	}
	session, err := h.Start(text, by)
	if err != nil || session == "" {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.load()
	st.Threads[name+"|"+session] = thread
	st.Chats[name+"|"+session] = true
	h.save()
	return nil
}

// line is the first line of s, at most n runes.
func line(s string, n int) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return clip(s, n)
}

// ChatChunk is the most runes of a session answer sent in one message; a longer answer is split.
const ChatChunk = 1800

// Reply sends a chat session's answer back to the thread it was started from, split into chunks
// of at most ChatChunk runes; key makes it unique. Sessions not started from chat get nothing.
func (h *Hub) Reply(ctx context.Context, session, key, text string) {
	r := []rune(strings.TrimSpace(text))
	for i := 0; len(r) > 0; i++ {
		n := min(len(r), ChatChunk)
		if n < len(r) {
			if j := lastBreak(r[:n]); j > n/2 {
				n = j
			}
		}
		h.Notify(ctx, Note{Key: "chat|" + session + "|" + key + "|" + strconv.Itoa(i), Kind: KindChat, Session: session, Text: string(r[:n])})
		r = []rune(strings.TrimLeft(string(r[n:]), "\n "))
	}
}

func lastBreak(r []rune) int {
	for i := len(r) - 1; i > 0; i-- {
		if r[i] == '\n' {
			return i
		}
	}
	return -1
}

// IsChat reports whether session was started from a chat channel on any connector.
func (h *Hub) IsChat(session string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.load()
	for _, c := range h.Conns {
		if st.Chats[c.Name()+"|"+session] {
			return true
		}
	}
	return false
}

// Test sends a test message to every connector's notifications channel and, where one is set,
// its chat channel; the result names each place and how it went.
func (h *Hub) Test(ctx context.Context) []string {
	var out []string
	for _, c := range h.Conns {
		places := []string{""}
		if cc, ok := c.(interface{ HasChat() bool }); ok && cc.HasChat() {
			places = append(places, ChatRoot)
		}
		for _, p := range places {
			where := c.Name() + " notifications"
			if p == ChatRoot {
				where = c.Name() + " chat"
			}
			_, err := c.Send(ctx, p, Note{Kind: "test", Title: "smithy test", Text: "This is the " + where + " channel."}, nil)
			if err != nil {
				out = append(out, where+": "+err.Error())
			} else {
				out = append(out, where+": sent")
			}
		}
	}
	return out
}

// Run receives from every connector until ctx ends, backing off after an error, and retries
// queued sends every few seconds.
func (h *Hub) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, c := range h.Conns {
		if l, ok := c.(interface{ SetLog(func(string)) }); ok && h.Log != nil {
			l.SetLog(h.Log)
		}
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				h.Retry(ctx)
			}
		}
	}()
	for _, c := range h.Conns {
		wg.Add(1)
		go func(c Connector) {
			defer wg.Done()
			for ctx.Err() == nil {
				ins, err := c.Receive(ctx)
				h.mu.Lock()
				h.logOnce(c.Name()+" receive", err)
				h.mu.Unlock()
				if err != nil {
					select {
					case <-ctx.Done():
					case <-time.After(5 * time.Second):
					}
					continue
				}
				for _, in := range ins {
					h.Handle(ctx, c, in)
				}
			}
		}(c)
	}
	wg.Wait()
}

// IdeaOf returns the idea in a main-chat message that starts with "idea:" or "/idea" (a bot
// suffix like /idea@name is allowed), or "" for any other message.
func IdeaOf(msg string) string {
	s := strings.TrimSpace(msg)
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "idea:"):
		s = s[len("idea:"):]
	case strings.HasPrefix(low, "/idea@"):
		_, s, _ = strings.Cut(s, " ")
	case low == "/idea" || strings.HasPrefix(low, "/idea "):
		s = s[len("/idea"):]
	default:
		return ""
	}
	return strings.TrimSpace(s)
}
