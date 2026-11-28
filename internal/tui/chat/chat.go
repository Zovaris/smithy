//go:build harness

// Package chat is the fullscreen chat over a smithy daemon, with its first-run walk, modals and attach.
package chat

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/models"
	"github.com/Zovaris/smithy/internal/remote"
	"github.com/Zovaris/smithy/internal/tui/kit"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// ChatUsage is the text for `smithy chat --help`; `smithy` with no arguments opens it too.
const ChatUsage = `usage: smithy chat [url | --server <name|url> | --standalone] [--session <id>]
A fullscreen chat over a smithy daemon: its events stream into the transcript and the input stays
pinned at the bottom. A lost connection retries and resumes after the last event seen.
Keys: enter send; pgup/pgdn, wheel or ctrl+u/ctrl+d scroll; home/end top/bottom; click a tool block
to open it; ctrl+o cycles minimal / expandable / verbose; tab agents; ctrl+k palette; a
question waiting on you opens as a card (a number, arrows and enter, or your own words; esc for later);
/ opens every command, /name or $name runs an installed skill, @ a file, ? then enter help; !cmd runs a shell
command in the session folder; ctrl+v attaches a clipboard image; esc clears the input; ctrl+c quits.
  url           default: the local daemon (token from ~/.smithy/daemon.token), started in the
                background when none runs; it outlives the chat and exits after daemon.idle_exit
                (default 30m) with no clients and no running work, or on smithy stop. A
                tailscale serve URL needs no token (daemon.allow decides)
  --server      a saved server (smithy servers), local or a URL; default: SMITHY_SERVER, then the
                default /connect or smithy servers use saved, then this machine
  --standalone  run a private daemon inside this process instead; it stops when the chat quits
  --session     open this session once connected`

// WelcomeUsage is `smithy welcome`: the chat opened on its first-run walk.
const WelcomeUsage = `usage: smithy welcome [url | --server <name|url>]
Opens the chat on the first-run walk: a short animated welcome, then the agents found on this
machine, the model new sessions start with, the stored API keys and the Telegram notifications.
esc leaves it at any step; smithy welcome walks it again.`

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
	sh                  *shellBlock
	open                bool
	ans                 string
	width, ver          int
	lines               []string
}

// anchor maps an offset to message i with r of its rows hidden below the screen; width is the
// wrap it was measured at.
type anchor struct{ offset, i, r, width int }

