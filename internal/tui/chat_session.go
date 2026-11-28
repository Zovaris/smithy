//go:build harness

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/usage"
)

// sessionAPI is the part of the daemon client the chat drives sessions with.
type sessionAPI interface {
	StartSession(ctx context.Context, agent, dir, msg string) (daemon.Session, error)
	SendSession(ctx context.Context, id, text string) error
	StopSession(ctx context.Context, id string) error
}

type sessionStartedMsg struct {
	s   daemon.Session
	err error
}

type sessionErrMsg struct {
	what string
	err  error
}

// noise is one daemon event kept out of the transcript: the status line shows the latest, /log
// all of them.
type noise struct {
	at    time.Time
	text  string
	quiet bool // only in /log: what another part of the screen already shows
}

const maxNoise = 500

// send is enter on a message: the first one starts a session with the last used agent, later
// ones go to the open session. With no daemon behind the chat it only shows the text.
func (m chatView) send(text string) (chatView, tea.Cmd) {
	m.offset = 0
	if m.sess == nil {
		m.push(chatMsg{who: "you", text: text})
		return m, nil
	}
	api, dir, id := m.sess, m.dir, m.current
	if id == "" && m.proj.dir == "" {
		if pa, ok := api.(projectAPI); ok {
			return m.askProject(pa, text)
		}
	}
	if m.proj.dir != "" {
		dir = m.proj.dir
	}
	if id == "" {
		m.status("starting a session…")
		return m, func() tea.Msg {
			s, err := api.StartSession(context.Background(), "", dir, text)
			return sessionStartedMsg{s: s, err: err}
		}
	}
	return m, func() tea.Msg {
		if err := api.SendSession(context.Background(), id, text); err != nil {
			return sessionErrMsg{what: "send", err: err}
		}
		return nil
	}
}

func (m chatView) stopSession() (chatView, tea.Cmd) {
	if m.current == "" || m.sess == nil {
		m.system("no session is open; /sessions lists them")
		return m, nil
	}
	api, id := m.sess, m.current
	return m, func() tea.Msg {
		if err := api.StopSession(context.Background(), id); err != nil {
			return sessionErrMsg{what: "stop", err: err}
		}
		return nil
	}
}

// open shows session id's conversation, keeping the one on screen for when it comes back.
func (m *chatView) open(id string) {
	if id == m.current {
		return
	}
	if m.convs == nil {
		m.convs = map[string][]*chatMsg{}
	}
	if m.current != "" || len(m.msgs) > 0 {
		m.convs[m.current] = m.msgs
	}
	m.current, m.msgs, m.offset, m.focus = id, m.convs[id], 0, -1
	m.proj = projectPick{}
	delete(m.convs, id)
	for _, c := range m.msgs {
		c.lines = nil
	}
	*m.at = anchor{}
}

// newSession clears the screen; the next message starts a session.
func (m *chatView) newSession() {
	m.open("")
	m.msgs = nil
}

// into appends c to session id's conversation, on screen when it is the open one.
func (m *chatView) into(id string, c chatMsg) {
	if id == m.current {
		m.push(c)
		return
	}
	if m.convs == nil {
		m.convs = map[string][]*chatMsg{}
	}
	m.convs[id] = append(m.convs[id], &c)
}

