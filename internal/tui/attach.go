//go:build harness

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	neturl "net/url"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/charmbracelet/colorprofile"
)

// AttachUsage is the text for `smithy attach --help`.
const AttachUsage = `usage: smithy attach [url]
Shows a smithy daemon live: its events and the decisions waiting on you. Keys 1-9 answer the
selected decision (the first answer wins; a later one shows the recorded outcome), up/down pick a
decision, q or Esc detaches (the daemon keeps running). A lost connection retries and resumes
after the last event seen, so nothing repeats or goes missing across a daemon restart.
  url   default: the local daemon (token from ~/.smithy/daemon.token); a tailscale serve URL
        needs no token (daemon.allow decides)`

const attachLines = 12

type attachView struct {
	th      theme
	url     string
	msgs    <-chan daemon.ClientMsg
	decide  func(id, answer string) (daemon.Outcome, error)
	conn    string // connecting, connected, reconnecting
	connErr string
	last    int64
	recent  []string
	pending []daemon.Decision
	sel     int
	note    string
	width   int
}

type attachDecided struct {
	o   daemon.Outcome
	err error
}

func newAttachView(msgs <-chan daemon.ClientMsg, url string, th theme) attachView {
	return attachView{th: th, url: url, msgs: msgs, conn: "connecting", width: 80}
}

// Attach runs `smithy attach`.
func Attach(args []string) int {
	var url string
	for _, a := range args {
		switch {
		case a == "-h" || a == "--help":
			fmt.Println(AttachUsage)
			return 0
		case strings.HasPrefix(a, "-"):
			fmt.Fprintln(os.Stderr, "smithy attach: unknown flag", a)
			return 2
		case url != "":
			fmt.Fprintln(os.Stderr, "smithy attach: one url at most")
			return 2
		default:
			url = a
		}
	}
	tok, terr := daemon.LoadToken(daemon.TokenFile(), false)
	if url == "" {
		i := daemon.Running()
		if i == nil {
			fmt.Fprintln(os.Stderr, "smithy attach: no daemon running; start one with: smithy daemon --bg")
			return 1
		}
		if terr != nil {
			fmt.Fprintln(os.Stderr, "smithy attach: token:", terr)
			return 1
		}
		url = i.URL()
	} else if !loopbackURL(url) {
		tok = "" // the token stays on this machine; remote access goes by daemon.allow
	}
	c := &daemon.Client{Base: url, Token: tok}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := c.State(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy attach:", err)
		return 1
	}
	ch := make(chan daemon.ClientMsg, 64)
	go c.Follow(ctx, max(st.LastSeq-attachLines, 0), ch)

	p := colorprofile.TrueColor
	if os.Getenv("NO_COLOR") != "" {
		p = colorprofile.Ascii
	}
	m := newAttachView(ch, url, newTheme(true, p, utf8Locale()))
	m.pending = st.Pending
	m.decide = func(id, answer string) (daemon.Outcome, error) { return c.Decide(ctx, id, answer) }
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "smithy attach:", err)
		return 1
	}
	return 0
}

