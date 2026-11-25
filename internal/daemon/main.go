//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/text"
)

// Usage is the text for `smithy daemon --help`.
const Usage = `usage: smithy daemon [--bg] [--port <n>] [--agent <cli>] [--once] [--max-hours <n>]
Runs the auto loop (the one smithy auto serve ran) and a local API on 127.0.0.1:<daemon.port>
(default 7477), writing every event to ~/.smithy/smithy.db. In a terminal it shows the serve view
(Esc, q or Ctrl+C asks to stop); stopping leaves the orchestrator and its workers running.
  --bg         start detached; prints "<pid> <url>", log in ~/.smithy/daemon.log
  --port       listen port (default: daemon.port); the address is always loopback
  --agent, --once, --max-hours   as smithy auto serve
API (every request needs "Authorization: Bearer <~/.smithy/daemon.token>", or through tailscale
serve a Tailscale-User-Login listed in daemon.allow; anything else gets 401):
  GET  /v1/state                 pid, last seq, pending decisions, the loop's heartbeat
  GET  /v1/events?since=<seq>    SSE of every event after seq; resumes with Last-Event-ID
  POST /v1/decisions             {"id","answer"}: the first answer wins (200); later ones get
                                 the recorded outcome (409)
Worker events (events.jsonl, the proxy log) are tailed into the store; proxy secret asks,
ready items and needs_decision items become decisions whose answers go back to the asker.
Stop a --bg daemon with: kill <pid> (~/.smithy/daemon.json shows it).`

type flags struct {
	bg   bool
	port int
}

func splitFlags(args []string) (flags, []string, error) {
	f := flags{port: -1}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--bg":
			f.bg = true
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
		return background(f.port, rest)
	}
	return foreground("smithy daemon", f.port, rest, view)
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
	return foreground("smithy auto serve", port, args, view)
}

func foreground(name string, port int, serveArgs []string, view auto.View) int {
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
	api.Route = api.RouteIntake(RouteBack)
	api.Resume()
	tctx, stopTails := context.WithCancel(context.Background())
	defer stopTails()
	api.Ctx = tctx
	go api.RunTails(tctx, time.Second, &Tail{Path: events.File(), Source: "tail:events"}, &Tail{Path: events.ProxyFile(), Source: ProxySource})
	conns, cerr := connect.FromConfig()
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "%s: chat connectors: %s\n", name, text.Clean(cerr.Error()))
	}
	if len(conns) > 0 {
		hub := &connect.Hub{Conns: conns, File: connect.File(), Kinds: connect.Kinds, Input: api.Input, Intake: api.Intake,
			Answer: func(id, answer, by string) error { _, err := api.Answer(id, answer, by); return err },
			Log:    func(l string) { api.Append("log", "", map[string]string{"line": l}) }}
		go api.RunNotes(tctx, hub, 30*time.Second)
		go RunReport(tctx, hub, time.Hour)
		go hub.Run(tctx)
	}
	h := auto.Hooks{
		Tap: func(l string) { api.Append("log", "", map[string]string{"line": l}) },
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
			go hs.Serve(ln)
			info := Info{PID: os.Getpid(), PIDStart: auto.ProcStart(os.Getpid()), Addr: ln.Addr().String(), Started: time.Now().UTC(), Log: os.Getenv("SMITHY_DAEMON_LOG")}
			if err := WriteInfo(info); err != nil {
				hs.Close()
				return nil, err
			}
			api.Append("daemon_up", "", map[string]any{"pid": info.PID, "addr": info.Addr})
			return func() {
				api.Append("daemon_down", "", map[string]any{"pid": info.PID})
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				hs.Shutdown(ctx)
				if cur, _ := ReadInfo(); cur != nil && cur.PID == info.PID {
					os.Remove(InfoFile())
				}
			}, nil
		},
	}
	return auto.ServeMainHooks(name, serveArgs, view, h)
}

func background(port int, serveArgs []string) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy daemon:", err)
		return 1
	}
	logPath := filepath.Join(ghx.Home(), "daemon.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "smithy daemon:", err)
		return 1
	}
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy daemon:", err)
		return 1
	}
	defer lf.Close()
	cmd := exec.Command(exe, append([]string{"daemon", "--port", strconv.Itoa(port)}, serveArgs...)...)
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.Env = append(os.Environ(), "SMITHY_DAEMON_LOG="+logPath)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "smithy daemon:", err)
		return 1
	}
	pid := cmd.Process.Pid
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	deadline := time.After(10 * time.Second)
	for {
		if i, _ := ReadInfo(); i != nil && i.PID == pid {
			fmt.Println(pid, i.URL())
			return 0
		}
		select {
		case <-exited:
			fmt.Fprintf(os.Stderr, "smithy daemon: exited before it listened; see %s\n", logPath)
			return 1
		case <-deadline:
			fmt.Fprintf(os.Stderr, "smithy daemon: pid %d did not listen within 10s; see %s\n", pid, logPath)
			return 1
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