// sessionEvent handles the session_* kinds; false for any other kind.
func (m *chatView) sessionEvent(kind, id string, data json.RawMessage) bool {
	switch kind {
	case daemon.KindSessionStarted:
		var s daemon.Session
		if json.Unmarshal(data, &s) == nil {
			m.meta(id, s)
		}
	case daemon.KindSessionLinks:
		var l daemon.SessionLinks
		if json.Unmarshal(data, &l) == nil {
			for i := range m.sessions {
				if m.sessions[i].id == id {
					m.sessions[i].links = &l
				}
			}
		}
	case daemon.KindSessionStatus:
		var d struct{ Status, Detail string }
		if json.Unmarshal(data, &d) == nil {
			m.track(id, d.Status)
			if d.Status == daemon.StatusFailed && d.Detail != "" {
				m.into(id, chatMsg{who: "daemon", kind: "system", text: m.th.g.fail + " " + d.Detail})
			}
		}
	case daemon.KindSessionMessage:
		var d struct{ Role, Text string }
		if json.Unmarshal(data, &d) != nil || strings.TrimSpace(d.Text) == "" {
			return true
		}
		if d.Role == "user" {
			m.into(id, chatMsg{who: "you", text: clean(d.Text)})
			m.now(id, "")
			return true
		}
		if m.lastText(id) != strings.TrimSpace(d.Text) {
			m.into(id, chatMsg{who: "agent", text: strings.TrimSpace(d.Text)})
			m.addStreamed(id, len(d.Text))
		}
	case daemon.KindSessionText:
		var d struct{ Text string }
		if json.Unmarshal(data, &d) != nil || strings.TrimSpace(d.Text) == "" {
			return true
		}
		if txt := strings.TrimSpace(d.Text); strings.HasPrefix(txt, "{") && json.Valid([]byte(txt)) {
			m.status("agent output: " + short(txt, 200))
			return true
		}
		m.into(id, chatMsg{who: "daemon", text: strings.TrimSpace(d.Text)})
		m.addStreamed(id, len(d.Text))
	case daemon.KindSessionTool:
		var t daemon.ToolEvent
		if json.Unmarshal(data, &t) == nil && t.Name != "" {
			t.Output, t.Args = m.rel(t.Output), json.RawMessage(m.rel(string(t.Args)))
			ct := toolOf(t)
			m.into(id, chatMsg{who: "daemon", tool: &ct})
			m.now(id, ct.Name+" "+ct.Arg)
		}
	case daemon.KindSessionDiff:
		var d struct {
			Stat, Patch string
		}
		if json.Unmarshal(data, &d) == nil && d.Patch != "" {
			ct := chatTool{Name: "Diff", Arg: diffFiles(d.Stat), Stat: diffStat(d.Patch), Status: "ok", Lines: patchLines(d.Patch)}
			m.into(id, chatMsg{who: "daemon", tool: &ct})
		}
	case daemon.KindSessionUsage:
		var u usage.Usage
		if json.Unmarshal(data, &u) == nil {
			if m.toks == nil {
				m.toks = map[string]int64{}
			}
			m.toks[id] += u.Input + u.Output
		}
		if u.CostUSD != nil {
			if m.cost == nil {
				m.cost = map[string]float64{}
			}
			m.cost[id] += *u.CostUSD
		}
	default:
		return false
	}
	return true
}

// lastText is the text of session id's last message, "" when it is not agent text.
func (m chatView) lastText(id string) string {
	msgs := m.convs[id]
	if id == m.current {
		msgs = m.msgs
	}
	if n := len(msgs); n > 0 && msgs[n-1].tool == nil && msgs[n-1].who != "you" {
		return msgs[n-1].text
	}
	return ""
}

func (m *chatView) meta(id string, s daemon.Session) {
	m.track(id, s.Status)
	for i := range m.sessions {
		if m.sessions[i].id == id {
			m.sessions[i].title, m.sessions[i].agent, m.sessions[i].model, m.sessions[i].effort = s.Title, s.Agent, s.Model, s.Effort
			if s.Dir != "" {
				m.sessions[i].dir = s.Dir
			}
			m.sessions[i].prs = s.PRs
			if !s.Created.IsZero() {
				m.sessions[i].since = s.Created
			}
		}
	}
}

// here reports whether s belongs to the folder the chat runs in.
func (m chatView) here(s chatSession) bool {
	return s.dir == "" || daemon.InProject(s.dir, m.dir) || daemon.InProject(s.dir, m.root)
}

// listed is the agents panel's rows: this folder's sessions, then the other projects' when
// allProj is on.
func (m chatView) listed() []chatSession {
	var in, out []chatSession
	for _, s := range m.sessions {
		if m.here(s) {
			in = append(in, s)
		} else {
			out = append(out, s)
		}
	}
	if m.allProj {
		return append(in, out...)
	}
	return in
}

