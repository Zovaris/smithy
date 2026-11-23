package tui

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/harmonica"
)

// Usage is the text for `smithy tui --help`.
const Usage = `usage: smithy tui [--no-anim]
A live, read-only dashboard: the auto watcher, its queue, warden batches, task lists and
pending decisions. It refreshes every 2 s from local files only (no network, no tokens).
Keys: tab/shift+tab or left/right switch panel, up/down select, enter details, esc close,
o open the URL, r refresh, ? all keys, q quit.
--no-anim (or SMITHY_REDUCE_MOTION=1) draws still frames; NO_COLOR=1 drops color.
Exit 0 = quit, 1 = the terminal could not run it, 2 = bad usage.`

// Version is the binary's version, shown in the header.
var Version = "dev"

const refreshEvery = 2 * time.Second

type panel int

const (
	pWatcher panel = iota
	pQueue
	pBatches
	pTasks
	pDecisions
	nPanels
)

var titles = [nPanels]string{"Watcher", "Queue", "Batches", "Tasks", "Decisions"}

type keyMap struct {
	Next, Prev, Up, Down, Enter, Close, Open, Refresh, Help, Quit key.Binding
}

var keys = keyMap{
	Next:    key.NewBinding(key.WithKeys("tab", "right"), key.WithHelp("tab", "next")),
	Prev:    key.NewBinding(key.WithKeys("shift+tab", "left"), key.WithHelp("⇧tab", "prev")),
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑↓", "select")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
	Close:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	Open:    key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "keys")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Next, k.Up, k.Enter, k.Open, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Next, k.Prev, k.Up, k.Down}, {k.Enter, k.Close, k.Open}, {k.Refresh, k.Help, k.Quit}}
}

type tickMsg time.Time
type frameMsg struct{}
type strikeMsg struct{}
type snapMsg Snapshot
type openedMsg struct{ err error }

// Model is the dashboard's Bubble Tea model.
type Model struct {
	snap     Snapshot
	focus    panel
	sel      [nPanels]int
	modal    bool
	loading  bool // a load is running; ticks skip theirs until it lands
	showHelp bool
	width    int
	height   int
	dark     bool
	profile  colorprofile.Profile
	unicode  bool
	motion   bool
	framing  bool // a frame loop is scheduled
	ticks    int
	status   string
	th       theme
	fx       *fx
	capPos   float64
	capVel   float64
	spring   harmonica.Spring
	queueLog []int
	now      func() time.Time
	load     func(time.Time) Snapshot
	opener   func(string) error
}

// options sets up a model; tests pass fixed ones, New reads the environment.
type options struct {
	profile colorprofile.Profile
	dark    bool
	unicode bool
	motion  bool
	now     func() time.Time
	load    func(time.Time) Snapshot
}

func newModel(o options) Model {
	m := Model{width: 120, height: 40, dark: o.dark, profile: o.profile, unicode: o.unicode, motion: o.motion,
		now: o.now, load: o.load, opener: openURL, fx: newFX(),
		spring: harmonica.NewSpring(harmonica.FPS(fps), 4.0, 0.95)}
	m.th = newTheme(m.dark, m.profile, m.unicode)
	m.snap = m.load(m.now())
	m.queueLog = []int{len(m.snap.Queue)}
	m.capPos = capPercent(m.snap.Watcher)
	return m
}

// New builds the model from the environment: NO_COLOR, SMITHY_REDUCE_MOTION, the locale.
func New(noAnim bool) Model {
	p := colorprofile.TrueColor
	if os.Getenv("NO_COLOR") != "" {
		p = colorprofile.Ascii
	}
	motion := !noAnim && os.Getenv("SMITHY_REDUCE_MOTION") == ""
	return newModel(options{profile: p, dark: true, unicode: utf8Locale(), motion: motion,
		now: time.Now, load: Load})
}