type chatView struct {
	th            kit.Theme
	resume        []string // the command that reopens this chat (OSC 7577); the open session is appended
	resumeDir     string
	quitting      bool
	url           string
	stream        <-chan api.ClientMsg
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
	pending  []api.Decision
	where    chatWhere
	held     []string // notes shown once the history replay reaches heldTo
	heldTo   int64
	sessions []chatSession
	// decide posts an answer to /v1/decisions and returns a chatDecidedMsg.
	decide func(id, answer string) tea.Cmd

	setup     setupAPI            // the daemon's setup endpoints; nil: none
	cfg       settingsAPI         // the daemon's settings endpoints; nil: none
	board     boardAPI            // the daemon's task endpoints; nil: none
	boardSt   boardView           // the open /board
	flows     workflowAPI         // the daemon's workflow endpoints; nil: none
	flowSt    workflowsView       // the open /workflows
	opener    func(string) error  // opens a URL; nil: the default browser
	queues    map[string][]string // messages typed during a session's turn, sent in order when it ends
	qEdit     bool                // the input holds a queued message taken back with up
	stopAsked bool                // ctrl+c interrupted this turn; a second one quits
	selecting bool                // ctrl+s: the mouse goes to the terminal so text can be selected
	infoBack  chatOverlay         // where esc on the info panel goes back to
	setupSt   *api.Setup          // last GET /v1/setup
	setupSel  int
	setupStep setupStep
	setupNote string // a fix, an error or install output shown in the open overlay
	modelFor  string
	modelIn   string       // the filter typed over the model list
	modelSel  int          // the selected row of modelRows
	modelList *models.List // what the agent reported; nil while it loads
	modelSess string
	effortSel int
	card      askCard
	themeSel  int

	welcome      welcomeStep
	welcomeAgent string
	welcomeNote  string // the walk's last saved pick, printed once when it ends
	welcomeSet   *api.Settings

	sess      sessionAPI            // the daemon's sessions endpoints; nil: offline
	current   string                // the open session, "" before the first message
	convs     map[string][]*chatMsg // the other sessions' conversations
	cost      map[string]float64    // spend per session
	toks      map[string]int64      // input and output tokens per session
	ctx       map[string]int64
	sideOff   bool
	sent      map[string]string
	streamed  map[string]int   // characters of agent text since the turn began, for tok/s
	effort    string           // agent.effort
	clock     func() time.Time // the header clock; nil: time.Now
	splashOn  bool             // play the entry animation at the first window size
	shimmer   int              // motion frames left of the top-effort shimmer on the composer
	still     bool             // reduced motion: the welcome field and anvil hold still
	splash    []string         // the entry animation's frames left; nil once it ends
	noise     []noise          // daemon events kept out of the transcript
	dir       string           // the folder sessions start in and @ lists
	root      string           // dir with symlinks resolved, as the daemon records it
	allProj   bool             // the agents panel lists every project, not only dir
	proj      projectPick      // the project modal a new session opens with
	branch    string           // its git branch, for the footer
	webURL    string           // the web UI with its token; "" when unknown
	files     *fileCache       // the @ popup's file list
	popModels *models.List     // the /model popup's list, for popModels.Agent
	palSet    []api.SettingRow // the daemon settings the palette lists
	popSel    int              // the selected / or @ popup row
	agentSel  int              // the selected session in the agents panel
	info      infoPanel        // the open read-only panel
	tendMode  string           // the daemon's tend mode for the header; "" until read
	tendPRs   int              // the PRs it tends
	frame     int              // motion frame, advanced only while something runs
	ticking   bool             // a frame tick is scheduled
	vb        valueBump        // the setting value that just changed, settling
	usage     *events.Report   // /usage's report, nil while it loads
	cfgAdv    bool             // /config shows its Advanced settings
	evAt      time.Time        // when the event being applied happened
	dirty     int              // files changed in the folder's repo, -1 outside one
	homeIn    valueBump        // the empty screen's entrance
	tabsOn    bool             // ui.agent_tabs: the sessions as tabs under the header

	server      string                    // the saved server name, local or URL this chat talks to
	picker      *connectPicker            // /connect's state while its box is open
	connectLoad func() tea.Msg            // loads /connect's servers; nil: none
	connectUse  func(string) error        // saves the default server
	connectAdd  func(remote.Server) error // saves a discovered server
	switchTo    *chatSwitch               // set when /connect picked another server

	shell    func(ctx context.Context, dir, line string) daemon.ShellRun // runs !cmd; nil: daemon.RunShell
	clip     func() ([]byte, error)                                      // reads a clipboard image; nil: readClipImage
	pasteDir string                                                      // where pasted images are saved; "": temp dir
	images   []string                                                    // pasted images waiting for the next message
}

// infoPanel is what a read-only box shows: /connect, /remote, /web, /log.
type infoPanel struct {
	title  string
	lines  []string
	scroll bool
	set    *panelSetting // a setting row on top, cycled with left/right; nil: none
	urls   []string      // per line: the link it opens, copies or removes; /links only
}

func newChatView(stream <-chan api.ClientMsg, url string, th kit.Theme) chatView {
	return chatView{th: th, url: url, stream: stream, conn: "connecting", input: newComposer(th), width: 80, height: 24, at: &anchor{}, focus: -1, files: &fileCache{}}
}

// newComposer is the input pinned at the bottom: a textarea on the raised tier behind the
// ember ›, three rows tall, growing to ten then scrolling; enter sends, shift+enter, alt+enter
// or ctrl+j breaks the line.
// composerPrompt draws the accent › on the first row; a theme change sets it again.
func composerPrompt(kit.Theme) func(textarea.PromptInfo) string {
	return func(textarea.PromptInfo) string { return "" }
}

