//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/connect"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/remote"
	"github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/text"
)

// Usage is the text for `smithy daemon --help`.
const Usage = `usage: smithy daemon [--bg] [--port <n>] [--idle-exit <d>] [--demo] [--agent <cli>] [--once] [--max-hours <n>]
Runs the auto loop (the one smithy auto serve ran) and a local API on 127.0.0.1:<daemon.port>
(default 7477), writing every event to ~/.smithy/smithy.db. In a terminal it shows the serve view
(Esc, q or Ctrl+C asks to stop); stopping leaves the orchestrator and its workers running. This
foreground form is for hosts (systemd, a server); on a laptop bare smithy starts the daemon itself.
  --bg         start detached; prints "<pid> <url>", log in ~/.smithy/daemon.log; exits on its own
               after daemon.idle_exit (default 30m) with no clients and no running work
  --idle-exit  exit after this long idle (a duration like 30m; 0 never, the foreground default)
  --port       listen port (default: daemon.port); the address is always loopback
  --demo       every session runs a scripted agent (tool calls, one decision): no CLI or key needed
  --agent, --once, --max-hours   as smithy auto serve
API (every request needs "Authorization: Bearer <~/.smithy/daemon.token>", or through tailscale
serve a Tailscale-User-Login listed in daemon.allow; anything else gets 401):
  GET  /v1/state                 pid, last seq, pending decisions, the loop's heartbeat
  GET  /v1/events?since=<seq>    SSE of every event after seq; resumes with Last-Event-ID
  POST /v1/decisions             {"id","answer"}: the first answer wins (200); later ones get
                                 the recorded outcome (409)
  GET  /v1/guard                 the command and fetch guard lists and the suggested fetch allowlist
  POST /v1/guard/suggested       add every suggested host to guard.fetch_allow
  POST /v1/sessions              {"agent","dir","message"}: start a session (agent: a CLI name,
                                 acp:<cli> or api:<provider>/<model>; "" the last used); 201
  GET  /v1/sessions              every session with status working, needs_you, done or failed;
                                 ?dir= keeps the ones started in that folder or below
  POST /v1/sessions/{id}/messages  {"text"}: the next user message (queued behind a turn); 202
  POST /v1/sessions/{id}/stop    cancel the turn; a later message goes on with the history
Session events (session_* kinds, decision_asked for a permission) carry the session id on
/v1/events. api: agents run in the daemon, so tool calls stream live and a call the auto rules
refuse becomes a decision (perm:<id>/<n>) until someone answers; other agents run smithy agent
once per turn. A reply in a session's chat-connector thread is that session's next message.
Worker events (events.jsonl, the proxy log) are tailed into the store; proxy secret asks,
ready items and needs_decision items become decisions whose answers go back to the asker.
Stop a background daemon with: smithy stop.`

type flags struct {
	bg   bool
	port int
	idle time.Duration
	// idleSet is true when --idle-exit was given.
	idleSet bool
	// demo runs every session on DemoRunner.
	demo bool
}

// Version is this binary's version, reported in GET /v1/state.
var Version = "dev"

func splitFlags(args []string) (flags, []string, error) {
	f := flags{port: -1}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--bg":
			f.bg = true
		case "--demo":
			f.demo = true
		case "--port":
			if i+1 >= len(args) {
				return f, nil, errors.New("--port needs a value")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 || n > 65535 {
				return f, nil, errors.New("--port takes a port number")
			}
			f.port = n
		case "--idle-exit":
			if i+1 >= len(args) {
				return f, nil, errors.New("--idle-exit needs a value")
			}
			i++
			d, err := ParseIdle(args[i])
			if err != nil {
				return f, nil, errors.New("--idle-exit takes a duration like 30m, or 0")
			}
			f.idle, f.idleSet = d, true
		default:
			rest = append(rest, a)
		}
	}
	return f, rest, nil
}

// Running is the live daemon's info, or nil when none runs.
func Running() *Info {
	i, err := ReadInfo()
	if err != nil || i == nil || !auto.AliveAs(i.PID, i.PIDStart) {
		return nil
	}
	return i
}

// Main runs `smithy daemon`.
func Main(args []string, view auto.View) int {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println(Usage)
			return 0
		}
	}
	f, rest, err := splitFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy daemon:", err)
		return 2
	}
	if i := Running(); i != nil {
		fmt.Fprintf(os.Stderr, "smithy daemon: already running (pid %d, %s)\n", i.PID, i.URL())
		return 1
	}
	if f.port < 0 {
		f.port, err = strconv.Atoi(config.Get("daemon.port", strconv.Itoa(DefaultPort)))
		if err != nil || f.port < 0 || f.port > 65535 {
			fmt.Fprintln(os.Stderr, "smithy daemon: daemon.port takes a port number")
			return 2
		}
	}
	if f.bg {
		idle := f.idle
		if !f.idleSet {
			if idle, err = ConfigIdle(); err != nil {
				fmt.Fprintln(os.Stderr, "smithy daemon:", err)
				return 2
			}
		}
		bgArgs := rest
		if f.demo {
			bgArgs = append([]string{"--demo"}, rest...)
		}
		i, err := StartBackground(f.port, idle, bgArgs)
		if err != nil {
			fmt.Fprintln(os.Stderr, "smithy daemon:", err)
			return 1
		}
		fmt.Println(i.PID, i.URL())
		return 0
	}
	return foreground(runOpts{name: "smithy daemon", port: f.port, args: rest, view: view, idle: f.idle, writeInfo: true, demo: f.demo})
}