func capPercent(w Watcher) float64 {
	if w.Cap <= 0 {
		return 0
	}
	return min(float64(w.Today)/float64(w.Cap), 1)
}

// Init starts the refresh loop, asks for the background color and sweeps the shimmer once.
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

// Update handles keys, resizes, ticks, frames and fresh snapshots.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
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
		var cmds []tea.Cmd
		if !m.loading {
			m.loading = true
			cmds = append(cmds, m.reload())
		}
		if m.motion && m.ticks%restTicks == 0 {
			m.fx.sweep(m.wordWidth())
			cmds = append(cmds, m.animate())
		}
		if len(cmds) == 0 {
			return m, tick()
		}
		return m, tea.Batch(append(cmds, tick())...)
	case strikeMsg:
		m.fx.sweep(m.wordWidth())
		return m, m.animate()
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
		m.queueLog = append(m.queueLog, len(m.snap.Queue))
		if len(m.queueLog) > 60 {
			m.queueLog = m.queueLog[len(m.queueLog)-60:]
		}
		if !m.motion {
			m.capPos = capPercent(m.snap.Watcher)
			return m, nil
		}
		if events(prev, m.snap) {
			m.fx.sweep(m.wordWidth())
		}
		return m, m.animate()
	case openedMsg:
		m.status = ""
		if msg.err != nil {
			m.status = "open: " + msg.err.Error()
		}
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// events reports a real change worth a shimmer: a launch, or an item merged.
func events(prev, cur Snapshot) bool {
	merged := func(s Snapshot) int {
		n := 0
		for _, b := range s.Batches {
			for _, it := range b.Items {
				if it.Stage == "merged" || it.Stage == "reported" {
					n++
				}
			}
		}
		return n
	}
	return cur.Watcher.Today > prev.Watcher.Today || merged(cur) > merged(prev)
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	case m.modal && key.Matches(msg, keys.Close, keys.Enter):
		m.modal = false
	case m.modal && key.Matches(msg, keys.Open):
		return m.open()
	case m.modal:
	case key.Matches(msg, keys.Close):
		m.showHelp = false
	case key.Matches(msg, keys.Next):
		m.focus = (m.focus + 1) % nPanels
	case key.Matches(msg, keys.Prev):
		m.focus = (m.focus + nPanels - 1) % nPanels
	case key.Matches(msg, keys.Up):
		if m.sel[m.focus] > 0 {
			m.sel[m.focus]--
		}
	case key.Matches(msg, keys.Down):
		if m.sel[m.focus] < len(m.rows(m.focus))-1 {
			m.sel[m.focus]++
		}
	case key.Matches(msg, keys.Enter):
		m.modal = len(m.detail()) > 0
	case key.Matches(msg, keys.Open):
		return m.open()
	case key.Matches(msg, keys.Refresh):
		if m.loading {
			return m, nil
		}
		m.status, m.loading = "refreshed", true
		return m, m.reload()
	}
	return m, nil
}

func (m Model) open() (tea.Model, tea.Cmd) {
	u := m.url()
	if u == "" {
		m.status = "no URL for this row"
		return m, nil
	}
	if !webURL(u) {
		m.status = "not a web URL"
		return m, nil
	}
	m.status = "opening " + u
	opener := m.opener
	return m, func() tea.Msg { return openedMsg{opener(u)} }
}

