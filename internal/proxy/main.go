package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/events"
)

// Usage is the text for `smithy proxy --help`.
const Usage = `usage: smithy proxy serve [--addr :8877] [--allow host,...] [--ask-timeout 5m] | approve <id> | deny <id>
serve is the egress proxy a container worker uses under auto.sandbox_network proxy (smithy agent
starts it as the smithy-proxy sidecar): CONNECT only to --allow hosts on 443, and
/s/<secret>/<path> sends a key bound with smithy auth bind to its host. A write held by an ask
secret is logged as a secret_ask event; approve or deny it by id, else it is denied after --ask-timeout.`

var askID = regexp.MustCompile(`^[0-9a-f]{16}$`)

// EventsApprover holds an ask for an approve or deny event with its id (smithy proxy approve,
// the TUI or Telegram), denying it after timeout.
func EventsApprover(timeout, poll time.Duration) func(context.Context, Ask) bool {
	return func(ctx context.Context, a Ask) bool {
		b := make([]byte, 8)
		rand.Read(b)
		id := hex.EncodeToString(b)
		start := time.Now().Add(-time.Second)
		if events.Append(events.Event{Kind: "secret_ask", ID: id, Detail: a.Name + " " + a.Method + " " + a.Host + a.Path + " (smithy proxy approve " + id + ")"}) != nil {
			return false
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		t := time.NewTicker(poll)
		defer t.Stop()
		for {
			es, _ := events.Read(start, 0)
			for _, e := range es {
				if e.ID == id && (e.Kind == "secret_approved" || e.Kind == "secret_denied") {
					return e.Kind == "secret_approved"
				}
			}
			select {
			case <-ctx.Done():
				events.Append(events.Event{Kind: "secret_denied", ID: id, Detail: "timed out"})
				return false
			case <-t.C:
			}
		}
	}
}

// DefaultDomains are the hosts a proxied worker always reaches: GitHub, the harnesses' model APIs
// and the package registries; auto.sandbox_domains adds more.
var DefaultDomains = []string{"github.com", "api.github.com", "codeload.github.com", "uploads.github.com", "objects.githubusercontent.com", "raw.githubusercontent.com",
	"api.anthropic.com", "api.openai.com", "chatgpt.com", "opencode.ai", "models.dev",
	"registry.npmjs.org", "proxy.golang.org", "sum.golang.org", "pypi.org", "files.pythonhosted.org"}

// Main runs `smithy proxy`.
func Main(args []string, stdout, stderr io.Writer) int {
	fail := func(code int, err error) int {
		fmt.Fprintln(stderr, "smithy proxy:", err)
		return code
	}
	switch {
	case len(args) == 2 && (args[0] == "approve" || args[0] == "deny"):
		if !askID.MatchString(args[1]) {
			return fail(2, fmt.Errorf("bad id %q", args[1]))
		}
		kind := map[string]string{"approve": "secret_approved", "deny": "secret_denied"}[args[0]]
		if err := events.Append(events.Event{Kind: kind, ID: args[1]}); err != nil {
			return fail(1, err)
		}
		fmt.Fprintln(stdout, kind, args[1])
		return 0
	case len(args) >= 1 && args[0] == "serve":
		addr, allow, timeout := ":"+strconv.Itoa(Port), DefaultDomains, 5*time.Minute
		for rest := args[1:]; len(rest) > 0; rest = rest[2:] {
			if len(rest) < 2 {
				return fail(2, fmt.Errorf("%s needs a value", rest[0]))
			}
			switch rest[0] {
			case "--addr":
				addr = rest[1]
			case "--allow":
				allow = nil
				for _, h := range strings.Split(rest[1], ",") {
					if h = strings.TrimSpace(h); h != "" {
						allow = append(allow, h)
					}
				}
			case "--ask-timeout":
				d, err := time.ParseDuration(rest[1])
				if err != nil || d <= 0 {
					return fail(2, fmt.Errorf("bad --ask-timeout %q", rest[1]))
				}
				timeout = d
			default:
				return fail(2, fmt.Errorf("unknown flag %s", rest[0]))
			}
		}
		p := &Proxy{Allow: allow, Secrets: auth.Secret, Approve: EventsApprover(timeout, time.Second),
			Log: func(e Entry) {
				line := fmt.Sprintf("%s %s %s %s %d", e.Kind, e.Name, e.Method, e.Host, e.Status)
				fmt.Fprintln(stderr, time.Now().UTC().Format(time.RFC3339), line)
				if e.Status >= 400 {
					events.Append(events.Event{Kind: "proxy_denied", Detail: line})
				}
			}}
		fmt.Fprintf(stderr, "smithy proxy: listening on %s, %d allowed hosts\n", addr, len(allow))
		srv := &http.Server{Addr: addr, Handler: p, ReadHeaderTimeout: 30 * time.Second}
		return fail(1, srv.ListenAndServe())
	}
	fmt.Fprintln(stderr, Usage)
	return 2
}
