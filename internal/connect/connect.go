// Package connect is the daemon's notification layer: every note worth a human goes to each
// enabled chat connector once (per-connector kinds), in a thread per session, with buttons for a
// decision's options. Inbound button presses answer the decision; a reply in a session's thread
// goes to that session as user input; a message in the main chat that starts with "idea:" (or
// /idea) goes to Intake. State (sent notes, threads, decision buttons) is a JSON file.
package connect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Note is one notification. Key makes it unique (sent once per connector); Session groups it in
// a thread ("" is the main chat). Decision and Options make it answerable.
type Note struct {
	Key, Kind, Session, Title, Text, URL string
	Decision                             string
	Options                              []string
}

// Button is one decision option; Data is opaque to the connector and comes back in Inbound.Button.
type Button struct{ Label, Data string }

// Inbound is what a connector received: a button press (Button, on the message Ref) or a text
// message (Thread, Text; Thread "" is the main chat). By is the sender's id on that connector.
type Inbound struct{ Thread, Text, Button, Ref, By string }

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
	Note    Note              `json:"note"`
	At      time.Time         `json:"at"`
}

type state struct {
	Sent      map[string]time.Time `json:"sent"`
	Threads   map[string]string    `json:"threads"`
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
	Log    func(string)
	Now    func() time.Time // nil: time.Now; the retry backoff's clock

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
		if h.Kinds != nil && !h.Kinds(name)[n.Kind] {
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
		for i, o := range n.Options {
			buttons = append(buttons, Button{Label: o, Data: hd + ":" + strconv.Itoa(i)})
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
				d = &decision{ID: n.Decision, Options: n.Options, Note: n, At: time.Now(), Refs: map[string]string{}}
				st.Decisions[hd] = d
			}
			d.Refs[name] = ref
		}
	}
	return nil
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

// Handle routes one inbound message from c: a known button answers its decision, a text in a
// known session thread goes to that session. Anything else is dropped.
func (h *Hub) Handle(ctx context.Context, c Connector, in Inbound) {
	h.mu.Lock()
	st := h.load()
	by := c.Name() + ":" + in.By
	var id, answer, session, idea string
	if in.Button != "" {
		hd, idx, _ := strings.Cut(in.Button, ":")
		i, err := strconv.Atoi(idx)
		if d := st.Decisions[hd]; d != nil && err == nil && i >= 0 && i < len(d.Options) && d.Refs[c.Name()] == in.Ref {
			id, answer = d.ID, d.Options[i]
		}
	} else if in.Thread == "" {
		idea = IdeaOf(in.Text)
	} else if strings.TrimSpace(in.Text) != "" {
		prefix := c.Name() + "|"
		for k, t := range st.Threads {
			if t == in.Thread && strings.HasPrefix(k, prefix) {
				session = strings.TrimPrefix(k, prefix)
			}
		}
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
	default:
		return
	}
	h.mu.Lock()
	h.logOnce(c.Name()+" inbound", err)
	h.mu.Unlock()
}

// Run receives from every connector until ctx ends, backing off after an error, and retries
// queued sends every few seconds.
func (h *Hub) Run(ctx context.Context) {
	var wg sync.WaitGroup
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
