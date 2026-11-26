//go:build harness

package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/models"
	"github.com/Zovaris/smithy/internal/remote"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// ChatUsage is the text for `smithy chat --help`; `smithy` with no arguments opens it too.
const ChatUsage = `usage: smithy chat [url | --server <name|url> | --standalone]
A fullscreen chat over a smithy daemon: its events stream into the transcript and the input stays
pinned at the bottom. A lost connection retries and resumes after the last event seen.
Keys: enter send; pgup/pgdn, wheel or ctrl+u/ctrl+d scroll; home/end top/bottom; click a tool block
to open it; v (empty input) cycles minimal / expandable / verbose; tab agents; ctrl+k palette; an
option's first letter (empty input) or a click on it answers the decision pinned at the top;
/ opens every command, @ a file, ? help; esc clears the input; ctrl+c quits.
  url           default: the local daemon (token from ~/.smithy/daemon.token), started in the
                background when none runs; it outlives the chat and exits after daemon.idle_exit
                (default 30m) with no clients and no running work, or on smithy stop. A
                tailscale serve URL needs no token (daemon.allow decides)
  --server      a saved server (smithy servers), local or a URL; default: SMITHY_SERVER, then the
                default /connect or smithy servers use saved, then this machine
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
	input         textarea.Model
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
	// ask posts a question to /v1/asks (the /ask demo); nil without a daemon.
	ask func(id, question string, options []string) tea.Cmd

	setup     setupAPI      // the daemon's setup endpoints; nil: none
	setupSt   *daemon.Setup // last GET /v1/setup
	setupSel  int
	setupStep setupStep
	setupNote string // a fix, an error or install output shown in the open overlay
	modelFor  string
	modelIn   string       // the filter typed over the model list
	modelSel  int          // the selected row of modelRows
	modelList *models.List // what the agent reported; nil while it loads

	sess     sessionAPI            // the daemon's sessions endpoints; nil: offline
	current  string                // the open session, "" before the first message
	convs    map[string][]*chatMsg // the other sessions' conversations
	cost     map[string]float64    // spend per session
	noise    []noise               // daemon events kept out of the transcript
	dir      string                // the folder sessions start in and @ lists
	root     string                // dir with symlinks resolved, as the daemon records it
	allProj  bool                  // the agents panel lists every project, not only dir
	branch   string                // its git branch, for the footer
	webURL   string                // the web UI with its token; "" when unknown
	files    *fileCache            // the @ popup's file list
	popSel   int                   // the selected / or @ popup row
	agentSel int                   // the selected session in the agents panel
	info     infoPanel             // the open read-only panel
	frame    int                   // motion frame, advanced only while something runs
	ticking  bool                  // a frame tick is scheduled

	server      string                    // the saved server name, local or URL this chat talks to
	picker      *connectPicker            // /connect's state while its box is open
	connectLoad func() tea.Msg            // loads /connect's servers; nil: none
	connectUse  func(string) error        // saves the default server
	connectAdd  func(remote.Server) error // saves a discovered server
	switchTo    *chatSwitch               // set when /connect picked another server
}

// infoPanel is what a read-only box shows: /connect, /remote, /web, /log.
type infoPanel struct {
	title  string
	lines  []string
	scroll bool
}

func newChatView(stream <-chan daemon.ClientMsg, url string, th theme) chatView {
	return chatView{th: th, url: url, stream: stream, conn: "connecting", input: newComposer(th), width: 80, height: 24, at: &anchor{}, focus: -1, files: &fileCache{}}
}

// newComposer is the input pinned at the bottom: a textarea on the raised tier behind the
// ember ›, three rows tall, growing to ten then scrolling; enter sends, shift+enter, alt+enter
// or ctrl+j breaks the line.
func newComposer(th theme) textarea.Model {
	in := textarea.New()
	in.ShowLineNumbers = false
	in.Placeholder = "Ask, or / for commands"
	in.DynamicHeight, in.MinHeight, in.MaxHeight, in.MaxContentHeight = true, 3, 10, 1<<20
	in.KeyMap.InsertNewline.SetKeys("shift+enter", "alt+enter", "ctrl+j")
	in.SetPromptFunc(2, func(i textarea.PromptInfo) string {
		st := lipgloss.NewStyle()
		if th.raised != nil {
			st = st.Background(th.raised)
		}
		if i.LineNumber == 0 {
			return th.fg(th.accent).Inherit(st).Render(th.g.prompt) + st.Render(" ")
		}
		return st.Render("  ")
	})
	st := textarea.DefaultStyles(true)
	for _, s := range []*textarea.StyleState{&st.Focused, &st.Blurred} {
		base := lipgloss.NewStyle()
		if th.raised != nil {
			base = base.Background(th.raised)
		}
		s.Base, s.CursorLine, s.EndOfBuffer, s.Prompt = base, base, base, base
		s.Text = base.Foreground(th.strong)
		s.Placeholder = base.Foreground(th.dim)
		if th.plain {
			s.Text, s.Placeholder = base, base
		}
	}
	in.SetStyles(st)
	in.SetHeight(3)
	in.Focus()
	return in
}

// chatArgs is what `smithy chat` was given.
type chatArgs struct {
	url        string
	server     string
	standalone bool
	open       string // a session to open once connected (after /connect)
}

func parseChatArgs(args []string) (chatArgs, error) {
	var a chatArgs
	for i := 0; i < len(args); i++ {
		x := args[i]
		switch {
		case x == "--standalone":
			a.standalone = true
		case x == "--server":
			if i+1 >= len(args) || args[i+1] == "" {
				return a, errors.New("--server takes a name or url")
			}
			a.server, i = args[i+1], i+1
		case strings.HasPrefix(x, "--server="):
			if a.server = strings.TrimPrefix(x, "--server="); a.server == "" {
				return a, errors.New("--server takes a name or url")
			}
		case strings.HasPrefix(x, "-"):
			return a, fmt.Errorf("unknown flag %s", x)
		case a.url != "":
			return a, errors.New("one url at most")
		default:
			a.url = x
		}
	}
	if a.standalone && (a.url != "" || a.server != "") {
		return a, errors.New("--standalone runs its own daemon; it takes no url or server")
	}
	if a.url != "" && a.server != "" {
		return a, errors.New("a url or --server, not both")
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
	env := os.Getenv("SMITHY_SERVER")
	for {
		code, next := runChat(a, env)
		if next == nil {
			return code
		}
		a, env = chatArgs{server: next.server, open: next.open}, ""
	}
}

// runChat runs one chat program against the server a resolves to; a non-nil switch means
// /connect picked another server and Chat should run again there.
func runChat(a chatArgs, env string) (int, *chatSwitch) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := remote.Server{Name: remote.Local}
	if a.url == "" && !a.standalone {
		s, err := remote.Resolve(remote.File(), a.server, env)
		if err != nil {
			fmt.Fprintln(os.Stderr, "smithy chat:", err)
			return 1, nil
		}
		srv = s
		if srv.Name != remote.Local {
			a.url = srv.URL
		}
	}
	url, tok, where := a.url, srv.Token, whereRemote
	serverName := serverKey(srv)
	if a.standalone {
		serverName = "standalone"
	} else if srv.Name == remote.Local && a.url != "" {
		serverName = a.url
	}
	var notes []string
	switch {
	case a.standalone:
		if i := daemon.Running(); i != nil {
			fmt.Fprintf(os.Stderr, "smithy --standalone: a background daemon runs (pid %d); stop it first with smithy stop, or run smithy to use it\n", i.PID)
			return 1, nil
		}
		ready := make(chan daemon.Info, 1)
		ended := make(chan int, 1)
		go func() { ended <- daemon.Standalone(ctx, func(i daemon.Info) { ready <- i }) }()
		select {
		case i := <-ready:
			url = i.URL()
		case code := <-ended:
			fmt.Fprintln(os.Stderr, "smithy --standalone: the in-process daemon did not start")
			return max(code, 1), nil
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
			return 1, nil
		}
		if fresh {
			notes = append(notes, startedNote(i, idle))
		}
		url, where = i.URL(), whereBackground
	}
	if tok == "" && (where != whereRemote || loopbackURL(url)) {
		t, terr := daemon.LoadToken(daemon.TokenFile(), false)
		if terr != nil {
			fmt.Fprintln(os.Stderr, "smithy chat: token:", terr)
			return 1, nil
		}
		tok = t
	}
	c := &daemon.Client{Base: url, Token: tok}
	st, err := c.State(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "smithy chat: %s: %v\n", serverName, err)
		if serverName != remote.Local && a.server == "" && env == "" {
			fmt.Fprintln(os.Stderr, "smithy chat: the default server is unreachable; smithy servers use local goes back to this machine")
		}
		return 1, nil
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
	m.server = serverName
	m.connectLoad = defaultConnectLoad(func() (string, string) {
		if where == whereBackground || where == whereStandalone {
			return url, tok
		}
		i := daemon.Running()
		if i == nil {
			return "", ""
		}
		t, _ := daemon.LoadToken(daemon.TokenFile(), false)
		return i.URL(), t
	})
	m.connectUse = func(n string) error { return remote.Use(remote.File(), n) }
	m.connectAdd = func(s remote.Server) error { return remote.Add(remote.File(), s) }
	if a.open != "" {
		m.open(a.open)
	}
	m.hold(notes, st.LastSeq)
	m.setPending(st.Pending)
	m.setup, m.sess = c, c
	m.dir, _ = os.Getwd()
	m.root, _ = filepath.EvalSymlinks(m.dir)
	m.branch = gitBranch(m.dir)
	if tok != "" {
		m.webURL = url + "/#token=" + neturl.QueryEscape(tok)
	} else {
		m.webURL = url
	}
	if where == whereBackground {
		m.status("the daemon runs in the background and keeps your sessions going after you quit")
	}
	m.decide = func(id, answer string) tea.Cmd {
		return func() tea.Msg {
			_, err := c.Decide(ctx, id, answer)
			return chatDecidedMsg{id: id, err: err}
		}
	}
	m.ask = func(id, question string, options []string) tea.Cmd {
		return func() tea.Msg {
			if err := c.Ask(ctx, id, question, options); err != nil {
				return chatDecidedMsg{id: id, err: err}
			}
			return nil
		}
	}
	fm, err := tea.NewProgram(m).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy chat:", err)
		return 1, nil
	}
	if fm, ok := fm.(chatView); ok && fm.switchTo != nil {
		return 0, fm.switchTo
	}
	if where == whereBackground {
		fmt.Fprintln(os.Stderr, "smithy: the daemon keeps running in the background; smithy stop ends it")
	}
	return 0, nil
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

func (m chatView) Init() tea.Cmd { return tea.Batch(textarea.Blink, m.wait(), m.loadSetup(true)) }

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
	n, cmd := m.update(msg)
	m = n.(chatView)
	if _, ok := msg.(chatFrameMsg); !ok && !m.ticking && m.animating() {
		m.ticking = true
		cmd = tea.Batch(cmd, frameTick())
	}
	return m, cmd
}

func (m chatView) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, ok := m.setupUpdate(msg); ok {
		return next, cmd
	}
	switch msg := msg.(type) {
	case chatFrameMsg:
		m.frame++
		for i := max(len(m.msgs)-8, 0); i < len(m.msgs); i++ {
			if c := m.msgs[i]; c.tool != nil && c.tool.Status == "running" {
				c.lines = nil
			}
		}
		if !m.animating() {
			m.ticking = false
			return m, nil
		}
		return m, frameTick()
	case sessionStartedMsg:
		if msg.err != nil {
			m.system(m.th.g.fail + " could not start a session: " + msg.err.Error())
			return m, nil
		}
		m.meta(msg.s.ID, msg.s)
		m.open(msg.s.ID)
		m.status("session " + msg.s.ID + " started with " + msg.s.Agent)
		return m, nil
	case sessionErrMsg:
		m.system(m.th.g.fail + " " + msg.what + " failed: " + msg.err.Error())
		return m, nil
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
	case connectLoadedMsg:
		m.connectLoaded(msg)
		return m, nil
	case chatDecidedMsg:
		m.decided(msg)
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(m.width-2*m.pad()-4, 1))
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
		if items := m.popup(); len(items) > 0 {
			if n, cmd, ok := m.popupKey(msg, items); ok {
				return n, cmd
			}
		}
		if done, cmd := m.shortcut(msg); done {
			return m, cmd
		}
		page := max(m.rows()-1, 1)
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.input.Value() != "" {
				m.input.SetValue("")
				return m, nil
			}
			if m.focus >= 0 {
				m.setFocus(-1)
				return m, nil
			}
			return m, nil
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
			m.popSel = 0
			if strings.HasPrefix(t, "/") {
				return m.command(t)
			}
			if t != "" {
				return m.send(t)
			}
			return m, nil
		}
		if !m.input.Focused() && msg.Text != "" {
			m.setFocus(-1)
		}
		m.popSel = 0
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

// rows is the transcript height: the screen less the header, the decision strip, the status
// line, the composer, the footer and the spinner row while the open session works.
func (m chatView) rows() int {
	n := m.height - headRows - m.stripRows() - 1 - m.input.Height() - 1
	if m.working() {
		n -= 2
	}
	return max(n, 1)
}

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

// textRows renders a text message as in the Chat mockups: your prompts on the raised tier
// behind a dim ›, the agent's reply in strong, its notes in base, file paths as links, a
// blank row after each.
func (m chatView) textRows(c *chatMsg, w int) []string {
	t := m.th
	p := m.pad()
	gut := strings.Repeat(" ", p)
	bw := w - 2*p
	if c.who == "you" {
		row := lipgloss.NewStyle()
		if t.raised != nil {
			row = row.Background(t.raised)
		}
		var lines []string
		for n, l := range strings.Split(ansi.Wrap(c.text, max(bw-6, 10), ""), "\n") {
			lead := "    "
			if n == 0 {
				lead = "  " + t.fg(t.dim).Render(t.g.prompt) + " "
			}
			lines = append(lines, gut+fill(row, bw, lead+t.fg(t.strong).Render(l))+gut)
		}
		return append(lines, "")
	}
	body := t.fg(t.text)
	text := c.text
	switch {
	case c.who == "agent":
		body = t.fg(t.strong)
	case c.kind == "thinking":
		body = t.fg(t.dim).Italic(true)
		text = "thinking " + t.g.dot + " " + text
	case c.kind != "":
		body = t.fg(t.dim)
	}
	if c.at != "" {
		text = c.at + " " + text
	}
	var lines []string
	for _, l := range strings.Split(ansi.Wrap(text, max(bw-4, 10), ""), "\n") {
		lines = append(lines, gut+"  "+m.linkify(l, body))
	}
	return append(lines, "")
}

// linkify draws l in st: file paths in the link color, `code` spans on the raised tier
// without their backticks.
func (m chatView) linkify(l string, st lipgloss.Style) string {
	t := m.th
	if t.plain {
		return l
	}
	code := st
	if t.raised != nil {
		code = code.Background(t.raised)
	}
	var b strings.Builder
	for n, part := range strings.Split(l, "`") {
		if n%2 == 1 {
			b.WriteString(code.Render(part))
			continue
		}
		words := strings.Split(part, " ")
		for i, wd := range words {
			if i > 0 {
				b.WriteString(st.Render(" "))
			}
			core := strings.TrimRight(wd, ".,:;)")
			if pathLike.MatchString(core) {
				b.WriteString(t.fg(t.info).Render(core) + st.Render(wd[len(core):]))
				continue
			}
			b.WriteString(st.Render(wd))
		}
	}
	return b.String()
}

