package tui

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/harmonica"
)

// Usage is the text for `smithy tui --help`.
const Usage = `usage: smithy tui [--no-anim]
A live dashboard of the autonomous forge: Incoming issues, Agents at work, what Needs you,
Activity, Worktrees and Plans. It refreshes every 2 s from local files only (no network).
Keys: 1-6 or tab/shift+tab panels, up/down select, enter details, o open, l log, c copy,
p pause/resume intake (auto.paused), / filter, r refresh, ? help, q quit. Mouse works too.
--no-anim (or SMITHY_REDUCE_MOTION=1) draws still frames; NO_COLOR=1 drops color.
Exit 0 = quit, 1 = the terminal could not run it, 2 = bad usage.`

// Version is the binary's version, shown in the header.
var Version = "dev"

const refreshEvery = 2 * time.Second

type panel int

const (
	pIncoming panel = iota
	pAgents
	pNeeds
	pActivity
	pWorktrees
	pPlans
	nPanels
)

var titles = [nPanels]string{"Incoming", "Agents at work", "Needs you", "Activity", "Worktrees", "Plans"}
var tabs = [nPanels]string{"Incoming", "Agents", "Needs you", "Activity", "Worktrees", "Plans"}

// meaning is what each panel holds, for empty states and the help overlay.
var meaning = [nPanels]string{
	"Issues from Linear or GitHub waiting to start (daily cap, hours).",
	"Each warden run and its workers: issue, branch, stage, CI, PR.",
	"Decisions, PRs ready for your review or merge, blocked work.",
	"A live feed: polls, queued issues, launches, PRs, CI, merges.",
	"Every smithy worktree on disk; orphans (no live item) in yellow.",
	"Task lists and their progress.",
}

type keyMap struct {
	Next, Prev, Up, Down, Jump, Enter, Close, Open, Log, Copy, Pause, Filter, Refresh, Help, Quit key.Binding
}

var keys = keyMap{
	Next:    key.NewBinding(key.WithKeys("tab", "right"), key.WithHelp("tab", "next panel")),
	Prev:    key.NewBinding(key.WithKeys("shift+tab", "left"), key.WithHelp("⇧tab", "previous panel")),
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Jump:    key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6"), key.WithHelp("1-6", "jump to panel")),
	Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
	Close:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	Open:    key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open url")),
	Log:     key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "log")),
	Copy:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy")),
	Pause:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause intake")),
	Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

// context is the footer's keys for the focused panel.
func context(p panel) []key.Binding {
	switch p {
	case pIncoming:
		return []key.Binding{keys.Enter, keys.Open, keys.Copy, keys.Pause, keys.Filter}
	case pAgents:
		return []key.Binding{keys.Enter, keys.Log, keys.Open, keys.Copy, keys.Filter}
	case pNeeds:
		return []key.Binding{keys.Enter, keys.Open, keys.Copy, keys.Filter}
	case pActivity:
		return []key.Binding{keys.Enter, keys.Open, keys.Filter}
	case pWorktrees:
		return []key.Binding{keys.Enter, keys.Copy, keys.Filter}
	}
	return []key.Binding{keys.Enter, keys.Copy, keys.Filter}
}

type tickMsg time.Time
type frameMsg struct{}
type strikeMsg struct{}
type snapMsg Snapshot
type openedMsg struct{ err error }
type pausedMsg struct {
	paused bool
	err    error
}

// Model is the dashboard's Bubble Tea model.
type Model struct {
	snap      Snapshot
	focus     panel
	sel       [nPanels]int
	modal     bool
	helpOpen  bool
	logOpen   bool
	logView   viewport.Model
	logTitle  string
	filtering bool
	input     textinput.Model
	filter    [nPanels]string
	loading   bool // a load is running; ticks skip theirs until it lands
	width     int
	height    int
	dark      bool
	profile   colorprofile.Profile
	unicode   bool
	motion    bool
	framing   bool // a frame loop is scheduled
	ticks     int
	status    string
	toast     string
	toastTill time.Time
	clickAt   time.Time
	clickPos  [2]int
	th        theme
	fx        *fx
	capPos    float64
	capVel    float64
	spring    harmonica.Spring
	now       func() time.Time
	load      func(time.Time) Snapshot
	opener    func(string) error
	pauser    func(bool) error
}