func newComposer(th kit.Theme) textarea.Model {
	in := textarea.New()
	in.ShowLineNumbers = false
	in.Placeholder = "Ask, or / for commands"
	in.DynamicHeight, in.MinHeight, in.MaxHeight, in.MaxContentHeight = true, 1, 10, 1<<20
	in.KeyMap.InsertNewline.SetKeys("shift+enter", "alt+enter", "ctrl+j")
	in.SetPromptFunc(0, composerPrompt(th))
	in.SetStyles(composerStyles(th))
	in.SetHeight(1)
	in.Focus()
	return in
}

// composerStyles puts the textarea on the focus tier in theme th; a theme change sets them again.
func composerStyles(th kit.Theme) textarea.Styles {
	st := textarea.DefaultStyles(true)
	for _, s := range []*textarea.StyleState{&st.Focused, &st.Blurred} {
		base := lipgloss.NewStyle()
		if th.Focus != nil {
			base = base.Background(th.Focus)
		}
		s.Base, s.CursorLine, s.EndOfBuffer, s.Prompt = base, base, base, base
		s.Text = base.Foreground(th.Strong)
		s.Placeholder = base.Foreground(th.Dim)
		if th.Plain {
			s.Text, s.Placeholder = base, base
		}
	}
	return st
}

// chatArgs is what `smithy chat` was given.
type chatArgs struct {
	url        string
	server     string
	standalone bool
	welcome    bool
	open       string // a session to open once connected (after /connect)
}

