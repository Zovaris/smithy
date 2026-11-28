//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/connect"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/notify"
	"github.com/Zovaris/smithy/internal/report"
	"github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/text"
)

// KindInput is a user's message to a session (a reply in its chat thread).
const KindInput = "user_input"

const notesSource = "notes:connect"

func session(id string) string {
	kind, rest, _ := strings.Cut(id, ":")
	if kind == "perm" {
		s, _, _ := strings.Cut(rest, "/")
		return s
	}
	if kind != "merge" && kind != "decide" {
		return ""
	}
	p := strings.Split(rest, "/")
	if len(p) < 2 {
		return ""
	}
	return p[0] + "/" + p[1]
}

// NoteOf maps one store event to a notification. A ready or needs_decision stage is sent as the
// decision the daemon asks for it (with buttons), never twice.
func NoteOf(e store.Event) (connect.Note, bool) {
	switch e.Kind {
	case KindAsked:
		var d Decision
		if json.Unmarshal(e.Data, &d) != nil || d.ID == "" {
			return connect.Note{}, false
		}
		n := connect.Note{Key: "asked|" + d.ID, Kind: "decision", Session: session(d.ID), Title: "Decision needed",
			Text: line1(d.Question, 500), Decision: d.ID, Options: d.Options}
		if strings.HasPrefix(d.ID, "intake:") || strings.HasPrefix(d.ID, "perm:") {
			n.Text = lines(d.Question, 3500)
		}
		if strings.HasPrefix(d.ID, "merge:") {
			n.Kind, n.Title = "pr_ready", "PR ready for review"
		}
		if len(d.Options) == 0 {
			n.Text += "\nReply in this thread to answer."
		}
		return n, true
	case "event":
		var ev events.Event
		if json.Unmarshal(e.Data, &ev) != nil {
			return connect.Note{}, false
		}
		if ev.Kind == "stage" && (ev.Detail == "ready" || ev.Detail == "needs_decision") {
			return connect.Note{}, false
		}
		sess := ""
		if ev.Batch != "" && ev.Item != "" {
			sess = ev.Batch + "/" + ev.Item
		}
		switch {
		case ev.Kind == "merged":
			return connect.Note{Key: "merged|" + sess + "|" + ev.PR, Kind: "pr_merged", Session: sess, Title: "PR merged · " + line1(ev.Item, 80), URL: ev.PR}, true
		case ev.Kind == "ci" && (ev.Detail == "red" || ev.Detail == "failing" || ev.Detail == "failure"):
			return connect.Note{Key: "ci_red|" + sess + "|" + ev.PR + "|" + ev.T.Format(time.RFC3339), Kind: "ci_red", Session: sess, Title: "CI red · " + line1(ev.Item, 80), URL: ev.PR}, true
		}
		a, ok := notify.Classify(ev)
		if !ok {
			return connect.Note{}, false
		}
		n := connect.Note{Key: a.Dedupe, Kind: a.Kind, Title: a.Label, Text: line1(a.Reason, 300), URL: a.PR}
		n.Session = sess
		if a.Key != "" {
			n.Title += " · " + line1(a.Key, 80)
		}
		return n, true
	}
	return connect.Note{}, false
}

// lines cleans each line of s (keeping the line breaks and leading indent) and caps it at n runes.
func lines(s string, n int) string {
	ls := strings.Split(s, "\n")
	for i, l := range ls {
		ls[i] = strings.Repeat(" ", len(l)-len(strings.TrimLeft(l, " "))) + text.Clean(l)
	}
	return clipRunes(strings.Join(ls, "\n"), n)
}

// Input records text from a chat thread as the session's user input.
func (sv *Server) Input(session, text, by string) error {
	sv.mu.Lock()
	_, ok := sv.sessions[session]
	sv.mu.Unlock()
	if ok {
		return sv.SendSession(session, text, by)
	}
	_, err := sv.Append(KindInput, session, map[string]string{"text": text, "by": by, "via": "chat"})
	return err
}

