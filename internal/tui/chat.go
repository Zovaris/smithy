//go:build harness

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// ChatUsage is the text for `smithy chat --help`; `smithy` with no arguments opens it too.
const ChatUsage = `usage: smithy chat [url | --standalone]
A fullscreen chat over a smithy daemon: its events stream into the transcript and the input stays
pinned at the bottom. A lost connection retries and resumes after the last event seen.
Keys: enter send; pgup/pgdn, wheel or ctrl+u/ctrl+d scroll; home/end top/bottom; click a tool block
to open it; v (empty input) cycles minimal / expandable / verbose; tab agents; ctrl+k palette; an
option's first letter (empty input) or a click on it answers the decision pinned at the top;
/mode /agents /palette /answer /help /quit; esc or ctrl+c quit.
  url           default: the local daemon (token from ~/.smithy/daemon.token), started in the
                background when none runs; it outlives the chat and exits after daemon.idle_exit
                (default 30m) with no clients and no running work, or on smithy stop. A
                tailscale serve URL needs no token (daemon.allow decides)
  --standalone  run a private daemon inside this process instead; it stops when the chat quits`

// chatWhere is where the daemon the chat talks to runs.
type chatWhere int

const (
	whereRemote     chatWhere = iota // a url given: another machine or a daemon someone else runs
	whereBackground                  // the local background daemon bare smithy starts or reuses
	whereStandalone                  // inside this process (--standalone)
)

// chatMsg is one transcript entry; lines caches its render at width and ver, filled only when
// drawn. kind is "" for text, "thinking" and "turn" (shown only in verbose) or "system".
type chatMsg struct {
	who, at, text, kind string
	tool                *chatTool
	open                bool
	width, ver          int
	lines               []string
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

	mode     chatMode
	ver      int // bumped when every cached render goes stale (mode change)
	focus    int // focused message, -1 for none
	overlay  chatOverlay
	pal      palette
	pending  []daemon.Decision
	where    chatWhere
	held     []string // notes shown once the history replay reaches heldTo
	heldTo   int64
	sessions []chatSession
	// decide posts an answer to /v1/decisions and returns a chatDecidedMsg.
	decide func(id, answer string) tea.Cmd
}

func newChatView(stream <-chan daemon.ClientMsg, url string, th theme) chatView {
	in := textinput.New()
	in.Prompt = th.fg(th.accent).Render(th.g.prompt) + " "
	in.Placeholder = "Ask, or / for commands"
	in.Focus()
	return chatView{th: th, url: url, stream: stream, conn: "connecting", input: in, width: 80, height: 24, at: &anchor{}, focus: -1}
}

// chatArgs is what `smithy chat` was given.
type chatArgs struct {
	url        string
	standalone bool
}

func parseChatArgs(args []string) (chatArgs, error) {
	var a chatArgs
	for _, x := range args {
		switch {
		case x == "--standalone":
			a.standalone = true
		case strings.HasPrefix(x, "-"):
			return a, fmt.Errorf("unknown flag %s", x)
		case a.url != "":
			return a, errors.New("one url at most")
		default:
			a.url = x
		}
	}
	if a.standalone && a.url != "" {
		return a, errors.New("--standalone runs its own daemon; it takes no url")
	}
	return a, nil
}

// versionNote warns when the daemon runs another version than this binary; "" when they match.
func versionNote(daemonV, mine string) string {
	if daemonV == mine {
		return ""
	}
	if daemonV == "" {
		daemonV = "an older version"
	}
	return fmt.Sprintf("the daemon runs %s, this smithy is %s; to switch: smithy stop, then smithy", daemonV, mine)
}

// startedNote is the first transcript line after bare smithy started the daemon itself.
func startedNote(i *daemon.Info, idle time.Duration) string {
	when := "until smithy stop"
	if idle > 0 {
		when = "until " + shortDur(idle) + " with no clients and no running work, or smithy stop"
	}
	return fmt.Sprintf("started the smithy daemon in the background (pid %d, %s); it keeps running after you quit, %s", i.PID, i.Addr, when)
}