func parseChatArgs(args []string) (chatArgs, error) {
	var a chatArgs
	for i := 0; i < len(args); i++ {
		x := args[i]
		switch {
		case x == "--welcome":
			a.welcome = true
		case x == "--standalone":
			a.standalone = true
		case x == "--server":
			if i+1 >= len(args) || args[i+1] == "" {
				return a, errors.New("--server takes a name or url")
			}
			a.server, i = args[i+1], i+1
		case x == "--session":
			if i+1 >= len(args) || args[i+1] == "" {
				return a, errors.New("--session takes a session id")
			}
			a.open, i = args[i+1], i+1
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
func startedNote(i *api.Info, idle time.Duration) string {
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
		ready := make(chan api.Info, 1)
		ended := make(chan int, 1)
		go func() { ended <- daemon.Standalone(ctx, func(i api.Info) { ready <- i }) }()
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
		i, fresh, err := daemon.Ensure(daemon.Running, func() (*api.Info, error) {
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
	c := &api.Client{Base: url, Token: tok}
	st, err := c.State(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "smithy chat: %s: %v\n", serverName, err)
		if serverName != remote.Local && a.server == "" && env == "" {
			fmt.Fprintln(os.Stderr, "smithy chat: the default server is unreachable; smithy servers use local goes back to this machine")
		}
		return 1, nil
	}
	if n := versionNote(st.Version, kit.Version); n != "" && where != whereStandalone {
		notes = append(notes, n)
	}
	ch := make(chan api.ClientMsg, 256)
	go c.Follow(ctx, 0, ch)
	p := colorprofile.TrueColor
	if os.Getenv("NO_COLOR") != "" {
		p = colorprofile.Ascii
	}
	m := newChatView(ch, url, kit.NewTheme(true, p, kit.Utf8Locale()))
	m.splashOn = splashWanted(isTTY(os.Stdout), os.Getenv("NO_COLOR"), os.Getenv("SMITHY_REDUCE_MOTION"), config.Get("ui.splash", "true"), config.Get("ui.reduce_motion", "false"))
	m.still = os.Getenv("SMITHY_REDUCE_MOTION") != "" || config.Get("ui.reduce_motion", "false") == "true"
	m.tabsOn = config.Get("ui.agent_tabs", "false") == "true"
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
	m.resume, m.resumeDir = resumeArgs(a, where), cwd()
	if a.open != "" {
		m.open(a.open)
	}
	if a.welcome {
		m.welcome = welcomeSplash
	}
	m.hold(notes, st.LastSeq)
	m.setPending(st.Pending)
	m.offerAsk()
	m.setup, m.sess, m.cfg, m.board, m.flows = c, c, c, c, c
	m.dir, _ = os.Getwd()
	m.root, _ = filepath.EvalSymlinks(m.dir)
	m.branch = gitBranch(m.dir)
	m.dirty = gitDirty(m.dir)
	m.homeIn = valueBump{key: "home", still: m.still}.start()
	m.effort = config.Get("agent.effort", "medium")
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

func (m chatView) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.wait(), m.loadSetup(true), m.loadTheme(), clockTick(), m.loadTend())
}

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
	if k, ok := msg.(tea.KeyPressMsg); ok && (k.String() == "ctrl+k" || k.String() == "ctrl+l") {
		cmd = tea.Batch(cmd, tea.ClearScreen)
	}
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
	case clockMsg:
		return m, tea.Batch(clockTick(), m.loadTend())
	case tendStateMsg:
		m.tendMode, m.tendPRs = msg.l.Mode, len(msg.l.PRs)
		return m, nil
	case chatFrameMsg:
		m.frame++
		m.vb, m.homeIn = m.vb.step(), m.homeIn.step()
		m.shimmer = max(m.shimmer-1, 0)
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
			m.system(m.th.G.Fail + " could not start a session: " + msg.err.Error())
			return m, nil
		}
		m.meta(msg.s.ID, msg.s)
		m.open(msg.s.ID)
		m.status("session " + msg.s.ID + " started with " + msg.s.Agent)
		return m, nil
	case projectsMsg:
		return m.projectsLoaded(msg)
	case dirsMsg:
		return m.dirsLoaded(msg)
	case boardMsg:
		return m.boardLoaded(msg)
	case boardMovedMsg:
		return m.boardMoved(msg)
	case workflowsMsg:
		return m.workflowsLoaded(msg)
	case workflowDoneMsg:
		return m.workflowDone(msg)
	case workflowDetailMsg:
		return m.workflowDetail(msg)
	case effortDefaultMsg:
		m.effortDefaultSet(msg)
		return m, nil
	case paletteSettingsMsg:
		m.palSet = msg.rows
		return m, nil
	case usageMsg:
		m.usage = &msg.r
		return m, nil
	case settingsDoneMsg:
		if msg.tend != nil {
			m.tendMode, m.tendPRs = msg.tend.Mode, len(msg.tend.PRs)
		}
		if msg.panel != nil {
			m.info, m.overlay, m.pal = *msg.panel, overlayInfo, palette{}
			return m, nil
		}
		for _, l := range msg.lines {
			m.system(l)
		}
		return m, nil
	case sessionErrMsg:
		m.system(m.th.G.Fail + " " + msg.what + " failed: " + msg.err.Error())
		return m, nil
	case api.ClientMsg:
		switch {
		case msg.Connected:
			m.conn, m.connErr = "connected", ""
		case msg.Err != nil:
			m.conn, m.connErr = "reconnecting", msg.Err.Error()
		case msg.Event != nil && msg.Event.Seq > m.last:
			m.last = msg.Event.Seq
			m.event(msg.Event.Kind, msg.Event.Session, msg.Event.Time, msg.Event.Data)
			if m.last >= m.heldTo {
				m.release()
			}
		}
		return m, tea.Batch(m.wait(), m.drain())
	case connectLoadedMsg:
		m.connectLoaded(msg)
		return m, nil
	case chatDecidedMsg:
		m.decided(msg)
		return m, nil
	case shellDoneMsg:
		m.shellDone(msg)
		return m, nil
	case clipMsg:
		m.clipped(msg)
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(m.width-2*m.pad()-4, 1))
		m.scroll(0)
		return m, m.startSplash()
	case splashMsg:
		if len(m.splash) <= 1 {
			m.splash = nil
			return m, nil
		}
		m.splash = m.splash[1:]
		return m, splashTick()
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
		if m.splash != nil {
			m.splash = nil
			return m, nil
		}
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
			if m.input.Value() == "" && m.busy(m.current) && !m.stopAsked {
				m.stopAsked = true
				m.status("interrupting the turn; ctrl+c again quits")
				return m.stopSession()
			}
			return m.quit()
		case "up":
			if m.input.Value() == "" && len(m.queues[m.current]) > 0 {
				m.takeQueued()
				return m, nil
			}
		case "ctrl+v":
			return m, m.pasteImage()
		case "esc":
			if len(m.images) > 0 && m.input.Value() == "" {
				m.images = nil
				return m, nil
			}
			if m.input.Value() != "" {
				m.input.SetValue("")
				return m, nil
			}
			if m.focus >= 0 {
				m.setFocus(-1)
				return m, nil
			}
			if m.busy(m.current) {
				m.status("interrupting the turn")
				return m.stopSession()
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
			if t == "" && len(m.images) == 0 && len(m.pending) > 0 {
				m.openAsk()
				return m, nil
			}
			if o, ok := m.refineOption(t); ok && len(m.images) == 0 {
				return m.refineAct(o)
			}
			if t == "?" && len(m.images) == 0 {
				m.overlay, m.pal = overlayHelp, palette{}
				return m, nil
			}
			if strings.HasPrefix(t, "/") {
				return m.command(t)
			}
			if strings.HasPrefix(t, "!") {
				return m.bang(strings.TrimSpace(t[1:]))
			}
			if name, arg, _ := strings.Cut(t, " "); strings.HasPrefix(name, "$") {
				if p := m.skillPrompt(name[1:], strings.TrimSpace(arg)); p != "" {
					return m.send(p)
				}
			}
			if t != "" || len(m.images) > 0 {
				t, m.images = withImages(t, m.images), nil
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
	return m, tea.Batch(cmd, m.loadPopModels())
}

func eventText(kind, session string, data json.RawMessage) string {
	switch kind {
	case daemon.KindAsked:
		var d api.Decision
		if json.Unmarshal(data, &d) != nil || d.ID == "" {
			return ""
		}
		return "? " + d.Question
	case daemon.KindAnswered:
		var o api.Outcome
		if json.Unmarshal(data, &o) != nil {
			return ""
		}
		return "answered: " + o.Answer + by(o.By)
	case daemon.KindRouted:
		return "" // bookkeeping: the answered line already said it
	case "log":
		var d struct {
			Line string `json:"line"`
		}
		if json.Unmarshal(data, &d) == nil {
			return kit.Clean(d.Line)
		}
	case daemon.KindWorkflowRun:
		var r struct{ Workflow, Status, Error string }
		if json.Unmarshal(data, &r) != nil || r.Workflow == "" {
			return ""
		}
		return strings.TrimSpace("workflow " + r.Workflow + " " + r.Status + " " + r.Error)
	case "event":
		var e struct{ Kind, Batch, Item, Detail string }
		if json.Unmarshal(data, &e) != nil || e.Kind == "" {
			return ""
		}
		what := strings.TrimSpace(strings.ReplaceAll(e.Kind, "_", " ") + " " + e.Detail)
		if who := cmp.Or(e.Item, e.Batch); who != "" {
			return who + " · " + what
		}
		return what
	}
	kind = strings.ReplaceAll(kind, "_", " ")
	if session != "" {
		return kind + " " + session
	}
	return kind
}

// rows is the transcript height: the screen less the header, the status
// line and the gap under it, the composer, the footer and the spinner row while the open
// session works.
func (m chatView) rows() int {
	n := m.height - headRows - 1 - m.input.Height() - 5
	if strings.TrimSpace(ansi.Strip(m.statusLine(m.tw()))) != "" {
		n--
	}
	if m.working() {
		n -= 2
	}
	if m.refined() != "" {
		n -= 2
	}
	n -= len(m.queueRows(max(m.width, 20)))
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
	w := m.tw()
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
	bw := m.textWidth(w)
	if c.who == "you" {
		var lines []string
		res := m.turnResult(c)
		if c.at != "" {
			res = strings.TrimSpace(res + "  " + t.Fg(t.Dim).Render(c.at))
		}
		below := bw-ansi.StringWidth(res)-2 < max(bw/2, 30)
		room := bw - ansi.StringWidth(res) - 2
		if below {
			room = bw
		}
		for n, l := range strings.Split(ansi.Wrap(c.text, room, ""), "\n") {
			right := ""
			if n == 0 && !below {
				right = res
			}
			lines = append(lines, m.spreadLane(w, "", m.railMark(true), t.Accent, t.Fg(t.Strong).Bold(true).Render(l), right, nil))
		}
		if below && res != "" {
			lines = append(lines, m.lane(w, "", m.railMark(true), t.Accent, ansi.Truncate(res, bw, "…"), nil))
		}
		return append(lines, m.gap(w))
	}
	body := t.Fg(t.Text)
	text := c.text
	switch {
	case c.who == "agent":
		body = t.Fg(t.Strong)
		text = hideGoalMark(text)
	case c.kind == "thinking":
		body = t.Fg(t.Dim).Italic(true)
		text = "thinking " + t.G.Dot + " " + text
	case c.kind != "":
		body = t.Fg(t.Dim)
	}
	if c.at != "" && c.who != "agent" {
		text = c.at + " " + text
	}
	var lines []string
	for _, l := range strings.Split(ansi.Wrap(text, bw, ""), "\n") {
		lines = append(lines, m.lane(w, "", m.railMark(false), t.Border, m.linkify(l, body), nil))
	}
	return append(lines, m.gap(w))
}

// linkify draws l in st: file paths in the link color, `code` spans on the raised tier
// without their backticks.
func (m chatView) linkify(l string, st lipgloss.Style) string {
	t := m.th
	if t.Plain {
		return l
	}
	code := st
	if t.Raised != nil {
		code = code.Background(t.Raised)
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
				b.WriteString(t.Fg(t.Info).Render(core) + st.Render(wd[len(core):]))
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
	if a.width != m.tw() || (a.offset == 0 && a.i != len(m.msgs)-1) {
		*a = anchor{i: len(m.msgs) - 1, width: m.tw()}
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
	if m.splash != nil && m.overlay != overlaySetup && m.welcome == welcomeOff {
		v := tea.NewView(m.splash[0])
		v.AltScreen = true
		return v
	}
	w := max(m.width, 20)
	out := make([]string, 0, m.height)
	out = append(out, m.header(w)...)
	vis, _ := m.visible()
	body := make([]string, 0, m.rows()+2)
	if m.fresh() && !m.working() && m.offset == 0 {
		body = append(body, m.home(m.tw(), m.rows())...)
	} else if intro := m.intro(m.tw()); m.lead(len(vis)) == len(intro) {
		body = append(body, intro...)
		body = append(body, vis...)
		for len(body) < m.rows() {
			body = append(body, "")
		}
	} else {
		for range m.rows() - len(vis) {
			body = append(body, "")
		}
		body = append(body, vis...)
	}
	if m.working() {
		body = append(body, m.spinRow(m.tw()), "")
	}
	if m.refined() != "" {
		r, _ := m.refineRow(m.tw())
		body = append(body, r, "")
	}
	if items := m.popup(); len(items) > 0 {
		pw := min(w-2*m.pad(), 72)
		pr := m.popupRows(items, pw)
		n := min(len(pr), len(body))
		for i, r := range pr[len(pr)-n:] {
			j := len(body) - n + i
			body[j] = splice(body[j], r, m.pad(), m.tw())
		}
	}
	if st := m.statusLine(m.tw()); strings.TrimSpace(ansi.Strip(st)) != "" {
		body = append(body, st, "")
	} else {
		body = append(body, "")
	}
	body = append(body, m.composer(m.tw())...)
	band := len(body) - len(m.composerQueue(m.tw()))
	body = append(body, "")
	out = append(out, m.besideSide(body, band)...)
	out = append(out, m.footer(w))
	if m.welcome != welcomeOff {
		out = m.welcomeView(w, m.height)
	}
	switch m.overlay {
	case overlayAgents:
		out = m.agentsBox(out, w, m.height)
	case overlaySetup, overlayPicker:
		out = m.pickerBox(out, w, m.height)
	case overlaySkills:
		out = m.skillsBox(out, w, m.height)
	case overlayProject:
		out = m.projectBox(out, w, m.height)
	case overlayPalette:
		out = m.paletteBox(out, w, m.height)
	case overlayHelp:
		out = m.helpBox(out, w, m.height)
	case overlayInfo:
		out = m.infoBox(out, w, m.height)
	case overlayAsk:
		out = m.askBox(out, w, m.height)
	case overlayConfig:
		out = m.configBox(out, w, m.height)
	case overlayUsage:
		out = m.usageBox(out, w, m.height)
	case overlayBoard:
		out = m.boardBox(out, w, m.height)
	case overlayWorkflows:
		out = m.workflowsBox(out, w, m.height)
	}
	if len(out) > m.height {
		out = out[len(out)-m.height:]
	}
	v := tea.NewView(strings.Join(out, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if m.selecting {
		v.MouseMode = tea.MouseModeNone
	}
	if t.Surface != nil {
		v.BackgroundColor = t.Surface
	}
	if !m.quitting && len(m.resume) > 0 {
		args := slices.Clone(m.resume)
		if m.current != "" && m.where != whereStandalone {
			args = append(args, "--session", m.current)
		}
		v.ProgramResume = &tea.ProgramResume{Args: args, Dir: m.resumeDir}
	}
	return v
}

// quit ends the program and withdraws the resume command, so a terminal restart does not reopen it.
func (m chatView) quit() (tea.Model, tea.Cmd) {
	m.quitting = true
	return m, tea.Quit
}

// resumeArgs is the command that reopens this chat against the same server; nil when the
// executable cannot be found.
func resumeArgs(a chatArgs, where chatWhere) []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	args := []string{exe, "chat"}
	switch {
	case where == whereStandalone:
		args = append(args, "--standalone")
	case a.url != "":
		args = append(args, a.url)
	case a.server != "":
		args = append(args, "--server", a.server)
	}
	return args
}

func cwd() string {
	d, _ := os.Getwd()
	return d
}

// composer is the input block: the focus tier from gutter to gutter behind an accent rail, the
// accent › inside it, and a row of chips under the text: agent, model and effort.
func (m chatView) composer(w int) []string {
	t := m.th
	_, tx, right := m.grid(w)
	bw := right - tx
	row := func(mark, s string) string {
		return m.lane(w, mark, m.railMark(true), t.Accent, ansi.Truncate(s, bw, ""), t.Focus)
	}
	var out []string
	if c := m.chips(); c != "" {
		out = append(out, row("", c+"  esc drops"))
	}
	out = append(out, row("", ""))
	for n, l := range strings.Split(m.input.View(), "\n") {
		if m.shimmer > 0 {
			l = t.Shimmer(ansi.Strip(l), float64(bw)*float64(shimmerFrames-m.shimmer)/shimmerFrames)
		}
		_ = n
		out = append(out, row("", l))
	}
	out = append(out, row("", ""), row("", m.metaChips()))
	return append(out, m.composerQueue(w)...)
}

// composerQueue is the queued messages listed under the input band.
func (m chatView) composerQueue(w int) []string {
	_, tx, right := m.grid(w)
	var out []string
	p := strings.Repeat(" ", tx-1)
	for _, r := range m.queueRows(right - tx + 1) {
		out = append(out, p+r)
	}
	return out
}

func (m chatView) metaChips() string {
	t := m.th
	parts := strings.Split(m.agentModel(), " "+t.G.Dot+" ")
	var out []string
	for i, s := range parts {
		if s == "" {
			continue
		}
		fg := t.Text
		if i == 0 {
			fg = t.Strong
		}
		out = append(out, m.chip(" "+s+" ", fg, t.Overlay, false))
	}
	if s, ok := m.session(); ok && s.effort != "" {
		out = append(out, m.chip(" effort ", t.Muted, t.Overlay, false)+m.chip(s.effort+" ", t.Accent, t.Overlay, true))
	}
	return strings.Join(out, on(t.Focus).Render(" "))
}

// gitBranch is dir's current branch, "" outside a repo.
func gitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// daemonCallTimeout bounds one daemon call made from a command, so a hung daemon cannot leave
// the TUI waiting forever.
const daemonCallTimeout = 2 * time.Minute

// daemonCtx is the context for one daemon call made from a command.
func daemonCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), daemonCallTimeout)
}

// gitDirty is how many files git reports changed in dir, -1 outside a repo.
func gitDirty(dir string) int {
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		return -1
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		return len(strings.Split(s, "\n"))
	}
	return 0
}