// NotesOnce sends every store event since the notes cursor through h; an answered decision
// edits its messages. The first run starts at the newest event, so history is not replayed.
func (sv *Server) NotesOnce(ctx context.Context, h *connect.Hub) error {
	c, err := sv.st.Cursor(notesSource)
	if err != nil {
		return err
	}
	if c == "" {
		sv.mu.Lock()
		last := sv.lastSeq
		sv.mu.Unlock()
		_, err := sv.st.AppendCursor(nil, notesSource, strconv.FormatInt(last, 10))
		return err
	}
	from, _ := strconv.ParseInt(c, 10, 64)
	evs, err := sv.st.Since(from)
	if err != nil {
		return err
	}
	for _, e := range evs {
		if e.Kind == KindSessionText || e.Kind == KindSessionStatus {
			sv.chatNote(ctx, h, e)
		} else if e.Kind == KindAnswered {
			var o Outcome
			if json.Unmarshal(e.Data, &o) == nil {
				h.Resolve(ctx, o.ID, line1(o.Answer, 200), line1(o.By, 80))
			}
		} else if n, ok := NoteOf(e); ok {
			h.Notify(ctx, n)
		}
		if _, err := sv.st.AppendCursor(nil, notesSource, strconv.FormatInt(e.Seq, 10)); err != nil {
			return err
		}
	}
	return nil
}

// maxChatBuf caps how much of one turn's answer is kept for the chat thread (the end is kept).
const maxChatBuf = 64 << 10

// chatNote collects a chat session's text and sends it back through h once its turn ends (the
// session stops working). Sessions not started from a chat channel are skipped.
func (sv *Server) chatNote(ctx context.Context, h *connect.Hub, e store.Event) {
	if e.Session == "" || !h.IsChat(e.Session) {
		return
	}
	var d map[string]string
	if json.Unmarshal(e.Data, &d) != nil {
		return
	}
	if sv.chatBuf == nil {
		sv.chatBuf = map[string]string{}
	}
	if e.Kind == KindSessionText {
		b := sv.chatBuf[e.Session] + d["text"]
		if len(b) > maxChatBuf {
			b = b[len(b)-maxChatBuf:]
		}
		sv.chatBuf[e.Session] = b
		return
	}
	if d["status"] == StatusWorking || sv.chatBuf[e.Session] == "" {
		return
	}
	h.Reply(ctx, e.Session, strconv.FormatInt(e.Seq, 10), strings.ToValidUTF8(sv.chatBuf[e.Session], ""))
	delete(sv.chatBuf, e.Session)
}

// ChatStart starts a session for a message in a chat channel, in the default project with the
// default agent, and returns its id.
func (sv *Server) ChatStart(text, by string) (string, error) {
	s, err := sv.StartSession("", "", text, by)
	return s.ID, err
}

// RunNotes runs NotesOnce on every new event (or every interval) until ctx ends.
func (sv *Server) RunNotes(ctx context.Context, h *connect.Hub, every time.Duration) {
	for ctx.Err() == nil {
		sv.mu.Lock()
		wake := sv.wake
		sv.mu.Unlock()
		if err := sv.NotesOnce(ctx, h); err != nil && sv.log != nil {
			sv.log("notes: " + err.Error())
		}
		select {
		case <-ctx.Done():
		case <-wake:
		case <-time.After(every):
		}
	}
}

// WeeklyNote is the Monday report as a main-chat note, keyed by its week so each connector gets
// it once; ok is false outside Monday 09:00 onwards.
func WeeklyNote(es []events.Event, now time.Time) (connect.Note, bool) {
	if !report.Due(now) {
		return connect.Note{}, false
	}
	r := report.Week(es, now)
	return connect.Note{Key: "report|" + r.From.Format("2006-01-02"), Kind: "report", Title: "Weekly report", Text: r.Text()}, true
}

// RunReport posts the weekly report through h when it is due, checking every interval until ctx ends.
func RunReport(ctx context.Context, h *connect.Hub, every time.Duration) {
	for ctx.Err() == nil {
		now := time.Now()
		if report.Due(now) {
			es, err := events.Read(report.Start(now).AddDate(0, 0, -60), 0)
			if n, ok := WeeklyNote(es, now); ok && err == nil {
				h.Notify(ctx, n)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(every):
		}
	}
}