// shortDur is d without trailing zero units: 30m, 1h, 1h30m, 90s.
func shortDur(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// Chat runs `smithy chat` (and bare `smithy` in the harness build). With no url it attaches to
// the local daemon, starting it in the background first when none runs; --standalone runs a
// private daemon inside this process that stops with it.
func Chat(args []string) int {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println(ChatUsage)
			return 0
		}
	}
	a, err := parseChatArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy chat:", err)
		return 2
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	url, tok, where := a.url, "", whereRemote
	var notes []string
	switch {
	case a.standalone:
		if i := daemon.Running(); i != nil {
			fmt.Fprintf(os.Stderr, "smithy --standalone: a background daemon runs (pid %d); stop it first with smithy stop, or run smithy to use it\n", i.PID)
			return 1
		}
		ready := make(chan daemon.Info, 1)
		ended := make(chan int, 1)
		go func() { ended <- daemon.Standalone(ctx, func(i daemon.Info) { ready <- i }) }()
		select {
		case i := <-ready:
			url = i.URL()
		case code := <-ended:
			fmt.Fprintln(os.Stderr, "smithy --standalone: the in-process daemon did not start")
			return max(code, 1)
		}
		defer func() { cancel(); <-ended }()
		where = whereStandalone
	case url == "":
		var idle time.Duration
		i, fresh, err := daemon.Ensure(daemon.Running, func() (*daemon.Info, error) {
			port, err := daemon.ConfigPort()
			if err != nil {
				return nil, err
			}
			if idle, err = daemon.ConfigIdle(); err != nil {
				return nil, err
			}
			fmt.Fprintln(os.Stderr, "smithy: starting the daemon in the background…")
			return daemon.StartBackground(port, idle, nil)
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "smithy: could not start the daemon:", err)
			return 1
		}
		if fresh {
			notes = append(notes, startedNote(i, idle))
		}
		url, where = i.URL(), whereBackground
	}
	if where != whereRemote || loopbackURL(url) {
		t, terr := daemon.LoadToken(daemon.TokenFile(), false)
		if terr != nil {
			fmt.Fprintln(os.Stderr, "smithy chat: token:", terr)
			return 1
		}
		tok = t
	}
	c := &daemon.Client{Base: url, Token: tok}
	st, err := c.State(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy chat:", err)
		return 1
	}
	if n := versionNote(st.Version, Version); n != "" && where != whereStandalone {
		notes = append(notes, n)
	}
	ch := make(chan daemon.ClientMsg, 256)
	go c.Follow(ctx, 0, ch)
	p := colorprofile.TrueColor
	if os.Getenv("NO_COLOR") != "" {
		p = colorprofile.Ascii
	}
	m := newChatView(ch, url, newTheme(true, p, utf8Locale()))
	m.where = where
	m.hold(notes, st.LastSeq)
	m.setPending(st.Pending)
	m.decide = func(id, answer string) tea.Cmd {
		return func() tea.Msg {
			_, err := c.Decide(ctx, id, answer)
			return chatDecidedMsg{id: id, err: err}
		}
	}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "smithy chat:", err)
		return 1
	}
	if where == whereBackground {
		fmt.Fprintln(os.Stderr, "smithy: the daemon keeps running in the background; smithy stop ends it")
	}
	return 0
}

// hold keeps notes until the replayed history reaches seq, so they land below it, not above.
func (m *chatView) hold(notes []string, seq int64) {
	m.held, m.heldTo = append(m.held, notes...), seq
	if m.last >= seq {
		m.release()
	}
}

