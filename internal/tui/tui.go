package tui

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/decisions"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/harmonica"
)

// Usage is the text for `smithy tui --help`.
const Usage = `usage: smithy tui [--no-anim] [--demo]
A live dashboard of the autonomous forge. Views: Overview, Agents, Incoming, Needs you,
Done, Activity, Worktrees, Plans, System, History. It refreshes every 2 s from local files only (no network).
Keys: tab/shift+tab or 1-9, 0 switch views; up/down (j/k) move inside a view; left/right switch
panes or cards; enter details or read; esc close; o open; l log; c copy; y copy the action command;
a accept / x reject a decision (Needs you);
s sort (Agents); t event type (Activity); p pause/resume intake (auto.paused); / filter;
r refresh; ? help; q quit. Mouse: click, double-click, wheel.
--demo shows a rich fake world (reads and writes nothing); --no-anim (or
SMITHY_REDUCE_MOTION=1) draws still frames; NO_COLOR=1 drops color.
Exit 0 = quit, 1 = the terminal could not run it, 2 = bad usage.`

// Version is the binary's version, shown in the header.
var Version = "dev"

const refreshEvery = 2 * time.Second

type view int

const (
	vOverview view = iota
	vAgents
	vIncoming
	vNeeds
	vDone
	vActivity
	vWorktrees
	vPlans
	vSystem
	vHistory
	nViews
)

var viewNames = [nViews]string{"Overview", "Agents", "Incoming", "Needs you", "Done", "Activity", "Worktrees", "Plans", "System", "History"}

// meaning is what each view holds, for empty states and the help overlay.
var meaning = [nViews]string{
	"Charts of the forge: throughput, agents by stage, the daily cap, CI health, lead time.",
	"Every worker: issue, repo, branch, model, stage, CI, PR, heartbeat; a timeline and log preview.",
	"Issues waiting to start, the ones recently skipped and why, and each source's status.",
	"Decisions taken without you, PRs ready for review or merge, blocked work.",
	"Finished work: issue, title, PR, result, how long it took and when.",
	"Every event, newest first: polls, queued, skipped, launches, stages, CI, reports, merges.",
	"Every smithy worktree on disk: branch, item, age, dirty, ahead/behind, orphaned.",
	"Task lists and their progress; enter reads the plan's spec or a task.",
	"Health (doctor, auth, versions, paths) and every setting: enter edits one, u unsets it.",
	"Past days' finished work from the last 30 days of events: issue, PR, cost and outcome, by day.",
}

type keyMap struct {
	Next, Prev, Up, Down, Left, Right, Jump, Enter, Close, Open, Log, Copy, Cmd, Sort, Type, Pause, Filter, Refresh, Help, Quit, Accept, Reject, Edit, Unset key.Binding
}

var keys = keyMap{
	Next:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next view")),
	Prev:    key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧tab", "previous view")),
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑↓", "move")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Left:    key.NewBinding(key.WithKeys("left"), key.WithHelp("←→", "pane")),
	Right:   key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "pane")),
	Jump:    key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9", "0"), key.WithHelp("0-9", "view")),
	Accept:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "accept")),
	Reject:  key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "reject")),
	Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
	Edit:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")),
	Unset:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "unset setting")),
	Close:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	Open:    key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open")),
	Log:     key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "log")),
	Copy:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy")),
	Cmd:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy command")),
	Sort:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
	Type:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "type")),
	Pause:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause intake")),
	Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

