package auto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/notify"
	"github.com/Zovaris/smithy/internal/pool"
	"github.com/Zovaris/smithy/internal/usage"
)

// ServeUsage is the text for `smithy auto serve --help`.
const ServeUsage = `usage: smithy auto serve [--agent <cli>] [--once] [--max-hours <n>]
Runs the auto loop in the foreground with no tokens (in the harness build it starts smithy daemon,
which runs this loop, or attaches to it): polls auto.sources every auto.every seconds and,
when auto take yields a task dir, starts one headless orchestrator session (smithy agent --bg
--profile auto) that runs tend auto once on it. At most one orchestrator at a time; new issues stay
queued until it ends. Its output goes to ~/.smithy/auto/sessions/<time>.log.
  --agent      agent CLI for the orchestrator (default: auto.agent, else the detected one)
  --once       one cycle, then exit (a started orchestrator keeps running)
  --max-hours  exit after this many hours (default: no limit)
In a terminal it shows a live status view (Esc, q or Ctrl+C asks to stop); otherwise one line per
event. Stopping serve never stops the orchestrator or its workers. Heartbeat: ~/.smithy/auto/serve.json.`

// Heartbeat is ~/.smithy/auto/serve.json, rewritten by smithy auto serve every cycle.
type Heartbeat struct {
	PID        int       `json:"pid"`
	Started    time.Time `json:"started"`
	Agent      string    `json:"agent"`
	Every      int       `json:"every"`
	LastCycle  time.Time `json:"last_cycle"`
	State      string    `json:"state"` // idle, polling or orchestrating
	SessionLog string    `json:"session_log,omitempty"`
	SessionPID int       `json:"session_pid,omitempty"`
	PIDStart   string    `json:"pid_start,omitempty"`     // the process's start time, so a reused pid reads as dead
	SessionPS  string    `json:"session_start,omitempty"` // same, for the orchestrator
	SessionAt  time.Time `json:"session_at,omitzero"`
	SessionDir string    `json:"session_dir,omitempty"`
}

// ServeFile is the heartbeat's path.
func ServeFile() string { return filepath.Join(ghx.Home(), "auto", "serve.json") }

// SessionsDir holds each orchestrator's prompt and log.
func SessionsDir() string { return filepath.Join(ghx.Home(), "auto", "sessions") }

// ReadServe reads the heartbeat; nil when serve is not running.
func ReadServe() (*Heartbeat, error) {
	b, err := os.ReadFile(ServeFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var hb Heartbeat
	if err := json.Unmarshal(b, &hb); err != nil {
		return nil, fmt.Errorf("%s: %w", ServeFile(), err)
	}
	return &hb, nil
}

// Health is "running", "stale" (its process is gone, or no cycle within 3x auto.every) or
// "not running" (no heartbeat).
func (hb *Heartbeat) Health(now time.Time) string {
	if hb == nil {
		return "not running"
	}
	last := hb.LastCycle
	if last.IsZero() {
		last = hb.Started
	}
	every := max(hb.Every, 1)
	if !AliveAs(hb.PID, hb.PIDStart) || now.Sub(last) > time.Duration(3*every)*time.Second {
		return "stale"
	}
	return "running"
}

// SessionRunning reports whether the heartbeat's orchestrator process is still alive.
func (hb *Heartbeat) SessionRunning() bool {
	return hb != nil && hb.SessionPID > 0 && AliveAs(hb.SessionPID, hb.SessionPS)
}

// procStart is the start time ps reports for pid ("" when unknown, as on Windows).
var procStart = func(pid int) string {
	if runtime.GOOS == "windows" || pid <= 0 {
		return ""
	}
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(string(out)), " ")
}

// ProcStart is the start time ps reports for pid ("" when unknown).
func ProcStart(pid int) string { return procStart(pid) }

