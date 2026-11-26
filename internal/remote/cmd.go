package remote

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Usage is the text for `smithy servers --help`.
const Usage = `usage: smithy servers [list | add <name> <url> [--code <code>] | rm <name> | use <name|local> | discover]
The smithy daemons this machine talks to: bare smithy, smithy chat and the web switcher use the
default one (SMITHY_SERVER=<name|url>, or smithy chat's server flag, overrides it for one run).
  list       every saved server with its status (online, version, ready agents); * marks the default
  add        save a server; --code trades a one-time code from smithy pair on that machine for a
             device token (a tailscale serve URL needs none: daemon.allow decides there)
  rm         forget a server
  use        make it the default; local goes back to this machine
  discover   the Tailscale peers running smithy (tailscale status, a 2s probe each, cached 5m)
Saved in ~/.smithy/servers.json (0600: it holds device tokens).`

// Env is what Cmd reads and writes; zero fields take the real ones.
type Env struct {
	File     string
	Out, Err io.Writer
	Host     func() string
	Discover func(context.Context) ([]Found, error)
}

// TailscaleStatus runs `tailscale status --json`.
func TailscaleStatus(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
}

// Cmd runs `smithy servers`.
func Cmd(args []string, e Env) int {
	if e.File == "" {
		e.File = File()
	}
	if e.Out == nil {
		e.Out = os.Stdout
	}
	if e.Err == nil {
		e.Err = os.Stderr
	}
	if e.Host == nil {
		e.Host = func() string { h, _ := os.Hostname(); return h }
	}
	if e.Discover == nil {
		e.Discover = Discoverer{Status: TailscaleStatus}.Discover
	}
	fail := func(err error) int { fmt.Fprintln(e.Err, "smithy servers:", err); return 1 }
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	ctx := context.Background()
	switch sub {
	case "-h", "--help", "help":
		fmt.Fprintln(e.Out, Usage)
	case "list":
		l, err := Load(e.File)
		if err != nil {
			return fail(err)
		}
		mark := func(n string) string {
			if n == l.Default || (n == Local && l.Default == "") {
				return "* "
			}
			return "  "
		}
		fmt.Fprintf(e.Out, "%s%-12s this machine\n", mark(Local), Local)
		for _, st := range ProbeAll(ctx, l.Servers, 2*time.Second) {
			fmt.Fprintf(e.Out, "%s%-12s %s  %s\n", mark(st.Server.Name), st.Server.Name, st.Server.URL, StatusLine(st))
		}
	case "add":
		var pos []string
		code := ""
		for i := 0; i < len(args); i++ {
			if args[i] == "--code" && i+1 < len(args) {
				code, i = args[i+1], i+1
				continue
			}
			pos = append(pos, args[i])
		}
		if len(pos) != 2 {
			fmt.Fprintln(e.Err, "smithy servers add <name> <url> [--code <code>]")
			return 2
		}
		s := Server{Name: pos[0], URL: pos[1]}
		u, err := CleanURL(s.URL)
		if err != nil {
			return fail(err)
		}
		if code != "" {
			if !PrivateURL(u) {
				fmt.Fprintln(e.Err, "smithy servers: warning: "+u+" is plain http off loopback and Tailscale; the device token crosses the network unencrypted (a smithy daemon listens directly only on its Tailscale IP; use that, or an https URL such as tailscale serve)")
			}
			tok, err := Pair(ctx, u, code, e.Host())
			if err != nil {
				return fail(err)
			}
			s.Token = tok
		}
		if err := Add(e.File, s); err != nil {
			return fail(err)
		}
		what := "saved"
		if code != "" {
			what = "paired and saved"
		}
		fmt.Fprintf(e.Out, "%s %s (%s); smithy servers use %s makes it the default\n", what, s.Name, u, strings.ToLower(s.Name))
	case "rm":
		if len(args) != 1 {
			fmt.Fprintln(e.Err, "smithy servers rm <name>")
			return 2
		}
		if err := Remove(e.File, args[0]); err != nil {
			return fail(err)
		}
		fmt.Fprintln(e.Out, "removed", args[0])
	case "use":
		if len(args) != 1 {
			fmt.Fprintln(e.Err, "smithy servers use <name|local>")
			return 2
		}
		if err := Use(e.File, args[0]); err != nil {
			return fail(err)
		}
		fmt.Fprintln(e.Out, "default server:", args[0])
	case "discover":
		fs, err := e.Discover(ctx)
		if err != nil {
			return fail(err)
		}
		if len(fs) == 0 {
			fmt.Fprintln(e.Out, "no tailscale peer answers as a smithy daemon")
		}
		for _, f := range fs {
			fmt.Fprintf(e.Out, "%-16s %s  smithy %s   smithy servers add %s %s\n", f.Host, f.URL, f.Version, f.Host, f.URL)
		}
	default:
		fmt.Fprintln(e.Err, Usage)
		return 2
	}
	return 0
}

// PrivateURL is true for https, or http to loopback or a Tailscale address (100.64.0.0/10,
// fd7a:115c:a1e0::/48), where a bearer token does not cross the network in the clear.
func PrivateURL(u string) bool {
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	if p.Scheme == "https" {
		return true
	}
	h := p.Hostname()
	if h == "localhost" || strings.HasSuffix(h, ".ts.net") {
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	_, ts6, _ := net.ParseCIDR("fd7a:115c:a1e0::/48")
	return ip.IsLoopback() || cgnat.Contains(ip) || ts6.Contains(ip)
}

// StatusLine is a server's status in a few words: online 0.23.0 · claude, opencode · 2 sessions.
func StatusLine(st Status) string {
	if !st.Online {
		return "offline"
	}
	parts := []string{"online " + st.Version}
	if st.Err != "" {
		return parts[0] + " · " + st.Err
	}
	if len(st.Agents) > 0 {
		parts = append(parts, strings.Join(st.Agents, ", "))
	} else {
		parts = append(parts, "no ready agent")
	}
	parts = append(parts, fmt.Sprintf("%d sessions", len(st.Sessions)))
	return strings.Join(parts, " · ")
}