// context is the footer's keys for a view.
func viewKeys(v view) []key.Binding {
	switch v {
	case vOverview:
		return []key.Binding{keys.Up, keys.Enter}
	case vAgents:
		return []key.Binding{keys.Up, keys.Left, keys.Enter, keys.Log, keys.Open, keys.Copy, keys.Sort, keys.Filter}
	case vIncoming:
		return []key.Binding{keys.Up, keys.Left, keys.Enter, keys.Open, keys.Copy, keys.Pause, keys.Filter}
	case vNeeds:
		return []key.Binding{keys.Up, keys.Enter, keys.Accept, keys.Reject, keys.Open, keys.Cmd}
	case vDone, vHistory:
		return []key.Binding{keys.Up, keys.Enter, keys.Open, keys.Copy, keys.Filter}
	case vSystem:
		return []key.Binding{keys.Up, keys.Edit, keys.Unset, keys.Refresh, keys.Log}
	case vActivity:
		return []key.Binding{keys.Up, keys.Enter, keys.Open, keys.Type, keys.Filter}
	case vWorktrees:
		return []key.Binding{keys.Up, keys.Enter, keys.Copy, keys.Filter}
	}
	return []key.Binding{keys.Up, keys.Left, keys.Enter, keys.Copy}
}

// actionKeys is what the modal footer offers for the selected row.
func (m Model) actionKeys() []key.Binding {
	r, _ := m.selected()
	var bs []key.Binding
	if r.decision > 0 {
		bs = append(bs, keys.Accept, keys.Reject)
	}
	if r.url != "" && r.url != "-" {
		bs = append(bs, keys.Open)
	}
	if r.cmd != "" {
		bs = append(bs, keys.Cmd)
	}
	if r.logPath != "" || r.logTail != nil {
		bs = append(bs, keys.Log)
	}
	if r.copy != "" {
		bs = append(bs, keys.Copy)
	}
	return append(bs, keys.Close)
}

var activityTypes = []string{"all", "poll", "queued", "skipped", "launched", "stage", "ci", "report", "merged"}
var agentSorts = []string{"stage", "beat", "age", "issue"}

type tickMsg time.Time
type frameMsg struct{}
type strikeMsg struct{}
type snapMsg Snapshot
type openedMsg struct{ err error }
type pausedMsg struct {
	paused bool
	err    error
}
type decidedMsg struct {
	what string
	err  error
}
type systemMsg System

// Model is the dashboard's Bubble Tea model.
type Model struct {
	snap      Snapshot
	view      view
	sel       [nViews]int
	sel2      [nViews]int // selection in a view's second pane
	pane      [nViews]int // 0 main pane, 1 second pane
	agentSort int
	actType   int
	modal     bool
	helpOpen  bool
	textOpen  bool
	text      viewport.Model
	textTitle string
	prompting bool // reading a reject reason
	promptN   int
	reason    textinput.Model
	sys       *System
	sysBusy   bool
	loadSys   func() System
	confirm   func(n int) error
	reject    func(n int, why string) error
	edit      *edit
	setter    setterFunc
	filtering bool
	input     textinput.Model
	filter    [nViews]string
	loading   bool // a load is running; ticks skip theirs until it lands
	width     int
	height    int
	dark      bool
	profile   colorprofile.Profile
	unicode   bool
	motion    bool
	framing   bool // a frame loop is scheduled
	ticks     int
	frames    int // frames drawn; moves the shimmer on running items
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
	theme   string // ui.theme
	now     func() time.Time
	load    func(time.Time) Snapshot
}

func newModel(o options) Model {
	m := Model{width: 120, height: 40, dark: o.dark, profile: o.profile, unicode: o.unicode, motion: o.motion,
		now: o.now, load: o.load, opener: openURL, pauser: setPaused, fx: newFX(o.seed),
		spring: harmonica.NewSpring(harmonica.FPS(fps), 4.0, 0.95), input: textinput.New(), reason: textinput.New(),
		loadSys: loadSystem, setter: writeSetting, confirm: decisions.Confirm, reject: decisions.Reject}
	m.reason.Prompt = "why reject? "
	m.reason.Placeholder = "one line the agent will read"
	m.input.Prompt = "/ "
	m.input.Placeholder = "filter this view"
	m.th = namedTheme(o.theme, m.dark, m.profile, m.unicode)
	m.snap = m.load(m.now())
	m.capPos = capPercent(m.snap.Watcher)
	if o.toast {
		m.toast = "tab switches views · ↑↓ move · enter for details · ? for help"
		m.toastTill = m.now().Add(4 * time.Second)
	}
	return m
}