// AliveAs reports whether pid is alive and, when start is known, is still the process that
// started then (a reused pid reads as dead).
func AliveAs(pid int, start string) bool {
	if !Alive(pid) {
		return false
	}
	if start == "" {
		return true
	}
	now := procStart(pid)
	return now == "" || now == start
}

// Alive reports whether a process with this pid exists.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

// ServeOptions are the flags of one serve run.
type ServeOptions struct {
	Agent    string
	Once     bool
	MaxHours float64
	Cwd      string
}

// selfExe is the smithy binary serve runs `smithy agent` with; tests replace it.
var selfExe = os.Executable

// DefaultAgent is auto.agent, else the CLI smithy agent would pick ("" when none).
func DefaultAgent() string {
	if a := config.Get("auto.agent", ""); a != "" {
		return a
	}
	return agents.ResolveCLI(os.Getenv, func(b string) bool { _, err := exec.LookPath(b); return err == nil })
}

// SessionHours is auto.session_hours: how long one orchestrator may run before smithy agent ends it.
func SessionHours() float64 {
	f, err := strconv.ParseFloat(config.Get("auto.session_hours", "3"), 64)
	if err != nil || f <= 0 {
		return 3
	}
	return f
}

// Prompt is the orchestrator's fixed prompt for one task dir.
func Prompt(dir string, hours float64) string {
	tend := "the tend skill's SKILL.md"
	if p, ok := agents.FindSkill("tend"); ok {
		tend = p
	}
	return fmt.Sprintf(`Read %s and follow it for `+"`tend auto once %s`"+`: start warden on that dir (its batch values are pre-answered; this run is the user's yes), launching workers with `+"`smithy agent`"+` (separate processes that outlive you). Report each issue with `+"`smithy auto report`"+` as its card moves. End when every item is ready, merged or blocked, or after %s hours; then print one line per issue and done:.
`, tend, dir, strconv.FormatFloat(hours, 'g', -1, 64))
}

// Server is one smithy auto serve run. Log gets one line per event.
type Server struct {
	Opt    ServeOptions
	Log    func(string)
	logMu  sync.Mutex // an abandoned cycle and stopped() may log at the same time
	beatMu sync.Mutex // guards ended: an abandoned cycle must not rewrite the heartbeat after Run removed it
	ended  bool
	hb     Heartbeat
	mkdr   func(string) (string, error)
	note   *notify.Notifier
	capDay string // local day a cap_reached event was last recorded (issue cap or spend cap)
	// limitedAt is the reset time a "limited" event was last recorded for (smithy pool).
	limitedAt time.Time
}

// NewServer checks no other serve is running and writes the first heartbeat.
func NewServer(o ServeOptions, log func(string)) (*Server, error) {
	if o.Agent == "" {
		o.Agent = DefaultAgent()
	}
	if o.Agent == "" {
		return nil, agents.ErrNoCLI
	}
	if o.Cwd == "" {
		o.Cwd, _ = os.Getwd()
	}
	err := acquire(ServeFile(), func(b []byte) bool {
		var prev Heartbeat
		return json.Unmarshal(b, &prev) == nil && prev.PID != os.Getpid() && AliveAs(prev.PID, prev.PIDStart)
	})
	if errors.Is(err, errHeld) {
		prev, _ := ReadServe()
		pid := 0
		if prev != nil {
			pid = prev.PID
		}
		return nil, fmt.Errorf("serve already running (pid %d); stop it first", pid)
	}
	if err != nil {
		return nil, err
	}
	s, _ := LoadSettings()
	sv := &Server{Opt: o, Log: log, mkdr: taskDir, note: notify.New(log),
		hb: Heartbeat{PID: os.Getpid(), PIDStart: procStart(os.Getpid()), Started: now(), Agent: o.Agent, Every: s.Every, State: "idle"}}
	sv.adopt()
	return sv, sv.beat()
}