func projHint(all bool) string {
	if all {
		return "here"
	}
	return "all"
}

// listedAt is the index of session id in the agents panel's rows, -1 when not listed.
func (m chatView) listedAt(id string) int {
	for i, s := range m.listed() {
		if s.id == id && id != "" {
			return i
		}
	}
	return -1
}

func (m *chatView) now(id, what string) {
	for i := range m.sessions {
		if m.sessions[i].id == id {
			m.sessions[i].now = what
		}
	}
}

// toolOf turns a session tool call into a transcript block: its main argument, its output's
// first lines and a line count.
func toolOf(t daemon.ToolEvent) chatTool {
	ct := chatTool{Name: t.Name, Arg: toolArg(t.Args), Status: "ok", Took: t.Took}
	if t.IsError {
		ct.Status = "failed"
	}
	if out := strings.TrimRight(t.Output, "\n"); strings.TrimSpace(out) != "" {
		lines := strings.Split(out, "\n")
		for i, l := range lines {
			lines[i] = clean(strings.ReplaceAll(l, "\t", "  "))
		}
		ct.Stat = fmt.Sprintf("%d lines", len(lines))
		if len(lines) == 1 {
			ct.Stat = short(lines[0], 24)
		}
		if len(lines) > 12 {
			lines = append(lines[:12], fmt.Sprintf("… %d more lines", len(lines)-12))
		}
		ct.Lines = lines
	}
	return ct
}

// toolArg is the argument a person reads first: the command, the path or the pattern.
func toolArg(raw json.RawMessage) string {
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil {
		return short(clean(string(raw)), 80)
	}
	for _, k := range []string{"command", "cmd", "file_path", "path", "pattern", "query", "url", "description"} {
		if s, ok := args[k].(string); ok && s != "" {
			return short(clean(s), 80)
		}
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s, ok := args[k].(string); ok && s != "" {
			return short(clean(s), 80)
		}
	}
	return ""
}

func diffFiles(stat string) string {
	var files []string
	for _, l := range strings.Split(stat, "\n") {
		if f, _, ok := strings.Cut(l, "|"); ok {
			files = append(files, filepath.Base(strings.TrimSpace(f)))
		}
	}
	if len(files) > 3 {
		return strings.Join(files[:3], " ") + fmt.Sprintf(" +%d", len(files)-3)
	}
	return strings.Join(files, " ")
}

func diffStat(patch string) string {
	add, del := 0, 0
	for _, l := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
		case strings.HasPrefix(l, "+"):
			add++
		case strings.HasPrefix(l, "-"):
			del++
		}
	}
	return fmt.Sprintf("+%d −%d", add, del)
}

// status puts text on the status line and in the log.
func (m *chatView) status(text string) { m.logNoise(noise{at: time.Now(), text: text}) }

// logNoise keeps n for /log, once per text; the status line shows it unless it is quiet.
func (m *chatView) logNoise(n noise) {
	for _, o := range m.noise {
		if o.text == n.text {
			return
		}
	}
	m.noise = append(m.noise, n)
	if len(m.noise) > maxNoise {
		m.noise = m.noise[len(m.noise)-maxNoise:]
	}
}

// patchLines is a patch without its git header lines: file names, hunks and changes only.
func patchLines(patch string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(patch, "\n"), "\n") {
		if strings.HasPrefix(l, "diff --git") || strings.HasPrefix(l, "index ") || strings.HasPrefix(l, "--- ") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// rel shows paths under the chat's folder relative to it.
func (m chatView) rel(s string) string {
	if m.dir == "" {
		return s
	}
	dirs := []string{m.dir}
	if r, err := filepath.EvalSymlinks(m.dir); err == nil && r != m.dir {
		dirs = append(dirs, r)
	}
	for _, d := range dirs {
		s = strings.ReplaceAll(s, strings.TrimSuffix(d, "/")+"/", "")
	}
	return s
}