// options sets up a model; tests pass fixed ones, New reads the environment.
type options struct {
	profile colorprofile.Profile
	dark    bool
	unicode bool
	motion  bool
	seed    uint64
	toast   bool
	now     func() time.Time
	load    func(time.Time) Snapshot
}

func newModel(o options) Model {
	m := Model{width: 120, height: 40, dark: o.dark, profile: o.profile, unicode: o.unicode, motion: o.motion,
		now: o.now, load: o.load, opener: openURL, pauser: setPaused, fx: newFX(o.seed),
		spring: harmonica.NewSpring(harmonica.FPS(fps), 4.0, 0.95), input: textinput.New()}
	m.input.Prompt = "/ "
	m.input.Placeholder = "filter this panel"
	m.th = newTheme(m.dark, m.profile, m.unicode)
	m.snap = m.load(m.now())
	m.capPos = capPercent(m.snap.Watcher)
	if o.toast {
		m.toast = "tab or 1-6 to move · enter for details · ? for help"
		m.toastTill = m.now().Add(4 * time.Second)
	}
	return m
}

// New builds the model from the environment: NO_COLOR, SMITHY_REDUCE_MOTION, the locale, and
// ~/.smithy/tui.seen for the first-run hint.
func New(noAnim bool) Model {
	p := colorprofile.TrueColor
	if os.Getenv("NO_COLOR") != "" {
		p = colorprofile.Ascii
	}
	seen := filepath.Join(ghx.Home(), "tui.seen")
	_, err := os.Stat(seen)
	first := os.IsNotExist(err)
	if first {
		_ = os.MkdirAll(filepath.Dir(seen), 0o700)
		_ = os.WriteFile(seen, []byte(time.Now().Format(time.RFC3339)+"\n"), 0o600)
	}
	motion := !noAnim && os.Getenv("SMITHY_REDUCE_MOTION") == "" && p != colorprofile.Ascii
	return newModel(options{profile: p, dark: true, unicode: utf8Locale(), motion: motion, toast: first,
		seed: uint64(time.Now().UnixNano()), now: time.Now, load: Load})
}

func setPaused(on bool) error {
	return config.Set(config.UserFile(), "auto.paused", fmt.Sprint(on))
}

func capPercent(w Watcher) float64 {
	if w.Cap <= 0 {
		return 0
	}
	return min(float64(w.Today)/float64(w.Cap), 1)
}

// Init starts the refresh loop, asks for the background color and strikes the anvil once.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tick(), tea.RequestBackgroundColor}
	if m.motion {
		cmds = append(cmds, func() tea.Msg { return strikeMsg{} })
	}
	return tea.Batch(cmds...)
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func frame() tea.Cmd {
	return tea.Tick(time.Second/fps, func(time.Time) tea.Msg { return frameMsg{} })
}

func (m Model) reload() tea.Cmd {
	load, now := m.load, m.now
	return func() tea.Msg { return snapMsg(load(now())) }
}

func (m Model) sprite() sprite {
	if m.width >= 110 {
		return bigAnvil
	}
	return smallAnvil
}

func (m *Model) strike(n int) tea.Cmd {
	if !m.motion {
		return nil
	}
	m.fx.strike(m.sprite(), n, len([]rune(wordB[0])))
	return m.animate()
}

// animate starts the frame loop if motion is on and none is running.
func (m *Model) animate() tea.Cmd {
	if !m.motion || m.framing {
		return nil
	}
	m.framing = true
	return frame()
}