// Session is ~/.smithy/auto/orchestrator.json, the lock that keeps one orchestrator at a time; it
// outlives serve, so a restarted serve never starts a second one.
type Session struct {
	PID   int       `json:"pid"`
	Log   string    `json:"log"`
	Dir   string    `json:"dir"`
	At    time.Time `json:"at"`
	Start string    `json:"start,omitempty"`
}

var errHeld = errors.New("held")

// acquire creates path exclusively (O_EXCL). When it exists, held decides from its content
// whether its owner is alive: errHeld if so, else the stale file is replaced once.
func acquire(path string, held func([]byte) bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	for try := 0; ; try++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.WriteString("{}\n")
			return f.Close()
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		b, _ := os.ReadFile(path)
		if len(strings.TrimSpace(string(b))) > 2 && held(b) || try > 0 {
			return errHeld
		}
		os.Remove(path)
	}
}

// writeAtomic replaces path through a temp file and a rename.
func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LockFile is the orchestrator lock's path.
func LockFile() string { return filepath.Join(ghx.Home(), "auto", "orchestrator.json") }

// ReadLock reads the orchestrator lock; nil when there is none.
func ReadLock() *Session {
	b, err := os.ReadFile(LockFile())
	if err != nil {
		return nil
	}
	var s Session
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return &s
}

// adopt copies a recorded orchestrator into the heartbeat.
func (sv *Server) adopt() {
	if l := ReadLock(); l != nil {
		sv.hb.SessionPID, sv.hb.SessionLog, sv.hb.SessionDir, sv.hb.SessionAt, sv.hb.SessionPS = l.PID, l.Log, l.Dir, l.At, l.Start
	}
}

func (sv *Server) release() {
	sv.beatMu.Lock()
	sv.ended = true
	os.Remove(ServeFile())
	sv.beatMu.Unlock()
}

// Heartbeat is the current heartbeat.
func (sv *Server) Heartbeat() Heartbeat { return sv.hb }

func (sv *Server) beat() error {
	sv.beatMu.Lock()
	defer sv.beatMu.Unlock()
	if sv.ended {
		return nil
	}
	b, _ := json.MarshalIndent(sv.hb, "", "  ")
	f := ServeFile()
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	return writeAtomic(f, append(b, '\n'))
}

func (sv *Server) logf(format string, a ...any) {
	if sv.Log != nil {
		sv.logMu.Lock()
		defer sv.logMu.Unlock()
		sv.Log(Clean(fmt.Sprintf(format, a...)))
	}
}

// Run loops until ctx ends, --once's cycle is done or --max-hours passes, then removes the
// heartbeat. A running orchestrator is left alone.
func (sv *Server) Run(ctx context.Context) error {
	defer func() {
		sv.beatMu.Lock()
		sv.ended = true
		os.Remove(ServeFile())
		sv.beatMu.Unlock()
	}()
	var deadline <-chan time.Time
	if sv.Opt.MaxHours > 0 {
		t := time.NewTimer(time.Duration(sv.Opt.MaxHours * float64(time.Hour)))
		defer t.Stop()
		deadline = t.C
	}
	sv.logf("serve started: pid %d, agent %s", sv.hb.PID, sv.hb.Agent)
	for {
		res := make(chan time.Duration, 1)
		go func() { res <- sv.Cycle() }()
		var every time.Duration
		select {
		case every = <-res:
		case <-ctx.Done():
			sv.stopped()
			return nil
		case <-deadline:
			sv.logf("serve: --max-hours reached")
			sv.stopped()
			return nil
		}
		if sv.Opt.Once {
			sv.logf("serve: --once done")
			return nil
		}
		select {
		case <-ctx.Done():
			sv.stopped()
			return nil
		case <-deadline:
			sv.logf("serve: --max-hours reached")
			sv.stopped()
			return nil
		case <-time.After(every):
		}
	}
}