// New builds the model from the environment: NO_COLOR, SMITHY_REDUCE_MOTION, the locale, and
// ~/.smithy/tui.seen for the first-run hint. In demo mode nothing is read or written.
func New(noAnim, demo bool) Model {
	p := colorprofile.TrueColor
	if os.Getenv("NO_COLOR") != "" {
		p = colorprofile.Ascii
	}
	first := false
	if !demo {
		seen := filepath.Join(ghx.Home(), "tui.seen")
		_, err := os.Stat(seen)
		if first = os.IsNotExist(err); first {
			_ = os.MkdirAll(filepath.Dir(seen), 0o700)
			_ = os.WriteFile(seen, []byte(time.Now().Format(time.RFC3339)+"\n"), 0o600)
		}
	}
	load := Load
	if demo {
		events.Off = true
		load = Demo
	}
	motion := motionOn(noAnim, os.Getenv("SMITHY_REDUCE_MOTION"), config.Get("tui.reduce_motion", "false"), p)
	m := newModel(options{profile: p, dark: true, unicode: utf8Locale(), motion: motion, toast: first || demo, theme: config.Get("ui.theme", "steel"),
		seed: uint64(time.Now().UnixNano()), now: time.Now, load: load})
	if demo {
		m.pauser = func(bool) error { return fmt.Errorf("demo: intake is not real") }
		m.confirm, m.reject, m.setter = nil, nil, nil
		m.loadSys = demoSystem
	}
	return m
}

// motionOn is false for --no-anim, SMITHY_REDUCE_MOTION, tui.reduce_motion=true or no color:
// then every frame is static.
func motionOn(noAnim bool, env, setting string, p colorprofile.Profile) bool {
	return !noAnim && env == "" && setting != "true" && p != colorprofile.Ascii
}

// running is a stage that moves: building or checking.
func running(stage string) bool { return stage == "building" || stage == "checking" }

// running reports whether any item is building or checking.
func (m Model) running() bool {
	for _, it := range m.items() {
		if running(stageOf(it)) {
			return true
		}
	}
	return false
}