func (m Model) busy() bool {
	return m.fx.busy() || abs(m.capPos-capPercent(m.snap.Watcher)) > 0.002 || abs(m.capVel) > 0.002
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// Update handles keys, the mouse, resizes, ticks, frames and fresh snapshots.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.sizeLog()
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.th = newTheme(m.dark, m.profile, m.unicode)
	case tea.ColorProfileMsg:
		if os.Getenv("NO_COLOR") == "" {
			m.profile = msg.Profile
			m.th = newTheme(m.dark, m.profile, m.unicode)
		}
	case tickMsg:
		m.ticks++
		if m.toast != "" && !m.now().Before(m.toastTill) {
			m.toast = ""
		}
		cmds := []tea.Cmd{tick()}
		if !m.loading {
			m.loading = true
			cmds = append(cmds, m.reload())
		}
		if m.ticks%idleTicks == 0 {
			cmds = append(cmds, m.strike(5))
		}
		return m, tea.Batch(cmds...)
	case strikeMsg:
		return m, m.strike(10)
	case frameMsg:
		m.framing = false
		m.fx.step()
		m.capPos, m.capVel = m.spring.Update(m.capPos, m.capVel, capPercent(m.snap.Watcher))
		if m.busy() {
			return m, m.animate()
		}
		m.capPos, m.capVel = capPercent(m.snap.Watcher), 0
	case snapMsg:
		m.loading = false
		if msg.At.Before(m.snap.At) {
			return m, nil
		}
		prev := m.snap
		m.snap = Snapshot(msg)
		m.clamp()
		if !m.motion {
			m.capPos = capPercent(m.snap.Watcher)
			return m, nil
		}
		if events(prev, m.snap) {
			return m, m.strike(12)
		}
		return m, m.animate()
	case openedMsg:
		m.status = ""
		if msg.err != nil {
			m.status = "open: " + msg.err.Error()
		}
	case pausedMsg:
		if msg.err != nil {
			m.status = "pause: " + msg.err.Error()
			return m, nil
		}
		m.status = "intake resumed"
		if msg.paused {
			m.status = "intake paused: smithy auto take launches nothing until p again"
		}
		m.loading = true
		return m, m.reload()
	case tea.MouseClickMsg:
		return m.click(msg.Mouse())
	case tea.MouseWheelMsg:
		mm := msg.Mouse()
		if m.logOpen {
			var cmd tea.Cmd
			m.logView, cmd = m.logView.Update(msg)
			return m, cmd
		}
		if p, ok := m.panelAt(mm.X, mm.Y); ok {
			m.focus = p
			if mm.Button == tea.MouseWheelUp {
				m.move(-1)
			} else if mm.Button == tea.MouseWheelDown {
				m.move(1)
			}
		}
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// events reports a real change worth a strike: something queued or launched, a PR ready, a merge.
func events(prev, cur Snapshot) bool {
	count := func(s Snapshot, stages ...string) int {
		n := 0
		for _, b := range s.Batches {
			for _, it := range b.Items {
				for _, st := range stages {
					if it.Stage == st {
						n++
					}
				}
			}
		}
		return n
	}
	return cur.Watcher.Today > prev.Watcher.Today || len(cur.Queue) > len(prev.Queue) ||
		count(cur, "merged", "reported") > count(prev, "merged", "reported") || count(cur, "verify") > count(prev, "verify")
}

func (m *Model) move(d int) {
	n := len(m.visible(m.focus))
	m.sel[m.focus] = max(min(m.sel[m.focus]+d, n-1), 0)
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.filtering = false
			m.filter[m.focus] = ""
			m.input.SetValue("")
			m.input.Blur()
			return m, nil
		case "enter":
			m.filtering = false
			m.input.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.filter[m.focus] = m.input.Value()
		m.sel[m.focus] = 0
		return m, cmd
	}
	if m.logOpen {
		switch {
		case key.Matches(msg, keys.Quit) && msg.String() == "ctrl+c":
			return m, tea.Quit
		case key.Matches(msg, keys.Close, keys.Log, keys.Quit):
			m.logOpen = false
			return m, nil
		}
		var cmd tea.Cmd
		m.logView, cmd = m.logView.Update(msg)
		return m, cmd
	}
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Help):
		m.helpOpen = !m.helpOpen
		m.modal = false
	case key.Matches(msg, keys.Close):
		m.modal, m.helpOpen, m.toast = false, false, ""
		if m.filter[m.focus] != "" {
			m.filter[m.focus] = ""
			m.input.SetValue("")
		}
	case m.helpOpen:
	case m.modal && key.Matches(msg, keys.Enter):
		m.modal = false
	case key.Matches(msg, keys.Open):
		return m.open()
	case key.Matches(msg, keys.Copy):
		return m.copy()
	case key.Matches(msg, keys.Log):
		return m.openLog()
	case m.modal:
	case key.Matches(msg, keys.Jump):
		m.focus = panel(msg.String()[0] - '1')
	case key.Matches(msg, keys.Next):
		m.focus = (m.focus + 1) % nPanels
	case key.Matches(msg, keys.Prev):
		m.focus = (m.focus + nPanels - 1) % nPanels
	case key.Matches(msg, keys.Up):
		m.move(-1)
	case key.Matches(msg, keys.Down):
		m.move(1)
	case key.Matches(msg, keys.Enter):
		_, ok := m.selected()
		m.modal = ok
	case key.Matches(msg, keys.Filter):
		m.filtering = true
		m.input.SetValue(m.filter[m.focus])
		return m, m.input.Focus()
	case key.Matches(msg, keys.Pause):
		on := !m.snap.Watcher.Paused
		pauser := m.pauser
		return m, func() tea.Msg { return pausedMsg{on, pauser(on)} }
	case key.Matches(msg, keys.Refresh):
		if m.loading {
			return m, nil
		}
		m.status, m.loading = "refreshed", true
		return m, m.reload()
	}
	return m, nil
}