func (sv *Server) stopped() {
	msg := "serve stopped"
	if sv.hb.SessionRunning() {
		msg += fmt.Sprintf("; orchestrator pid %d keeps running (log %s; smithy tui or smithy cards board shows its workers)", sv.hb.SessionPID, sv.hb.SessionLog)
	}
	sv.logf("%s", msg)
	events.Append(events.Event{Kind: "serve_down", Detail: "serve stopped"})
	if sv.note != nil {
		sv.note.Final(now())
	}
}

// Cycle polls once and, when no orchestrator is running, takes the queue into a task dir and
// starts one. It returns the wait before the next cycle.
func (sv *Server) Cycle() time.Duration {
	s, err := LoadSettings()
	if err != nil {
		sv.logf("serve: settings: %v", err)
		sv.hb.LastCycle, sv.hb.State = now(), "idle"
		_ = sv.beat()
		return 60 * time.Second
	}
	sv.hb.Every, sv.hb.State = s.Every, "polling"
	_ = sv.beat()
	srcs, openErr := s.Open("")
	if openErr != nil {
		sv.logf("serve: %v", openErr)
	}
	if len(srcs) > 0 {
		got, err := Poll(s, srcs, logWriter{sv})
		for _, q := range got {
			sv.logf("%s", q.Line())
		}
		if err != nil {
			sv.logf("serve: poll: %v", err)
		}
	}
	sv.hb.LastCycle = now()
	sv.adopt()
	switch {
	case sv.hb.SessionRunning():
		sv.hb.State = "orchestrating"
	case s.Paused:
		sv.hb.State = "idle"
		sv.logf("serve: paused (auto.paused); nothing launches")
	default:
		sv.hb.State = "idle"
		if sv.hb.SessionPID > 0 {
			sv.logf("orchestrator pid %d ended (log %s)", sv.hb.SessionPID, sv.hb.SessionLog)
			n, gaveUp, err := Requeue(sv.hb.SessionDir)
			switch {
			case err != nil:
				sv.logf("serve: requeue: %v", err)
			case n > 0:
				sv.logf("requeued %d issue(s): orchestrator started nothing (log %s)", n, sv.hb.SessionLog)
			}
			for _, id := range gaveUp {
				sv.logf("giving up on %s: orchestrators started nothing %d times; it stays launched", id, MaxRequeues+1)
				events.Append(events.Event{Kind: "orchestrator_failed", ID: id, Detail: "orchestrators started nothing; gave up"})
			}
			sv.hb.SessionPID, sv.hb.SessionDir = 0, ""
		}
		if sv.overCap(s) || sv.poolHeld() {
			break
		}
		if err := acquire(LockFile(), func(b []byte) bool {
			var l Session
			return json.Unmarshal(b, &l) == nil && AliveAs(l.PID, l.Start)
		}); err != nil {
			sv.logf("serve: orchestrator lock: %v", err)
			break
		}
		before, _ := os.ReadFile(StateFile())
		r, err := Take(s, 0, sv.mkdr)
		switch {
		case err != nil:
			os.Remove(LockFile())
			sv.logf("serve: take: %v", err)
		case r.Count == 0:
			os.Remove(LockFile())
			if r.Why != "empty queue" {
				sv.logf("auto none: %s", r.Why)
			}
			if day := now().Format("2006-01-02"); strings.HasPrefix(r.Why, "daily cap") && sv.capDay != day {
				sv.capDay = day
				events.Append(events.Event{Kind: "cap_reached", Detail: r.Why})
			}
		default:
			if err := sv.launch(r.Dir); err != nil {
				os.Remove(LockFile())
				if before != nil {
					_ = writeAtomic(StateFile(), before)
				}
				os.RemoveAll(r.Dir)
				sv.logf("serve: orchestrator did not start, %d issue(s) back in the queue: %v", r.Count, err)
				events.Append(events.Event{Kind: "orchestrator_failed", Detail: fmt.Sprintf("did not start, %d issue(s) back in the queue", r.Count)})
			} else {
				sv.hb.State = "orchestrating"
				sv.logf("orchestrator started: pid %d, %d issue(s), %s, log %s", sv.hb.SessionPID, r.Count, r.Dir, sv.hb.SessionLog)
			}
		}
	}
	_ = sv.beat()
	if sv.note != nil {
		go sv.note.Flush(now())
	}
	return time.Duration(s.Every) * time.Second
}