// band is the shimmer center for a text n cells wide, offset by k so rows don't move in lockstep.
func (m Model) band(n, k int) float64 {
	span := n + 2*shimmerHalf
	return float64((m.frames+k*7)%span) - shimmerHalf
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

// Init starts the refresh loop, asks for the background color and strikes the plate once.
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
		return bigPlate
	}
	return smallPlate
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
	return m.fx.busy() || (m.motion && m.running()) || abs(m.capPos-capPercent(m.snap.Watcher)) > 0.002 || abs(m.capVel) > 0.002
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
		m.sizeText()
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.th = namedTheme(m.th.name, m.dark, m.profile, m.unicode)
	case tea.ColorProfileMsg:
		if os.Getenv("NO_COLOR") == "" {
			m.profile = msg.Profile
			m.th = namedTheme(m.th.name, m.dark, m.profile, m.unicode)
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
		m.frames++
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
		if happened(prev, m.snap) {
			return m, m.strike(12)
		}
		return m, m.animate()
	case openedMsg:
		m.status = ""
		if msg.err != nil {
			m.status = "open: " + msg.err.Error()
		}
	case decidedMsg:
		if msg.err != nil {
			m.status = msg.what + ": " + msg.err.Error()
			return m, nil
		}
		m.say(msg.what)
		m.modal, m.loading = false, true
		return m, m.reload()
	case savedMsg:
		if msg.err != nil {
			m.status = "config: " + msg.err.Error()
			return m, nil
		}
		m.say(msg.what)
		m.sys = nil
		return m, m.show(vSystem)
	case systemMsg:
		s := System(msg)
		m.sys, m.sysBusy = &s, false
		for _, r := range s.Settings {
			if r.Key == "ui.theme" {
				m.th = m.th.renamed(r.Value)
			}
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
		if m.textOpen {
			var cmd tea.Cmd
			m.text, cmd = m.text.Update(msg)
			return m, cmd
		}
		if mm.Button == tea.MouseWheelUp {
			m.move(-1)
		} else if mm.Button == tea.MouseWheelDown {
			m.move(1)
		}
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// happened reports a real change worth a strike: something queued or launched, a PR ready, a merge.
func happened(prev, cur Snapshot) bool {
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

// move moves the selection of the focused pane of the current view.
func (m *Model) move(d int) {
	v := m.view
	if m.pane[v] == 1 {
		n := len(m.rows2(v))
		m.sel2[v] = max(min(m.sel2[v]+d, n-1), 0)
		return
	}
	n := len(m.rows(v))
	switch {
	case v == vOverview:
		n = 6
	case v == vSystem && m.sys != nil:
		n = len(m.sys.Settings)
	}
	m.sel[v] = max(min(m.sel[v]+d, n-1), 0)
}

// say shows a toast for 4 s.
func (m *Model) say(s string) {
	m.toast, m.toastTill = s, m.now().Add(4*time.Second)
}

// show switches to a view, loading the System report the first time it opens.
func (m *Model) show(v view) tea.Cmd {
	m.view = v
	if v == vSystem && m.sys == nil && !m.sysBusy {
		m.sysBusy = true
		load := m.loadSys
		return func() tea.Msg { return systemMsg(load()) }
	}
	return nil
}

// decide accepts or rejects pending decision n; in demo mode it only says what it would run.
func (m Model) decide(n int, accept bool, why string) (tea.Model, tea.Cmd) {
	cmd := fmt.Sprintf("smithy decisions confirm %d", n)
	if !accept {
		cmd = fmt.Sprintf("smithy decisions reject %d %s", n, sh(why))
	}
	if (accept && m.confirm == nil) || (!accept && m.reject == nil) {
		m.say("demo: would run " + cmd)
		return m, nil
	}
	confirm, reject := m.confirm, m.reject
	return m, func() tea.Msg {
		if accept {
			return decidedMsg{fmt.Sprintf("accepted decision %d", n), confirm(n)}
		}
		return decidedMsg{fmt.Sprintf("rejected decision %d: the agent sees it in smithy decisions pending", n), reject(n, why)}
	}
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.edit != nil {
		return m.editKey(msg)
	}
	if m.prompting {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.prompting = false
			m.reason.Blur()
			return m, nil
		case "enter":
			why := strings.TrimSpace(m.reason.Value())
			if why == "" {
				m.status = "type a reason, or esc to cancel"
				return m, nil
			}
			m.prompting = false
			m.reason.Blur()
			return m.decide(m.promptN, false, why)
		}
		var cmd tea.Cmd
		m.reason, cmd = m.reason.Update(msg)
		return m, cmd
	}
	if m.filtering {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.filtering = false
			m.filter[m.view] = ""
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
		m.filter[m.view] = m.input.Value()
		m.sel[m.view] = 0
		return m, cmd
	}
	if m.textOpen {
		switch {
		case msg.String() == "ctrl+c":
			return m, tea.Quit
		case key.Matches(msg, keys.Close, keys.Log, keys.Quit, keys.Enter):
			m.textOpen = false
			return m, nil
		}
		var cmd tea.Cmd
		m.text, cmd = m.text.Update(msg)
		return m, cmd
	}
	m.status = ""
	v := m.view
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Help):
		m.helpOpen = !m.helpOpen
		m.modal = false
	case key.Matches(msg, keys.Close):
		m.modal, m.helpOpen, m.toast = false, false, ""
		if m.filter[v] != "" {
			m.filter[v] = ""
			m.input.SetValue("")
		}
	case m.helpOpen:
	case m.modal && key.Matches(msg, keys.Enter):
		m.modal = false
	case key.Matches(msg, keys.Open):
		return m.open()
	case key.Matches(msg, keys.Copy):
		return m.copyField(false)
	case key.Matches(msg, keys.Cmd):
		return m.copyField(true)
	case key.Matches(msg, keys.Log):
		return m.openLog()
	case key.Matches(msg, keys.Accept, keys.Reject) && v == vNeeds:
		r, ok := m.selected()
		if !ok || r.decision == 0 {
			m.status = "a and x act on decisions; this row is not one"
			return m, nil
		}
		if key.Matches(msg, keys.Accept) {
			return m.decide(r.decision, true, "")
		}
		m.prompting, m.promptN = true, r.decision
		m.reason.SetValue("")
		return m, m.reason.Focus()
	case m.modal:
	case key.Matches(msg, keys.Jump):
		return m, m.show(view((msg.String()[0] - '0' + 9) % 10))
	case key.Matches(msg, keys.Next):
		return m, m.show((v + 1) % nViews)
	case key.Matches(msg, keys.Prev):
		return m, m.show((v + nViews - 1) % nViews)
	case key.Matches(msg, keys.Up):
		if v == vOverview {
			m.move(-3)
		} else {
			m.move(-1)
		}
	case key.Matches(msg, keys.Down):
		if v == vOverview {
			m.move(3)
		} else {
			m.move(1)
		}
	case key.Matches(msg, keys.Left, keys.Right):
		d := 1
		if key.Matches(msg, keys.Left) {
			d = -1
		}
		switch v {
		case vOverview:
			m.move(d)
		case vAgents, vIncoming, vPlans:
			m.pane[v] = max(min(m.pane[v]+d, 1), 0)
		}
	case key.Matches(msg, keys.Enter):
		return m.enter()
	case key.Matches(msg, keys.Unset) && v == vSystem:
		return m.openEdit(true)
	case key.Matches(msg, keys.Sort) && v == vAgents:
		m.agentSort = (m.agentSort + 1) % len(agentSorts)
		m.status = "sorted by " + agentSorts[m.agentSort]
	case key.Matches(msg, keys.Type) && v == vActivity:
		m.actType = (m.actType + 1) % len(activityTypes)
		m.sel[v] = 0
	case key.Matches(msg, keys.Filter):
		m.filtering = true
		m.input.SetValue(m.filter[v])
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

// enter opens the selected row's details; on the Overview it jumps to the card's view, and on a
// plan's task it opens the task file.
func (m Model) enter() (tea.Model, tea.Cmd) {
	v := m.view
	if v == vOverview {
		return m, m.show([]view{vActivity, vAgents, vIncoming, vAgents, vDone, vNeeds}[m.sel[vOverview]])
	}
	if v == vSystem {
		return m.openEdit(false)
	}
	if r, ok := m.selected(); ok && r.md != "" {
		m.showMarkdown(r.mdTitle, r.md)
		return m, nil
	}
	if _, ok := m.selected(); ok {
		m.modal = true
	}
	return m, nil
}

// showMarkdown opens the reader with markdown rendered by glamour (plain text without color).
func (m *Model) showMarkdown(title, src string) {
	w := max(min(m.width-8, 140), 20)
	style := "dark"
	switch {
	case m.th.plain || !m.unicode:
		style = "ascii"
	case !m.dark:
		style = "light"
	}
	out := src
	if r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(w-4)); err == nil {
		if s, err := r.Render(src); err == nil {
			out = osc8.ReplaceAllString(s, "")
		}
	}
	m.showText(title, strings.Split(strings.TrimRight(out, "\n"), "\n"))
}

func (m *Model) showText(title string, lines []string) {
	m.textOpen, m.modal = true, false
	m.textTitle = title
	m.text = viewport.New()
	m.sizeText()
	m.text.SetContentLines(lines)
}

// click: a tab switches views, a row selects (a second click on it opens), elsewhere closes overlays.
func (m Model) click(mm tea.Mouse) (tea.Model, tea.Cmd) {
	if m.modal || m.helpOpen || m.textOpen || m.prompting {
		m.modal, m.helpOpen, m.textOpen, m.prompting = false, false, false, false
		return m, nil
	}
	if v, ok := m.tabAt(mm.X, mm.Y); ok {
		return m, m.show(v)
	}
	if i, ok := m.rowAt(mm.X, mm.Y); ok {
		v := m.view
		double := m.sel[v] == i && m.clickPos == [2]int{mm.X, mm.Y} && m.now().Sub(m.clickAt) < 500*time.Millisecond
		m.sel[v], m.pane[v] = i, 0
		m.clickAt, m.clickPos = m.now(), [2]int{mm.X, mm.Y}
		if double {
			return m.enter()
		}
	}
	return m, nil
}

func (m Model) open() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok || r.url == "" || r.url == "-" {
		m.status = "no URL for this row"
		return m, nil
	}
	if !webURL(r.url) {
		m.status = "not a web URL"
		return m, nil
	}
	if m.snap.Demo {
		m.say("demo: would open " + r.url)
		return m, nil
	}
	m.status = "opening " + r.url
	opener, u := m.opener, r.url
	return m, func() tea.Msg { return openedMsg{opener(u)} }
}