func (m *chatView) release() {
	for _, n := range m.held {
		m.system(n)
	}
	m.held = nil
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
	stale := false
	for i := len(m.msgs) - 2; c.tool != nil && i >= 0 && m.msgs[i].tool != nil; i-- {
		m.msgs[i].lines, stale = nil, true
	}
	if m.offset > 0 {
		n := len(m.wrap(len(m.msgs) - 1))
		m.offset += n
		m.at.offset += n
	}
	if m.offset == 0 || stale {
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
			m.event(msg.Event.Kind, msg.Event.Session, msg.Event.Time.Local().Format("15:04"), msg.Event.Data)
			if m.last >= m.heldTo {
				m.release()
			}
		}
		return m, m.wait()
	case chatDecidedMsg:
		m.decided(msg)
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(m.width-3, 1))
		m.scroll(0)
		return m, nil
	case tea.MouseClickMsg:
		return m.click(msg)
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scroll(3)
		case tea.MouseWheelDown:
			m.scroll(-3)
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.overlay != overlayNone {
			return m.overlayKey(msg)
		}
		if done, cmd := m.shortcut(msg); done {
			return m, cmd
		}
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
			t := strings.TrimSpace(m.input.Value())
			if !m.input.Focused() && m.focus >= 0 {
				return m.toggle(m.focus), nil
			}
			m.input.SetValue("")
			if strings.HasPrefix(t, "/") {
				return m.command(t)
			}
			if t != "" {
				m.offset = 0
				m.push(chatMsg{who: "you", text: t})
			}
			return m, nil
		}
		if !m.input.Focused() && msg.Text != "" {
			m.setFocus(-1)
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

// rows is the transcript height: the screen less the decision strip, the pinned input and the
// footer.
func (m chatView) rows() int { return max(m.height-2-m.stripRows(), 1) }

// scroll moves by d rows (up is positive), clamped so the top message stays reachable but not
// passed. It wraps only the messages between the bottom and the new offset.
func (m *chatView) scroll(d int) {
	want := max(m.offset+d, 0)
	need, seen := want+m.rows(), 0
	for i := len(m.msgs) - 1; i >= 0 && seen < need; i-- {
		seen += len(m.wrap(i))
	}
	m.offset = min(want, max(seen-m.rows(), 0))
}

// wrap is message i's rows at the screen width in the current mode, cached until the width, the
// mode or the message changes.
func (m chatView) wrap(i int) []string {
	c := m.msgs[i]
	w := max(m.width, 20)
	if c.lines != nil && c.width == w && c.ver == m.ver {
		return c.lines
	}
	c.width, c.ver = w, m.ver
	c.lines = m.render(i, w)
	return c.lines
}

// textRows renders a text message as in the Chat-Minimal mockup: your prompts on a raised row
// behind a dim ›, daemon text in base, a blank row after each.
func (m chatView) textRows(c *chatMsg, w int) []string {
	t := m.th
	row, lead, body := lipgloss.NewStyle(), "  ", t.fg(t.text)
	switch {
	case c.who == "you":
		lead, body = t.fg(t.dim).Render(t.g.prompt)+" ", t.fg(t.strong)
		if t.raised != nil {
			row = row.Background(t.raised)
		}
	case c.kind != "":
		body = t.fg(t.dim)
	}
	text := c.text
	if c.kind == "thinking" {
		text = "thinking " + t.g.dot + " " + text
	}
	if c.at != "" {
		text = c.at + " " + text
	}
	lines := []string{}
	for n, l := range strings.Split(ansi.Wrap(text, max(w-6, 10), ""), "\n") {
		if n > 0 {
			lead = "  "
		}
		lines = append(lines, row.Width(w).Render("  "+lead+body.Render(l)))
	}
	return append(lines, "")
}

// seek moves the anchor to m.offset one message at a time from where it last was, so a frame
// costs the rows scrolled since the previous one, not the whole transcript.
func (m chatView) seek() (i, r int) {
	a := m.at
	if a.width != max(m.width, 20) || (a.offset == 0 && a.i != len(m.msgs)-1) {
		*a = anchor{i: len(m.msgs) - 1, width: max(m.width, 20)}
	}
	i, r = a.i, a.r+m.offset-a.offset
	for i >= 0 && r >= len(m.wrap(i)) {
		r -= len(m.wrap(i))
		i--
	}
	for r < 0 && i < len(m.msgs)-1 {
		i++
		r += len(m.wrap(i))
	}
	r = max(r, 0)
	*a = anchor{offset: m.offset, i: i, r: r, width: a.width}
	return i, r
}

// visible returns the transcript rows on screen and the message each comes from, wrapping only
// those messages.
func (m chatView) visible() (out []string, idx []int) {
	rows := m.rows()
	i, skip := m.seek()
	for ; i >= 0 && len(out) < rows; i-- {
		ls := m.wrap(i)
		for j := len(ls) - 1 - skip; j >= 0 && len(out) < rows; j-- {
			out, idx = append(out, ls[j]), append(idx, i)
		}
		skip = 0
	}
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
		idx[l], idx[r] = idx[r], idx[l]
	}
	return out, idx
}

func (m chatView) View() tea.View {
	t := m.th
	w := max(m.width, 20)
	raised := lipgloss.NewStyle()
	if t.raised != nil {
		raised = raised.Background(t.raised)
	}
	out := make([]string, 0, m.height)
	if m.stripRows() > 0 {
		s, _ := m.strip(w)
		out = append(out, s)
	}
	var vis []string
	switch m.overlay {
	case overlayAgents:
		vis = m.agentsPanel(w)
	case overlayPalette:
		vis = m.palettePanel(w)
	default:
		vis, _ = m.visible()
	}
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
	left := "  " + strings.Join([]string{hint("enter", "send"), hint("v", "mode: "+m.mode.String()), hint("tab", "agents"), hint("ctrl+k", "palette")}, "   ")
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
	where := ""
	switch m.where {
	case whereBackground:
		where = dim.Render(" "+t.g.dot+" background, ") + key.Render("smithy stop") + dim.Render(" ends it")
	case whereStandalone:
		where = dim.Render(" " + t.g.dot + " standalone")
	}
	// Narrow widths drop the url first, then the daemon line; the key hints always stay.
	for _, r := range []string{right + dim.Render(" "+t.g.dot+" "+m.url) + where, right + where, right} {
		r += "  "
		if gap := w - lipgloss.Width(left) - lipgloss.Width(r); gap >= 1 {
			return left + strings.Repeat(" ", gap) + r
		}
	}
	return ansi.Truncate(left, w, "")
}