// overCap reports whether today's reported spend is at or over auto.max_cost_per_day; it logs
// each time and records one cap_reached event per day.
func (sv *Server) overCap(s Settings) bool {
	if s.MaxCostPerDay == nil {
		return false
	}
	t := now()
	es, err := events.Read(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()), 0)
	if err != nil {
		sv.logf("serve: spend: %v", err)
	}
	spent := events.Spend(es, t, 1)[0]
	if spent < *s.MaxCostPerDay {
		return false
	}
	msg := fmt.Sprintf("daily spend cap %s reached (%s today)", usage.Cost(s.MaxCostPerDay), usage.Cost(&spent))
	sv.logf("serve: %s; nothing launches until tomorrow", msg)
	if d := t.Format("2006-01-02"); sv.capDay != d {
		sv.capDay = d
		_ = events.Append(events.Event{Kind: "cap_reached", Detail: msg})
	}
	return true
}

// poolHeld reports whether every pool account serve's agent could use is limited; it logs the
// earliest reset each time and records one "limited" event per reset time.
func (sv *Server) poolHeld() bool {
	h := agents.PoolHarness(sv.hb.Agent)
	if h == "" {
		return false
	}
	until, all := pool.Earliest(h, now())
	if !all {
		return false
	}
	msg := "every pool account is limited; earliest reset " + until.Local().Format("2006-01-02 15:04")
	sv.logf("serve: %s; nothing launches until then", msg)
	if !sv.limitedAt.Equal(until) {
		sv.limitedAt = until
		_ = events.Append(events.Event{Kind: "limited", Detail: msg})
	}
	return true
}

type logWriter struct{ sv *Server }

func (w logWriter) Write(p []byte) (int, error) {
	for _, l := range strings.Split(strings.TrimSpace(string(p)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			w.sv.logf("%s", l)
		}
	}
	return len(p), nil
}

var _ io.Writer = logWriter{}

// launch starts the orchestrator for dir with smithy agent --bg --profile auto.
func (sv *Server) launch(dir string) error {
	exe, err := selfExe()
	if err != nil {
		return err
	}
	if err := sandboxReady(sv.hb.Agent); err != nil {
		return err
	}
	if err := os.MkdirAll(SessionsDir(), 0o700); err != nil {
		return err
	}
	ts := now().Format("20060102-150405")
	prompt := filepath.Join(SessionsDir(), ts+".prompt.md")
	log := filepath.Join(SessionsDir(), ts+".log")
	hours := SessionHours()
	if err := os.WriteFile(prompt, []byte(Prompt(dir, hours)), 0o600); err != nil {
		return err
	}
	cmd := exec.Command(exe, "agent", "--bg", "--profile", "auto", "--timeout", strconv.FormatFloat(hours*60, 'f', -1, 64),
		"--cwd", sv.Opt.Cwd, "--log", log, prompt)
	cmd.Dir = sv.Opt.Cwd
	env := append(agents.Scrub(os.Environ()), agents.SandboxRoleEnv+"=orchestrator")
	if sv.hb.Agent != "custom" {
		env = append(env, "AGENT_CLI="+sv.hb.Agent)
	}
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return err
	}
	f := strings.SplitN(strings.TrimSpace(string(out)), " ", 2)
	if len(f) < 2 {
		return fmt.Errorf("smithy agent printed %q, not <pid> <log>", strings.TrimSpace(string(out)))
	}
	pid, err := strconv.Atoi(f[0])
	if err != nil {
		return fmt.Errorf("smithy agent printed %q, not <pid> <log>", strings.TrimSpace(string(out)))
	}
	sv.hb.SessionPID, sv.hb.SessionLog, sv.hb.SessionAt, sv.hb.SessionDir, sv.hb.SessionPS = pid, f[1], now(), dir, procStart(pid)
	b, _ := json.Marshal(Session{PID: pid, Log: f[1], Dir: dir, At: sv.hb.SessionAt, Start: sv.hb.SessionPS})
	return writeAtomic(LockFile(), append(b, '\n'))
}