var pathLike = regexp.MustCompile(`^[\w.~-]*(/[\w.-]+)+\.[A-Za-z]\w{0,5}$|^[\w-]+\.(go|ts|tsx|js|md|json|py|rs|toml|yaml|yml|sh)$`)

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
	out := make([]string, 0, m.height)
	out = append(out, m.header(w)...)
	if m.stripRows() > 0 {
		s, _ := m.strip(w)
		out = append(out, s, "")
	}
	vis, _ := m.visible()
	body := make([]string, 0, m.rows()+2)
	for range m.rows() - len(vis) {
		body = append(body, "")
	}
	body = append(body, vis...)
	if m.working() {
		body = append(body, m.spinRow(w), "")
	}
	if items := m.popup(); len(items) > 0 {
		pw := min(w-2*m.pad(), 72)
		pr := m.popupRows(items, pw)
		for i, r := range pr {
			if j := len(body) - len(pr) + i; j >= 0 {
				body[j] = splice(body[j], r, m.pad(), w)
			}
		}
	}
	out = append(out, body...)
	out = append(out, m.statusLine(w))
	out = append(out, m.composer(w)...)
	out = append(out, m.footer(w))
	switch m.overlay {
	case overlayAgents:
		out = m.agentsBox(out, w, m.height)
	case overlaySetup, overlayPicker:
		out = m.pickerBox(out, w, m.height)
	case overlaySkills:
		out = m.skillsBox(out, w, m.height)
	case overlayPalette:
		out = m.paletteBox(out, w, m.height)
	case overlayHelp:
		out = m.helpBox(out, w, m.height)
	case overlayInfo:
		out = m.infoBox(out, w, m.height)
	}
	if len(out) > m.height {
		out = out[len(out)-m.height:]
	}
	v := tea.NewView(strings.Join(out, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if t.surface != nil {
		v.BackgroundColor = t.surface
	}
	return v
}

// composer is the input block: the raised tier from gutter to gutter, the ember › inside it.
func (m chatView) composer(w int) []string {
	t := m.th
	raised := lipgloss.NewStyle()
	if t.raised != nil {
		raised = raised.Background(t.raised)
	}
	p := strings.Repeat(" ", m.pad())
	bw := w - 2*m.pad()
	var out []string
	for _, l := range strings.Split(m.input.View(), "\n") {
		out = append(out, p+fill(raised, bw, "  "+l)+p)
	}
	return out
}

// gitBranch is dir's current branch, "" outside a repo.
func gitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
