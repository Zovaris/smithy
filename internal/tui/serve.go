package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/auto"
	"github.com/charmbracelet/colorprofile"
)

// serveLines is how many recent events the serve view keeps.
const serveLines = 8

// serveView is the compact live view of smithy auto serve.
type serveView struct {
	th      theme
	sv      *auto.Server
	done    <-chan struct{}
	logs    <-chan string
	now     func() time.Time
	hb      *auto.Heartbeat
	st      auto.Status
	snap    Snapshot
	load    func(time.Time) Snapshot
	loaded  time.Time
	recent  []string
	confirm bool
	width   int
}

type serveTick struct{}
type serveLog string
type serveDone struct{}

// RunServeView draws the serve view until done closes or the user confirms a stop (Esc, q or
// Ctrl+C, then y). It only reads files; the loop runs in smithy auto serve.
func RunServeView(done <-chan struct{}, sv *auto.Server, logs <-chan string) error {
	p := colorprofile.TrueColor
	if os.Getenv("NO_COLOR") != "" {
		p = colorprofile.Ascii
	}
	m := newServeView(done, sv, logs, newTheme(true, p, utf8Locale()), time.Now, Load)
	_, err := tea.NewProgram(m).Run()
	return err
}

func newServeView(done <-chan struct{}, sv *auto.Server, logs <-chan string, th theme, now func() time.Time, load func(time.Time) Snapshot) serveView {
	m := serveView{th: th, sv: sv, done: done, logs: logs, now: now, load: load, width: 80}
	m.refresh()
	return m
}

func (m *serveView) refresh() {
	t := m.now()
	m.hb, _ = auto.ReadServe()
	if s, err := auto.LoadSettings(); err == nil {
		m.st, _ = auto.StatusOf(s)
	}
	if t.Sub(m.loaded) >= 5*time.Second {
		m.snap, m.loaded = m.load(t), t
	}
}

func (m serveView) Init() tea.Cmd {
	return tea.Batch(m.tick(), m.waitLog(), m.waitDone())
}

func (m serveView) tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return serveTick{} })
}

func (m serveView) waitLog() tea.Cmd {
	return func() tea.Msg {
		select {
		case l := <-m.logs:
			return serveLog(l)
		case <-m.done:
			return serveDone{}
		}
	}
}

func (m serveView) waitDone() tea.Cmd {
	return func() tea.Msg { <-m.done; return serveDone{} }
}

func (m serveView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case serveDone:
		return m, tea.Quit
	case serveTick:
		m.refresh()
		return m, m.tick()
	case serveLog:
		m.recent = append(m.recent, m.now().Format("15:04:05")+" "+clean(string(msg)))
		if len(m.recent) > serveLines {
			m.recent = m.recent[len(m.recent)-serveLines:]
		}
		return m, m.waitLog()
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyPressMsg:
		k := msg.String()
		if m.confirm {
			switch k {
			case "y", "Y", "enter":
				return m, tea.Quit
			default:
				m.confirm = false
			}
			return m, nil
		}
		switch k {
		case "esc", "q", "ctrl+c":
			m.confirm = true
		}
	}
	return m, nil
}

func (m serveView) View() tea.View {
	return tea.NewView(strings.Join(m.lines(), "\n"))
}

// lines renders the view: a status block, the recent events and the key hint.
func (m serveView) lines() []string {
	t := m.th
	mut, val := t.fg(t.muted), t.fg(t.text)
	now := m.now()
	w := max(m.width, 40)
	label := func(s string) string { return mut.Render(fmt.Sprintf(" %-9s", s)) }
	dot := mut.Render(" " + t.g.dot + " ")
	hb := m.hb
	health := hb.Health(now)
	state := t.fg(t.err).Render(t.g.off + " " + health)
	switch health {
	case "running":
		state = t.fg(t.ok).Render(t.g.on+" running") + dot + val.Render(fmt.Sprintf("pid %d", hb.PID)) + dot +
			val.Render("agent "+hb.Agent) + dot + mut.Render("up "+dur(now.Sub(hb.Started)))
	case "stale":
		state = t.fg(t.warn).Render(t.g.half+" stale") + dot + mut.Render(fmt.Sprintf("pid %d", hb.PID))
	}
	out := []string{" " + t.fg(t.accent).Bold(!t.plain).Render("smithy serve") + "  " + state}

	src := or(strings.Join(m.st.Sources, ", "))
	next := "-"
	loop := "-"
	if hb != nil {
		loop = hb.State
		if !hb.LastCycle.IsZero() {
			next = fmt.Sprintf("%ds", max(int(hb.LastCycle.Add(time.Duration(m.st.Every)*time.Second).Sub(now).Seconds()), 0))
		}
	}
	paused := ""
	if s, err := auto.LoadSettings(); err == nil && s.Paused {
		paused = dot + t.fg(t.warn).Render("paused")
	}
	out = append(out, label("sources")+val.Render(src)+dot+mut.Render("next poll ")+val.Render(next)+dot+mut.Render(loop)+paused)
	hours := "any time"
	if m.st.Hours != "" {
		hours = m.st.Hours
	}
	out = append(out, label("today")+val.Render(fmt.Sprintf("%d / %d", m.st.Today, m.st.Cap))+mut.Render(" launched")+dot+
		mut.Render("queue ")+val.Render(fmt.Sprint(m.st.Queued))+dot+mut.Render("hours ")+val.Render(hours))
	sess := mut.Render("none")
	if hb.SessionRunning() {
		sess = val.Render(fmt.Sprintf("pid %d", hb.SessionPID)) + dot + val.Render(hb.Agent) + dot +
			mut.Render(dur(now.Sub(hb.SessionAt))) + dot + mut.Render(short(home(hb.SessionLog), max(w-40, 20)))
	}
	out = append(out, label("session")+sess)
	working, waiting, stuck, ready, ci := Model{snap: m.snap}.counts()
	out = append(out, label("workers")+val.Render(fmt.Sprintf("%d working", working))+dot+val.Render(fmt.Sprintf("%d waiting", waiting))+dot+
		val.Render(fmt.Sprintf("%d stuck", stuck))+dot+val.Render(fmt.Sprintf("%d ready", ready))+dot+val.Render(fmt.Sprintf("%d CI running", ci)))
	out = append(out, "", label("recent"))
	if len(m.recent) == 0 {
		out = append(out, mut.Italic(true).Render("  nothing yet"))
	}
	for _, l := range m.recent {
		out = append(out, "  "+val.Render(short(l, w-3)))
	}
	out = append(out, "")
	if m.confirm {
		out = append(out, " "+t.fg(t.warn).Bold(!t.plain).Render("stop serve? running agents keep going (y/n)"))
	} else {
		out = append(out, mut.Render(" esc/q stop "+t.g.dot+" smithy tui for the full dashboard"))
	}
	return out
}