// sandboxReady fails closed before an orchestrator starts when auto.sandbox can't sandbox the
// workers it would launch with agent.
var sandboxReady = defaultSandboxReady

func defaultSandboxReady(agent string) error {
	mode, err := agents.SandboxMode()
	if err == nil {
		err = agents.SandboxCheckAgent(mode, agent, agents.HostProbe())
	}
	if err != nil {
		return fmt.Errorf("auto.sandbox: %v; nothing launches until it is fixed (smithy doctor)", err)
	}
	return nil
}

// View draws the live serve view until done closes or the user stops serve; logs carries each
// event line. smithy wires tui.RunServeView here (charm modules stay in internal/tui).
type View func(done <-chan struct{}, sv *Server, logs <-chan string) error

// Hooks let smithy daemon wrap the serve loop: Tap sees every event line; Started runs once the
// loop holds its lock and returns the function that undoes it when serve ends.
type Hooks struct {
	Tap     func(line string)
	Started func(sv *Server) (stop func(), err error)
}

// ServeMain runs `smithy auto serve`. view is used when stdout is a terminal; otherwise (or with
// a nil view) each event is one plain line.
func ServeMain(args []string, view View) int {
	return ServeMainHooks("smithy auto serve", args, view, Hooks{})
}

// ServeMainHooks is ServeMain under another command name, with hooks.
func ServeMainHooks(name string, args []string, view View, h Hooks) int {
	var o ServeOptions
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "-h", "--help":
			fmt.Println(ServeUsage)
			return 0
		case "--once":
			o.Once = true
		case "--agent", "--max-hours":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, name+": %s needs a value\n", a)
				return 2
			}
			i++
			if a == "--agent" {
				o.Agent = args[i]
				continue
			}
			f, err := strconv.ParseFloat(args[i], 64)
			if err != nil || f <= 0 {
				fmt.Fprintln(os.Stderr, name+": --max-hours takes a number > 0")
				return 2
			}
			o.MaxHours = f
		default:
			fmt.Fprintf(os.Stderr, name+": unexpected argument %s\n%s\n", a, ServeUsage)
			return 2
		}
	}
	if _, err := LoadSettings(); err != nil {
		fmt.Fprintln(os.Stderr, name+":", err)
		return 2
	}
	tty := view != nil && isTerminal(os.Stdout)
	logs := make(chan string, 64)
	var mu sync.Mutex
	var last string
	log := func(l string) {
		if h.Tap != nil {
			h.Tap(l)
		}
		mu.Lock()
		last = l
		mu.Unlock()
		if !tty {
			fmt.Println(now().Format("2006-01-02 15:04:05"), l)
			return
		}
		select {
		case logs <- l:
		default:
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	sv, err := NewServer(o, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, name+":", Clean(err.Error()))
		if errors.Is(err, agents.ErrNoCLI) {
			return 2
		}
		return 1
	}
	if h.Started != nil {
		stop, err := h.Started(sv)
		if err != nil {
			sv.release()
			fmt.Fprintln(os.Stderr, name+":", Clean(err.Error()))
			return 1
		}
		defer stop()
	}
	done := make(chan struct{})
	go func() {
		_ = sv.Run(ctx)
		close(done)
	}()
	if tty {
		if err := view(done, sv, logs); err != nil {
			fmt.Fprintln(os.Stderr, name+": view:", err)
		}
		cancel()
	}
	<-done
	if tty {
		mu.Lock()
		fmt.Println(last)
		mu.Unlock()
	}
	return 0
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