// ConfigIdle is daemon.idle_exit (default 30m).
func ConfigIdle() (time.Duration, error) {
	return ParseIdle(config.Get("daemon.idle_exit", DefaultIdleExit))
}

// ConfigPort is daemon.port (default 7477).
func ConfigPort() (int, error) {
	port, err := strconv.Atoi(config.Get("daemon.port", strconv.Itoa(DefaultPort)))
	if err != nil || port < 0 || port > 65535 {
		return 0, errors.New("daemon.port takes a port number")
	}
	return port, nil
}

// Standalone runs a private daemon inside this process on a free loopback port until ctx ends:
// no daemon.json (other clients never find it) and no log lines on stdout. ready gets its
// address once it listens; the return is serve's exit code.
func Standalone(ctx context.Context, ready func(Info)) int {
	return foreground(runOpts{name: "smithy --standalone", port: 0, ctx: ctx, quiet: true, ready: ready})
}

// ServeOrAttach is `smithy auto serve` in the harness build: it attaches to a running daemon,
// else runs one in the foreground with the same flags.
func ServeOrAttach(args []string, view auto.View) int {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println(auto.ServeUsage)
			fmt.Println("Harness build: serve runs inside smithy daemon (local API, events in ~/.smithy/smithy.db); with a daemon running, it attaches and prints its events (Ctrl+C detaches).")
			return 0
		}
	}
	if i := Running(); i != nil {
		return attach(i)
	}
	port, err := strconv.Atoi(config.Get("daemon.port", strconv.Itoa(DefaultPort)))
	if err != nil || port < 0 || port > 65535 {
		fmt.Fprintln(os.Stderr, "smithy auto serve: daemon.port takes a port number")
		return 2
	}
	return foreground(runOpts{name: "smithy auto serve", port: port, args: args, view: view, writeInfo: true})
}

type runOpts struct {
	name  string
	port  int
	args  []string
	view  auto.View
	idle  time.Duration // 0: never exit when idle
	ctx   context.Context
	quiet bool
	// writeInfo publishes ~/.smithy/daemon.json so bare smithy, chat and web find this daemon.
	writeInfo bool
	ready     func(Info)
	demo      bool
}

