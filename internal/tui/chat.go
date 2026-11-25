//go:build harness

package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// ChatUsage is the text for `smithy chat --help`; `smithy` with no arguments opens it too.
const ChatUsage = `usage: smithy chat [url]
A fullscreen chat over a smithy daemon: its events stream into the transcript and the input stays
pinned at the bottom. A lost connection retries and resumes after the last event seen.
Keys: enter send; pgup/pgdn, wheel or ctrl+u/ctrl+d scroll; home/end top/bottom; esc or ctrl+c quit.
  url   default: the local daemon (token from ~/.smithy/daemon.token); a tailscale serve URL
        needs no token (daemon.allow decides)`

// chatMsg is one transcript entry; lines caches its wrap at width, filled only when drawn.
type chatMsg struct {
	who, at, text string
	width         int
	lines         []string
}

// anchor maps an offset to message i with r of its rows hidden below the screen; width is the
// wrap it was measured at.
type anchor struct{ offset, i, r, width int }

type chatView struct {
	th            theme
	url           string
	stream        <-chan daemon.ClientMsg
	conn, connErr string
	last          int64
	msgs          []*chatMsg
	offset        int     // rows scrolled up from the bottom
	at            *anchor // where offset last landed, so a frame walks only from there
	input         textinput.Model
	width, height int
}

func newChatView(stream <-chan daemon.ClientMsg, url string, th theme) chatView {
	in := textinput.New()
	in.Prompt = th.fg(th.accent).Render(th.g.prompt) + " "
	in.Placeholder = "Ask, or / for commands"
	in.Focus()
	return chatView{th: th, url: url, stream: stream, conn: "connecting", input: in, width: 80, height: 24, at: &anchor{}}
}

// Chat runs `smithy chat` (and bare `smithy` in the harness build).
func Chat(args []string) int {
	var url string
	for _, a := range args {
		switch {
		case a == "-h" || a == "--help":
			fmt.Println(ChatUsage)
			return 0
		case strings.HasPrefix(a, "-"):
			fmt.Fprintln(os.Stderr, "smithy chat: unknown flag", a)
			return 2
		case url != "":
			fmt.Fprintln(os.Stderr, "smithy chat: one url at most")
			return 2
		default:
			url = a
		}
	}
	tok, terr := daemon.LoadToken(daemon.TokenFile(), false)
	if url == "" {
		i := daemon.Running()
		if i == nil {
			fmt.Fprintln(os.Stderr, "smithy chat: no daemon running; start one with: smithy daemon --bg")
			return 1
		}
		if terr != nil {
			fmt.Fprintln(os.Stderr, "smithy chat: token:", terr)
			return 1
		}
		url = i.URL()
	} else if !loopbackURL(url) {
		tok = ""
	}
	c := &daemon.Client{Base: url, Token: tok}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := c.State(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "smithy chat:", err)
		return 1
	}
	ch := make(chan daemon.ClientMsg, 256)
	go c.Follow(ctx, 0, ch)
	p := colorprofile.TrueColor
	if os.Getenv("NO_COLOR") != "" {
		p = colorprofile.Ascii
	}
	if _, err := tea.NewProgram(newChatView(ch, url, newTheme(true, p, utf8Locale()))).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "smithy chat:", err)
		return 1
	}
	return 0
}

func (m chatView) Init() tea.Cmd { return tea.Batch(textinput.Blink, m.wait()) }

func (m chatView) wait() tea.Cmd {
	if m.stream == nil {
		return nil
	}
	return func() tea.Msg { return <-m.stream }
}

func (m *chatView) push(c chatMsg) {
	m.msgs = append(m.msgs, &c)
	if m.offset > 0 {
		n := len(m.wrap(&c))
		m.offset += n
		m.at.offset += n
	} else {
		*m.at = anchor{}
	}
}