func (m Model) copyField(command bool) (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	s := r.copy
	if command {
		s = r.cmd
	}
	if !ok || s == "" {
		m.status = "nothing to copy on this row"
		return m, nil
	}
	m.status = "copied " + s
	return m, tea.SetClipboard(s)
}

func (m Model) openLog() (tea.Model, tea.Cmd) {
	if m.view == vSystem {
		p := m.snap.Watcher.Serve.SessionLog
		if p == "" {
			m.status = "no serve session log (smithy auto serve starts one per task dir)"
			return m, nil
		}
		lines := tail(p, 2000)
		if lines == nil {
			m.status = "no log yet at " + p
			return m, nil
		}
		m.showText("log "+home(p), lines)
		m.text.GotoBottom()
		return m, nil
	}
	r, ok := m.selected()
	if !ok || (r.logPath == "" && r.logTail == nil) {
		m.status = "no log for this row"
		return m, nil
	}
	lines := r.logTail
	if r.logPath != "" {
		lines = tail(r.logPath, 2000)
	}
	if lines == nil {
		m.status = "no log yet at " + r.logPath
		return m, nil
	}
	m.showText("log "+home(r.logPath), lines)
	m.text.GotoBottom()
	return m, nil
}

func (m *Model) sizeText() {
	m.text.SetWidth(max(min(m.width-8, 140), 20))
	m.text.SetHeight(max(m.height-14, 5))
}