func foreground(o runOpts) int {
	name, port := o.name, o.port
	st, err := store.Open(store.DefaultPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: store: %v\n", name, err)
		return 1
	}
	defer st.Close()
	tok, err := LoadToken(TokenFile(), true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: token: %v\n", name, err)
		return 1
	}
	api, err := New(st, tok, AllowList(config.Get("daemon.allow", "")), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		return 1
	}
	api.Version = Version
	if o.demo {
		api.Runners = DemoRunner
	}
	api.Pair = &Pairing{File: DevicesFile()}
	api.ServersFile = remote.File()
	api.Route = api.RouteIntake(RouteBack)
	_ = api.Resume()
	tctx, stopTails := context.WithCancel(context.Background())
	defer stopTails()
	api.Ctx = tctx
	go api.RunTails(tctx, time.Second, &Tail{Path: events.File(), Source: "tail:events"}, &Tail{Path: events.ProxyFile(), Source: ProxySource})
	api.Tend = NewTender()
	api.Tend.Sources = func() []TendRef { return mergeRefs(TendSources(), api.SessionTendRefs()) }
	go api.Tend.Run(tctx, 2*time.Minute)
	conns, cerr := connect.FromConfig()
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "%s: chat connectors: %s\n", name, text.Clean(cerr.Error()))
	}
	if len(conns) > 0 {
		hub := &connect.Hub{Conns: conns, File: connect.File(), Kinds: connect.Kinds, Input: api.Input, Intake: api.Intake,
			Answer: func(id, answer, by string) error { _, err := api.Answer(id, answer, by); return err },
			Log:    func(l string) { _, _ = api.Append("log", "", map[string]string{"line": l}) }}
		go api.RunNotes(tctx, hub, 30*time.Second)
		go RunReport(tctx, hub, time.Hour)
		go hub.Run(tctx)
	}
	parent := o.ctx
	if parent == nil {
		parent = context.Background()
	}
	life, quit := context.WithCancel(parent)
	defer quit()
	h := auto.Hooks{
		Ctx:   life,
		Quiet: o.quiet,
		Tap:   func(l string) { _, _ = api.Append("log", "", map[string]string{"line": l}) },
		Started: func(sv *auto.Server) (func(), error) {
			if len(conns) > 0 {
				sv.StopNotify()
			}
			ln, err := Listen(port)
			if err != nil {
				return nil, fmt.Errorf("listen on 127.0.0.1:%d: %w", port, err)
			}
			api.ServeState = func() any { return sv.Heartbeat() }
			hs := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second}
			go func() { _ = hs.Serve(ln) }()
			var rs *http.Server
			if addr := config.Get("daemon.listen", ""); addr != "" && o.writeInfo {
				if err := CheckRemoteListen(addr); err != nil {
					hs.Close()
					return nil, err
				}
				rln, err := net.Listen("tcp", addr)
				if err != nil {
					hs.Close()
					return nil, fmt.Errorf("daemon.listen %s: %w", addr, err)
				}
				rs = &http.Server{Handler: api.RemoteHandler(), ReadHeaderTimeout: 10 * time.Second}
				go func() { _ = rs.Serve(rln) }()
				_, _ = api.Append("log", "", map[string]string{"line": "daemon: also listening on " + rln.Addr().String() + " (bearer token or paired device only)"})
			}
			info := Info{PID: os.Getpid(), PIDStart: auto.ProcStart(os.Getpid()), Addr: ln.Addr().String(), Started: time.Now().UTC(), Log: os.Getenv("SMITHY_DAEMON_LOG")}
			if o.writeInfo {
				if err := WriteInfo(info); err != nil {
					hs.Close()
					return nil, err
				}
			}
			_, _ = api.Append("daemon_up", "", map[string]any{"pid": info.PID, "addr": info.Addr})
			if o.idle > 0 {
				busy := func() bool { hb := sv.Heartbeat(); return hb.SessionRunning() }
				go api.WatchIdle(life, o.idle, 15*time.Second, busy, func() {
					_, _ = api.Append("log", "", map[string]string{"line": fmt.Sprintf("daemon: idle for %s with no clients and no running work; exiting", o.idle)})
					quit()
				})
			}
			if o.ready != nil {
				o.ready(info)
			}
			return func() {
				_, _ = api.Append("daemon_down", "", map[string]any{"pid": info.PID})
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = hs.Shutdown(ctx)
				if rs != nil {
					_ = rs.Shutdown(ctx)
				}
				if !o.writeInfo {
					return
				}
				if cur, _ := ReadInfo(); cur != nil && cur.PID == info.PID {
					os.Remove(InfoFile())
				}
			}, nil
		},
	}
	return auto.ServeMainHooks(name, o.args, o.view, h)
}

// StartBackground starts `smithy daemon` detached (log in ~/.smithy/daemon.log) and returns its
// info once it listens. idle is its --idle-exit.
func StartBackground(port int, idle time.Duration, serveArgs []string) (*Info, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(layout.Home(), "daemon.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return nil, err
	}
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer lf.Close()
	args := append([]string{"daemon", "--port", strconv.Itoa(port), "--idle-exit", idle.String()}, serveArgs...)
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.Env = append(os.Environ(), "SMITHY_DAEMON_LOG="+logPath)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pid := cmd.Process.Pid
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	deadline := time.After(10 * time.Second)
	for {
		if i, _ := ReadInfo(); i != nil && i.PID == pid {
			return i, nil
		}
		select {
		case <-exited:
			return nil, fmt.Errorf("the daemon exited before it listened; see %s", logPath)
		case <-deadline:
			return nil, fmt.Errorf("daemon pid %d did not listen within 10s; see %s", pid, logPath)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func attach(i *Info) int {
	tok, err := LoadToken(TokenFile(), false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy auto serve: token:", err)
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	since, err := lastSeq(ctx, i.URL(), tok)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy auto serve:", err)
		return 1
	}
	fmt.Printf("attached to smithy daemon (pid %d, %s); Ctrl+C detaches, the daemon keeps running\n", i.PID, i.URL())
	if err := follow(ctx, i.URL(), tok, since, os.Stdout); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "smithy auto serve:", err)
		return 1
	}
	return 0
}

func lastSeq(ctx context.Context, base, tok string) (int64, error) {
	r, _ := http.NewRequestWithContext(ctx, "GET", base+"/v1/state", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return 0, fmt.Errorf("daemon state: %s", res.Status)
	}
	var st State
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		return 0, err
	}
	return st.LastSeq, nil
}

func follow(ctx context.Context, base, tok string, since int64, w io.Writer) error {
	ch := make(chan ClientMsg, 64)
	go (&Client{Base: base, Token: tok}).Follow(ctx, since, ch)
	for {
		select {
		case <-ctx.Done():
			return nil
		case m := <-ch:
			if m.Err != nil {
				fmt.Fprintln(w, "smithy auto serve: lost the daemon, retrying:", m.Err)
			}
			if m.Event == nil || m.Event.Kind != "log" {
				continue
			}
			var d struct {
				Line string `json:"line"`
			}
			if json.Unmarshal(m.Event.Data, &d) == nil {
				fmt.Fprintln(w, m.Event.Time.Local().Format("2006-01-02 15:04:05"), auto.Clean(d.Line))
			}
		}
	}
}
