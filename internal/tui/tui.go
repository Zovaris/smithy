package tui

import (
	"fmt"
	"image/color"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Usage is the text for `smithy tui --help`.
const Usage = `usage: smithy tui
A live, read-only dashboard: the auto watcher, its queue, warden batches, task lists and
pending decisions. It refreshes every 2 s from local files only (no network, no tokens).
Keys: tab/shift+tab or left/right switch panel, up/down select, enter details, esc close,
o open the URL, r refresh, q quit.
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
	Next, Prev, Up, Down, Enter, Close, Open, Refresh, Quit key.Binding
}

var keys = keyMap{
	Next:    key.NewBinding(key.WithKeys("tab", "right"), key.WithHelp("tab/→", "next panel")),
	Prev:    key.NewBinding(key.WithKeys("shift+tab", "left"), key.WithHelp("⇧tab/←", "prev")),
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "select")),
	Down:    key.NewBinding(key.WithKeys("down", "j")),
	Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
	Close:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	Open:    key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open url")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

type tickMsg time.Time
type snapMsg Snapshot
type openedMsg struct{ err error }

// Model is the dashboard's Bubble Tea model.
type Model struct {
	snap  Snapshot
	focus panel
	sel   [nPanels]int
	modal bool
	// loading is true while a load runs; ticks skip their load until it lands.
	loading bool
	width   int
	height  int
	dark    bool
	status  string
	now     func() time.Time
	load    func(time.Time) Snapshot
	opener  func(string) error
}

// New builds the model with the real clock, loader and URL opener.
func New() Model {
	m := Model{width: 120, height: 40, dark: true, now: time.Now, load: Load, opener: openURL}
	m.snap = m.load(m.now())
	return m
}

// Init starts the refresh loop and asks the terminal for its background color.
func (m Model) Init() tea.Cmd { return tea.Batch(tick(), tea.RequestBackgroundColor) }

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) reload() tea.Cmd {
	load, now := m.load, m.now
	return func() tea.Msg { return snapMsg(load(now())) }
}

// Update handles keys, resizes, ticks and fresh snapshots.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
	case tickMsg:
		if m.loading {
			return m, tick()
		}
		m.loading = true
		return m, tea.Batch(m.reload(), tick())
	case snapMsg:
		m.loading = false
		if msg.At.Before(m.snap.At) {
			return m, nil
		}
		m.snap = Snapshot(msg)
		m.clamp()
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

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case m.modal && key.Matches(msg, keys.Close, keys.Enter):
		m.modal = false
	case m.modal && key.Matches(msg, keys.Open):
		return m.open()
	case m.modal:
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
				{"ci", r.CI}, {"pr", r.PR}, {"age", r.Age}, {"note", or(r.Text)}}
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

// palette holds the colors for the terminal's background.
type palette struct {
	accent, muted, text, ok, warn, bad, border color.Color
}

func (m Model) colors() palette {
	ld := lipgloss.LightDark(m.dark)
	c := lipgloss.Color
	return palette{
		accent: ld(c("#5A4FCF"), c("#A89BFF")),
		muted:  ld(c("#8A8A8A"), c("#6C6C6C")),
		text:   ld(c("#1F1F1F"), c("#E4E4E4")),
		ok:     ld(c("#1E8A4C"), c("#5FD38D")),
		warn:   ld(c("#A86A00"), c("#F2C14E")),
		bad:    ld(c("#C0392B"), c("#FF6B6B")),
		border: ld(c("#C8C8C8"), c("#3A3A3A")),
	}
}

// View renders the dashboard.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "smithy tui"
	return v
}

func (m Model) render() string {
	w, h := max(m.width, 40), max(m.height, 12)
	head := m.header(w)
	foot := m.footer(w)
	bodyH := h - lipgloss.Height(head) - lipgloss.Height(foot)
	var body string
	if w >= 100 && bodyH >= 24 {
		body = m.wide(w, bodyH)
	} else {
		body = m.stacked(w, bodyH)
	}
	screen := lipgloss.JoinVertical(lipgloss.Left, head, body, foot)
	if !m.modal {
		return screen
	}
	box := m.modalBox(w, bodyH)
	x := max((w-lipgloss.Width(box))/2, 0)
	y := lipgloss.Height(head) + max((bodyH-lipgloss.Height(box))/2, 0)
	return lipgloss.NewCompositor(lipgloss.NewLayer(screen), lipgloss.NewLayer(box).X(x).Y(y).Z(1)).Render()
}

func (m Model) header(w int) string {
	c := m.colors()
	left := lipgloss.NewStyle().Bold(true).Foreground(c.accent).Render("smithy") + " " +
		lipgloss.NewStyle().Foreground(c.muted).Render(Version+" · tui")
	right := lipgloss.NewStyle().Foreground(c.muted).Render(m.snap.At.Local().Format("Mon 02 Jan 15:04:05"))
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(right)-2, 1)
	return " " + left + strings.Repeat(" ", gap) + right + " "
}

func (m Model) footer(w int) string {
	c := m.colors()
	hm := help.New()
	hm.Styles = help.DefaultStyles(m.dark)
	hm.SetWidth(w - 2)
	bs := []key.Binding{keys.Next, keys.Prev, keys.Up, keys.Enter, keys.Open, keys.Refresh, keys.Quit}
	if m.modal {
		bs = []key.Binding{keys.Close, keys.Open, keys.Quit}
	}
	line := " " + hm.ShortHelpView(bs)
	if m.status != "" {
		st := lipgloss.NewStyle().Foreground(c.warn).Render(m.status)
		line = " " + ansi.Truncate(st, w-2, "…")
	}
	return line
}

func (m Model) wide(w, h int) string {
	leftW := w / 3
	topH := 9
	botH := 10
	midH := h - topH - botH
	top := lipgloss.JoinHorizontal(lipgloss.Top, m.box(pWatcher, leftW, topH), m.box(pQueue, w-leftW, topH))
	bot := lipgloss.JoinHorizontal(lipgloss.Top, m.box(pTasks, w/2, botH), m.box(pDecisions, w-w/2, botH))
	return lipgloss.JoinVertical(lipgloss.Left, top, m.box(pBatches, w, midH), bot)
}

// stacked puts the panels one under another: the focused one gets the room, the others show a summary line.
func (m Model) stacked(w, h int) string {
	var parts []string
	focusH := h - 3*(int(nPanels)-1)
	for p := panel(0); p < nPanels; p++ {
		if p == m.focus {
			parts = append(parts, m.box(p, w, max(focusH, 3)))
		} else {
			parts = append(parts, m.box(p, w, 3))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// box draws a panel with its title, a count and as many lines as fit around the selection.
func (m Model) box(p panel, w, h int) string {
	c := m.colors()
	focused := p == m.focus
	bc := c.border
	tc := c.muted
	if focused {
		bc, tc = c.accent, c.accent
	}
	inner := w - 4
	title := lipgloss.NewStyle().Bold(true).Foreground(tc).Render(titles[p]) +
		lipgloss.NewStyle().Foreground(c.muted).Render("  "+m.count(p))
	lines := []string{ansi.Truncate(title, inner, "…")}
	content := m.lines(p, inner)
	avail := h - 3
	if avail > 0 {
		start := 0
		if sel := m.sel[p]; focused && p != pWatcher && sel >= avail {
			start = sel - avail + 1
		}
		for i := start; i < len(content) && i < start+avail; i++ {
			lines = append(lines, content[i])
		}
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(bc).
		Padding(0, 1).Width(w).Height(h).MaxHeight(h).Render(strings.Join(lines, "\n"))
}

func (m Model) count(p panel) string {
	switch p {
	case pWatcher:
		return aliveWord(m.snap.Watcher)
	case pQueue:
		return fmt.Sprintf("%d queued", len(m.snap.Queue))
	case pBatches:
		return plural(len(m.snap.Batches), "batch", "batches") + " · " + plural(len(m.items()), "item", "items")
	case pTasks:
		return plural(len(m.snap.Tasks), "list", "lists")
	case pDecisions:
		return fmt.Sprintf("%d pending", len(m.snap.Decisions))
	}
	return ""
}

func (m Model) empty(s string) []string {
	return []string{lipgloss.NewStyle().Foreground(m.colors().muted).Italic(true).Render(s)}
}

// lines renders a panel's rows, already cut to width; the selected row is highlighted when focused.
func (m Model) lines(p panel, w int) []string {
	c := m.colors()
	mut := lipgloss.NewStyle().Foreground(c.muted)
	txt := lipgloss.NewStyle().Foreground(c.text)
	var rows []string
	switch p {
	case pWatcher:
		wt := m.snap.Watcher
		st := lipgloss.NewStyle().Foreground(c.bad)
		if wt.Alive {
			st = lipgloss.NewStyle().Foreground(c.ok)
		} else if !wt.Polled {
			st = mut
		}
		kv := func(k, v string) string { return mut.Render(fmt.Sprintf("%-10s", k)) + v }
		rows = []string{
			kv("state", st.Render("● "+aliveWord(wt))),
			kv("sources", txt.Render(or(strings.Join(wt.Sources, ", ")))),
			kv("last poll", txt.Render(pollWord(wt, m.snap.At))),
			kv("today", txt.Render(fmt.Sprintf("%d / %d", wt.Today, wt.Cap))),
			kv("hours", txt.Render(hoursWord(wt))),
		}
		for _, e := range m.snap.Errors {
			rows = append(rows, lipgloss.NewStyle().Foreground(c.bad).Render("! "+e))
		}
		return cut(rows, w)
	case pQueue:
		if len(m.snap.Queue) == 0 {
			return m.empty("nothing queued")
		}
		for _, q := range m.snap.Queue {
			rows = append(rows, "  "+txt.Render(fmt.Sprintf("%-14s ", q.Source+" "+q.ID))+
				mut.Render(fmt.Sprintf("%-7s %-7s ", or(q.Model), or(q.Speed)))+txt.Render(q.Title))
		}
	case pBatches:
		if len(m.snap.Batches) == 0 {
			return m.empty("no warden batches")
		}
		rows = append(rows, mut.Render(fmt.Sprintf("  %-22s %-9s %-14s %-7s %-8s %s", "item", "stage", "state", "model", "ci", "pr")))
		for _, it := range m.items() {
			r := it.item
			rows = append(rows, "  "+txt.Render(fmt.Sprintf("%-22s ", ansi.Truncate(it.batch+"/"+r.ID, 22, "…")))+
				m.stageStyle(r.Stage).Render(fmt.Sprintf("%-9s ", r.Stage))+
				mut.Render(fmt.Sprintf("%-14s %-7s ", r.State, r.Model))+
				m.ciStyle(r.CI).Render(fmt.Sprintf("%-8s ", r.CI))+mut.Render(shortPR(r.PR)))
		}
		return m.highlight(p, cut(rows, w), 1)
	case pTasks:
		if len(m.snap.Tasks) == 0 {
			return m.empty("no task lists")
		}
		for _, tl := range m.snap.Tasks {
			var parts []string
			for _, s := range statuses(tl.Counts) {
				parts = append(parts, fmt.Sprintf("%d %s", tl.Counts[s], s))
			}
			rows = append(rows, "  "+txt.Render(fmt.Sprintf("%-22s ", ansi.Truncate(filepath.Base(tl.Dir), 22, "…")))+
				mut.Render(strings.Join(parts, " · ")))
		}
	case pDecisions:
		if len(m.snap.Decisions) == 0 {
			return m.empty("no pending decisions")
		}
		for _, d := range m.snap.Decisions {
			f := strings.Split(d, " · ")
			q := d
			if len(f) >= 3 {
				q = f[2]
			}
			rows = append(rows, "  "+txt.Render(q))
		}
	}
	return m.highlight(p, cut(rows, w), 0)
}

// highlight marks the selected row (rows start with two spaces; offset skips a header row).
func (m Model) highlight(p panel, rows []string, offset int) []string {
	i := m.sel[p] + offset
	if p != m.focus || i >= len(rows) {
		return rows
	}
	plain := strings.TrimPrefix(ansi.Strip(rows[i]), "  ")
	rows[i] = lipgloss.NewStyle().Foreground(m.colors().accent).Bold(true).Render("› " + plain)
	return rows
}

func cut(rows []string, w int) []string {
	for i, r := range rows {
		rows[i] = ansi.Truncate(r, w, "…")
	}
	return rows
}

func shortPR(pr string) string {
	if i := strings.LastIndex(pr, "/pull/"); i >= 0 {
		return "#" + pr[i+len("/pull/"):]
	}
	return pr
}

func (m Model) stageStyle(s string) lipgloss.Style {
	c := m.colors()
	switch s {
	case "merged", "reported", "verify":
		return lipgloss.NewStyle().Foreground(c.ok)
	case "stuck", "decide":
		return lipgloss.NewStyle().Foreground(c.bad)
	case "working", "starting", "waiting":
		return lipgloss.NewStyle().Foreground(c.warn)
	}
	return lipgloss.NewStyle().Foreground(c.muted)
}

func (m Model) ciStyle(s string) lipgloss.Style {
	c := m.colors()
	switch s {
	case "green", "pass", "success":
		return lipgloss.NewStyle().Foreground(c.ok)
	case "red", "fail", "failure":
		return lipgloss.NewStyle().Foreground(c.bad)
	}
	return lipgloss.NewStyle().Foreground(c.muted)
}

func (m Model) modalBox(w, h int) string {
	c := m.colors()
	d := m.detail()
	bw := min(w-4, 90)
	inner := bw - 4
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(c.accent).Render(titles[m.focus] + " · details"), ""}
	kw := 0
	for _, kv := range d {
		kw = max(kw, len(kv[0]))
	}
	for _, kv := range d {
		k := lipgloss.NewStyle().Foreground(c.muted).Render(fmt.Sprintf("%-*s  ", kw, kv[0]))
		lines = append(lines, ansi.Truncate(k+lipgloss.NewStyle().Foreground(c.text).Render(kv[1]), inner, "…"))
	}
	if len(lines) > h-2 {
		lines = lines[:max(h-2, 1)]
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c.accent).
		Padding(0, 1).Width(bw).Render(strings.Join(lines, "\n"))
}

// Main runs `smithy tui`.
func Main(args []string) int {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println(Usage)
			return 0
		}
		fmt.Fprintf(os.Stderr, "smithy tui: unexpected argument %s\n%s\n", a, Usage)
		return 2
	}
	if _, err := tea.NewProgram(New()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "smithy tui:", err)
		return 1
	}
	return 0
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