// click focuses the panel under the pointer and selects the row; a second click on it opens it.
func (m Model) click(mm tea.Mouse) (tea.Model, tea.Cmd) {
	if m.modal || m.helpOpen || m.logOpen {
		m.modal, m.helpOpen, m.logOpen = false, false, false
		return m, nil
	}
	if p, ok := m.tabAt(mm.X, mm.Y); ok {
		m.focus = p
		return m, nil
	}
	p, ok := m.panelAt(mm.X, mm.Y)
	if !ok {
		return m, nil
	}
	m.focus = p
	if i, ok := m.rowAt(p, mm.Y); ok {
		double := m.sel[p] == i && m.clickPos == [2]int{mm.X, mm.Y} && m.now().Sub(m.clickAt) < 500*time.Millisecond
		m.sel[p] = i
		m.clickAt, m.clickPos = m.now(), [2]int{mm.X, mm.Y}
		if double {
			m.modal = true
		}
	}
	return m, nil
}

func (m Model) open() (tea.Model, tea.Cmd) {
	e, ok := m.selected()
	if !ok || e.url == "" || e.url == "-" {
		m.status = "no URL for this row"
		return m, nil
	}
	if !webURL(e.url) {
		m.status = "not a web URL"
		return m, nil
	}
	m.status = "opening " + e.url
	opener, u := m.opener, e.url
	return m, func() tea.Msg { return openedMsg{opener(u)} }
}

func (m Model) copy() (tea.Model, tea.Cmd) {
	e, ok := m.selected()
	if !ok || e.copy == "" {
		m.status = "nothing to copy on this row"
		return m, nil
	}
	m.status = "copied " + e.copy
	return m, tea.SetClipboard(e.copy)
}

func (m Model) openLog() (tea.Model, tea.Cmd) {
	e, ok := m.selected()
	if !ok || e.log == "" {
		m.status = "no log for this row"
		return m, nil
	}
	lines := tail(e.log, 2000)
	if lines == nil {
		m.status = "no log yet at " + e.log
		return m, nil
	}
	m.logOpen, m.modal = true, false
	m.logTitle = e.log
	m.logView = viewport.New()
	m.sizeLog()
	m.logView.SetContentLines(lines)
	m.logView.GotoBottom()
	return m, nil
}

func (m *Model) sizeLog() {
	m.logView.SetWidth(max(min(m.width-8, 140), 20))
	m.logView.SetHeight(max(m.height-12, 5))
}

func (m *Model) clamp() {
	for p := panel(0); p < nPanels; p++ {
		if n := len(m.visible(p)); m.sel[p] >= n {
			m.sel[p] = max(n-1, 0)
		}
	}
}

func (m Model) selected() (entry, bool) {
	es := m.visible(m.focus)
	if i := m.sel[m.focus]; i < len(es) {
		return es[i], true
	}
	return entry{}, false
}

// webURL accepts only http(s) URLs with a host, so `o` never hands the opener a file or script URL.
func webURL(u string) bool {
	p, err := url.Parse(u)
	return err == nil && (p.Scheme == "http" || p.Scheme == "https") && p.Host != ""
}

func openURL(u string) error {
	name := "xdg-open"
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	}
	return exec.Command(name, u).Start()
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", max(int(d.Seconds()), 0))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func shortPR(pr string) string {
	if i := strings.LastIndex(pr, "/pull/"); i >= 0 {
		return "#" + pr[i+len("/pull/"):]
	}
	return pr
}

// Main runs `smithy tui`.
func Main(args []string) int {
	noAnim := false
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Println(Usage)
			return 0
		case "--no-anim":
			noAnim = true
		default:
			fmt.Fprintf(os.Stderr, "smithy tui: unexpected argument %s\n%s\n", a, Usage)
			return 2
		}
	}
	if _, err := tea.NewProgram(New(noAnim)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "smithy tui:", err)
		return 1
	}
	return 0
}