func (m *Model) clamp() {
	for p := panel(0); p < nPanels; p++ {
		if n := len(m.rows(p)); m.sel[p] >= n {
			m.sel[p] = max(n-1, 0)
		}
	}
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

// flatItem is one selectable Batches row.
type flatItem struct {
	batch string
	item  Item
}

func (m Model) items() []flatItem {
	var out []flatItem
	for _, b := range m.snap.Batches {
		for _, it := range b.Items {
			out = append(out, flatItem{b.Name, it})
		}
	}
	return out
}

func (m Model) rows(p panel) []int {
	n := 0
	switch p {
	case pWatcher:
		n = 1
	case pQueue:
		n = len(m.snap.Queue)
	case pBatches:
		n = len(m.items())
	case pTasks:
		n = len(m.snap.Tasks)
	case pDecisions:
		n = len(m.snap.Decisions)
	}
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func (m Model) url() string {
	i := m.sel[m.focus]
	switch m.focus {
	case pQueue:
		if i < len(m.snap.Queue) {
			return m.snap.Queue[i].URL
		}
	case pBatches:
		if it := m.items(); i < len(it) && it[i].item.PR != "-" {
			return it[i].item.PR
		}
	case pDecisions:
		if i < len(m.snap.Decisions) {
			if _, ref, ok := strings.Cut(m.snap.Decisions[i], "ref: "); ok {
				return strings.TrimSpace(ref)
			}
		}
	}
	return ""
}

// detail is the modal's key/value lines for the selected row.
func (m Model) detail() [][2]string {
	i := m.sel[m.focus]
	w := m.snap.Watcher
	switch m.focus {
	case pWatcher:
		d := [][2]string{{"state", aliveWord(w)}, {"sources", strings.Join(w.Sources, ", ")},
			{"last poll", pollWord(w, m.snap.At)}, {"today", fmt.Sprintf("%d of %d", w.Today, w.Cap)},
			{"hours", hoursWord(w)}, {"every", fmt.Sprintf("%ds", w.Every)}, {"queued", fmt.Sprint(len(m.snap.Queue))}}
		for _, e := range m.snap.Errors {
			d = append(d, [2]string{"error", e})
		}
		return d
	case pQueue:
		if i < len(m.snap.Queue) {
			q := m.snap.Queue[i]
			return [][2]string{{"issue", q.Source + " " + q.ID}, {"title", q.Title}, {"model", or(q.Model)},
				{"speed", or(q.Speed)}, {"queued", q.QueuedAt.Local().Format("2006-01-02 15:04")}, {"url", q.URL}}
		}
	case pBatches:
		if it := m.items(); i < len(it) {
			r := it[i].item
			return [][2]string{{"batch", it[i].batch}, {"item", r.ID}, {"title", or(r.Title)}, {"stage", r.Stage},
				{"state", r.State}, {"phase", r.Phase}, {"progress", r.Progress}, {"model", r.Model},
				{"ci", r.CI}, {"pr", r.PR}, {"age", r.Age}, {"note", or(r.Text)},
				{"card", cardPath(it[i].batch, r.ID)}}
		}
	case pTasks:
		if i < len(m.snap.Tasks) {
			tl := m.snap.Tasks[i]
			d := [][2]string{{"dir", tl.Dir}}
			for _, t := range tl.Tasks {
				d = append(d, [2]string{t.NN, t.Status + "  " + t.Title})
			}
			return d
		}
	case pDecisions:
		if i < len(m.snap.Decisions) {
			f := strings.Split(m.snap.Decisions[i], " · ")
			names := []string{"date", "by", "decision", "why", "ref"}
			var d [][2]string
			for j, v := range f {
				n := "field"
				if j < len(names) {
					n = names[j]
				}
				d = append(d, [2]string{n, strings.TrimPrefix(v, "ref: ")})
			}
			return d
		}
	}
	return nil
}

func aliveWord(w Watcher) string {
	switch {
	case w.Alive:
		return "alive"
	case w.Polled:
		return "stale"
	}
	return "never polled"
}

func pollWord(w Watcher, now time.Time) string {
	if !w.Polled {
		return "never"
	}
	return w.LastPoll.Local().Format("15:04:05") + " (" + ago(now.Sub(w.LastPoll)) + ")"
}

func hoursWord(w Watcher) string {
	if w.Hours == "" {
		return "any time"
	}
	if w.OpenNow {
		return w.Hours + " (open)"
	}
	return w.Hours + " (closed)"
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
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