func (m *Model) clamp() {
	for v := view(1); v < nViews; v++ {
		if v == vSystem {
			continue
		}
		if n := len(m.rows(v)); m.sel[v] >= n {
			m.sel[v] = max(n-1, 0)
		}
		if n := len(m.rows2(v)); m.sel2[v] >= n {
			m.sel2[v] = max(n-1, 0)
		}
	}
}

// selected is the row the keys act on: the focused pane's selection.
func (m Model) selected() (row, bool) {
	v := m.view
	rs, i := m.rows(v), m.sel[v]
	if m.pane[v] == 1 {
		rs, i = m.rows2(v), m.sel2[v]
	}
	if i < len(rs) {
		return rs[i], true
	}
	return row{}, false
}

// webURL accepts only http(s) URLs with a host, so `o` never hands the opener a file or script URL.
func webURL(u string) bool {
	p, err := url.Parse(u)
	return err == nil && (p.Scheme == "http" || p.Scheme == "https") && p.Host != ""
}

// osc8 matches terminal hyperlink sequences, dropped so a link's shown text never hides its target.
var osc8 = regexp.MustCompile("\x1b\\]8;[^\x07\x1b]*(?:\x07|\x1b\\\\)")

// sh quotes s for a POSIX shell when it holds anything beyond a safe set.
func sh(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:@%+=,-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// OpenURL opens u in the default browser.
func OpenURL(u string) error { return openURL(u) }

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

func dur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
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
	noAnim, demo := false, false
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Println(Usage)
			return 0
		case "--no-anim":
			noAnim = true
		case "--demo":
			demo = true
		default:
			fmt.Fprintf(os.Stderr, "smithy tui: unexpected argument %s\n%s\n", a, Usage)
			return 2
		}
	}
	if _, err := tea.NewProgram(New(noAnim, demo)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "smithy tui:", err)
		return 1
	}
	return 0
}