func loopbackURL(s string) bool {
	u, err := neturl.Parse(s)
	if err != nil {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (m attachView) Init() tea.Cmd { return m.wait() }

func (m attachView) wait() tea.Cmd {
	if m.msgs == nil {
		return nil
	}
	return func() tea.Msg { return <-m.msgs }
}

func (m attachView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case daemon.ClientMsg:
		switch {
		case msg.Connected:
			m.conn, m.connErr = "connected", ""
		case msg.Err != nil:
			m.conn, m.connErr = "reconnecting", msg.Err.Error()
		case msg.Event != nil && msg.Event.Seq > m.last:
			m.last = msg.Event.Seq
			m.apply(msg.Event.Kind, msg.Event.Session, msg.Event.Time.Local().Format("15:04:05"), msg.Event.Data)
		}
		return m, m.wait()
	case attachDecided:
		switch {
		case msg.err != nil:
			m.note = "answer failed: " + msg.err.Error()
		case msg.o.Won:
			m.note = "answered: " + msg.o.Answer
			m.drop(msg.o.ID)
		default:
			m.note = "already answered: " + msg.o.Answer + by(msg.o.By)
			m.drop(msg.o.ID)
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyPressMsg:
		switch k := msg.String(); k {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			m.sel = max(m.sel-1, 0)
		case "down", "j":
			m.sel = min(m.sel+1, max(len(m.pending)-1, 0))
		default:
			if len(k) == 1 && k[0] >= '1' && k[0] <= '9' && m.sel < len(m.pending) && m.decide != nil {
				d := m.pending[m.sel]
				opts := choices(d)
				if n := int(k[0] - '1'); n < len(opts) {
					m.note = "sending " + opts[n] + "…"
					return m, func() tea.Msg {
						o, err := m.decide(d.ID, opts[n])
						return attachDecided{o, err}
					}
				}
			}
		}
	}
	return m, nil
}

func by(who string) string {
	if who == "" {
		return ""
	}
	return " (" + who + ")"
}

// choices are a decision's buttons; a question without options is yes or no.
func choices(d daemon.Decision) []string {
	if len(d.Options) == 0 {
		return []string{"yes", "no"}
	}
	return d.Options
}

func (m *attachView) drop(id string) {
	for i, d := range m.pending {
		if d.ID == id {
			m.pending = append(m.pending[:i:i], m.pending[i+1:]...)
			break
		}
	}
	m.sel = min(m.sel, max(len(m.pending)-1, 0))
}

func (m *attachView) apply(kind, session, at string, data json.RawMessage) {
	line := kind
	switch kind {
	case daemon.KindAsked:
		var d daemon.Decision
		if json.Unmarshal(data, &d) != nil || d.ID == "" {
			return
		}
		for _, p := range m.pending {
			if p.ID == d.ID {
				return
			}
		}
		m.pending = append(m.pending, d)
		line = "asked: " + d.Question
	case daemon.KindAnswered:
		var o daemon.Outcome
		if json.Unmarshal(data, &o) != nil {
			return
		}
		m.drop(o.ID)
		line = "answered: " + o.Answer + by(o.By)
	case "log":
		var d struct {
			Line string `json:"line"`
		}
		if json.Unmarshal(data, &d) == nil {
			line = clean(d.Line)
		}
	default:
		if session != "" {
			line += " " + session
		}
	}
	m.recent = append(m.recent, at+" "+line)
	if len(m.recent) > attachLines {
		m.recent = m.recent[len(m.recent)-attachLines:]
	}
}

func (m attachView) View() tea.View { return tea.NewView(strings.Join(m.lines(), "\n")) }

func (m attachView) lines() []string {
	t := m.th
	mut, val := t.fg(t.muted), t.fg(t.text)
	w := max(m.width, 40)
	dot := mut.Render(" " + t.g.dot + " ")
	conn := t.fg(t.ok).Render(t.g.on + " connected")
	switch m.conn {
	case "connecting":
		conn = t.fg(t.warn).Render(t.g.half + " connecting")
	case "reconnecting":
		conn = t.fg(t.err).Render(t.g.off+" reconnecting") + dot + mut.Render(short(m.connErr, max(w-50, 10)))
	}
	out := []string{" " + t.fg(t.accent).Bold(!t.plain).Render("smithy attach") + "  " + conn + dot + mut.Render(m.url) +
		dot + mut.Render(fmt.Sprintf("seq %d", m.last)), ""}
	out = append(out, mut.Render(" decisions"))
	if len(m.pending) == 0 {
		out = append(out, mut.Italic(true).Render("  nothing waits on you"))
	}
	for i, d := range m.pending {
		mark := "  "
		q := val
		if i == m.sel {
			mark = t.fg(t.warn).Render(" ?")
			q = t.fg(t.strong).Bold(!t.plain)
		}
		out = append(out, mark+" "+q.Render(short(d.Question, w-4)))
		if i == m.sel {
			var b []string
			for n, o := range choices(d) {
				if n == 9 {
					break
				}
				b = append(b, fmt.Sprintf("[%d] %s", n+1, o))
			}
			out = append(out, "    "+val.Render(short(strings.Join(b, "  "), w-5)))
		}
	}
	if m.note != "" {
		out = append(out, "  "+t.fg(t.info).Render(short(m.note, w-3)))
	}
	out = append(out, "", mut.Render(" events"))
	if len(m.recent) == 0 {
		out = append(out, mut.Italic(true).Render("  nothing yet"))
	}
	for _, l := range m.recent {
		out = append(out, "  "+val.Render(short(l, w-3)))
	}
	out = append(out, "", mut.Render(" 1-9 answer"+dot+"↑/↓ pick"+dot+"q detach"))
	return out
}