func (m chatView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case daemon.ClientMsg:
		switch {
		case msg.Connected:
			m.conn, m.connErr = "connected", ""
		case msg.Err != nil:
			m.conn, m.connErr = "reconnecting", msg.Err.Error()
		case msg.Event != nil && msg.Event.Seq > m.last:
			m.last = msg.Event.Seq
			if t := eventText(msg.Event.Kind, msg.Event.Session, msg.Event.Data); t != "" {
				m.push(chatMsg{who: "daemon", at: msg.Event.Time.Local().Format("15:04"), text: t})
			}
		}
		return m, m.wait()
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(m.width-3, 1))
		m.scroll(0)
		return m, nil
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scroll(3)
		case tea.MouseWheelDown:
			m.scroll(-3)
		}
		return m, nil
	case tea.KeyPressMsg:
		page := max(m.rows()-1, 1)
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "pgup", "ctrl+u":
			m.scroll(page)
			return m, nil
		case "pgdown", "ctrl+d":
			m.scroll(-page)
			return m, nil
		case "home":
			m.scroll(1 << 30)
			return m, nil
		case "end":
			m.offset = 0
			return m, nil
		case "enter":
			if t := strings.TrimSpace(m.input.Value()); t != "" {
				m.offset = 0
				m.push(chatMsg{who: "you", text: t})
				m.input.SetValue("")
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func eventText(kind, session string, data json.RawMessage) string {
	switch kind {
	case daemon.KindAsked:
		var d daemon.Decision
		if json.Unmarshal(data, &d) != nil || d.ID == "" {
			return ""
		}
		return "? " + d.Question
	case daemon.KindAnswered:
		var o daemon.Outcome
		if json.Unmarshal(data, &o) != nil {
			return ""
		}
		return "answered: " + o.Answer + by(o.By)
	case "log":
		var d struct {
			Line string `json:"line"`
		}
		if json.Unmarshal(data, &d) == nil {
			return clean(d.Line)
		}
	}
	if session != "" {
		return kind + " " + session
	}
	return kind
}

// rows is the transcript height: the screen less the pinned input and the footer.
func (m chatView) rows() int { return max(m.height-2, 1) }

// scroll moves by d rows (up is positive), clamped so the top message stays reachable but not
// passed. It wraps only the messages between the bottom and the new offset.
func (m *chatView) scroll(d int) {
	want := max(m.offset+d, 0)
	need, seen := want+m.rows(), 0
	for i := len(m.msgs) - 1; i >= 0 && seen < need; i-- {
		seen += len(m.wrap(m.msgs[i]))
	}
	m.offset = min(want, max(seen-m.rows(), 0))
}

// wrap renders a message at the screen width, styled as in the Chat-Minimal mockup: your
// prompts on a raised row behind a dim ›, daemon text in base, a blank row after each.
func (m chatView) wrap(c *chatMsg) []string {
	t := m.th
	w := max(m.width, 20)
	if c.lines != nil && c.width == w {
		return c.lines
	}
	row, lead, body := lipgloss.NewStyle(), "  ", t.fg(t.text)
	if c.who == "you" {
		lead, body = t.fg(t.dim).Render(t.g.prompt)+" ", t.fg(t.strong)
		if t.raised != nil {
			row = row.Background(t.raised)
		}
	}
	text := c.text
	if c.at != "" {
		text = c.at + " " + text
	}
	c.width = w
	c.lines = nil
	for n, l := range strings.Split(ansi.Wrap(text, max(w-6, 10), ""), "\n") {
		if n > 0 {
			lead = "  "
		}
		c.lines = append(c.lines, row.Width(w).Render("  "+lead+body.Render(l)))
	}
	c.lines = append(c.lines, "")
	return c.lines
}

// seek moves the anchor to m.offset one message at a time from where it last was, so a frame
// costs the rows scrolled since the previous one, not the whole transcript.
func (m chatView) seek() (i, r int) {
	a := m.at
	if a.width != max(m.width, 20) || (a.offset == 0 && a.i != len(m.msgs)-1) {
		*a = anchor{i: len(m.msgs) - 1, width: max(m.width, 20)}
	}
	i, r = a.i, a.r+m.offset-a.offset
	for i >= 0 && r >= len(m.wrap(m.msgs[i])) {
		r -= len(m.wrap(m.msgs[i]))
		i--
	}
	for r < 0 && i < len(m.msgs)-1 {
		i++
		r += len(m.wrap(m.msgs[i]))
	}
	r = max(r, 0)
	*a = anchor{offset: m.offset, i: i, r: r, width: a.width}
	return i, r
}

// visible returns the transcript rows on screen, wrapping only the messages they come from.
func (m chatView) visible() []string {
	rows := m.rows()
	var out []string
	i, skip := m.seek()
	for ; i >= 0 && len(out) < rows; i-- {
		ls := m.wrap(m.msgs[i])
		for j := len(ls) - 1 - skip; j >= 0 && len(out) < rows; j-- {
			out = append(out, ls[j])
		}
		skip = 0
	}
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out
}

func (m chatView) View() tea.View {
	t := m.th
	w := max(m.width, 20)
	raised := lipgloss.NewStyle()
	if t.raised != nil {
		raised = raised.Background(t.raised)
	}
	vis := m.visible()
	out := make([]string, 0, m.height)
	for range m.rows() - len(vis) {
		out = append(out, "")
	}
	out = append(out, vis...)
	out = append(out, raised.Width(w).MaxWidth(w).Render("  "+m.input.View()))
	out = append(out, m.footer(w))
	v := tea.NewView(strings.Join(out, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if t.surface != nil {
		v.BackgroundColor = t.surface
	}
	return v
}

// footer is the key hints on the left and the daemon connection on the right.
func (m chatView) footer(w int) string {
	t := m.th
	dim, key := t.fg(t.dim), t.fg(t.text)
	hint := func(k, what string) string { return key.Render(k) + dim.Render(" "+what) }
	left := "  " + strings.Join([]string{hint("enter", "send"), hint("pgup/pgdn", "scroll"), hint("esc", "quit")}, "   ")
	if m.offset > 0 {
		left += "   " + t.fg(t.info).Render(fmt.Sprintf("→ %d rows up", m.offset))
	}
	right := t.fg(t.ok).Render(t.g.pass) + dim.Render(" connected")
	switch m.conn {
	case "connecting":
		right = t.fg(t.accent).Render(t.g.on) + dim.Render(" connecting")
	case "reconnecting":
		right = t.fg(t.err).Render(t.g.fail) + dim.Render(" reconnecting: "+short(m.connErr, 30))
	}
	right += dim.Render(" " + t.g.dot + " " + m.url + "  ")
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left, w, "")
	}
	return left + strings.Repeat(" ", gap) + right
}
