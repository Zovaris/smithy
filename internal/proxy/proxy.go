// Package proxy is smithy's egress proxy, run natively next to the daemon (smithy proxy serve) or
// in-process by smithy mcp's secret tool: it tunnels CONNECT only to allowed hosts on port 443, and
// sends stored keys for the worker (http://127.0.0.1:<port>/s/<name>/<path>) to the key's bound
// hosts over HTTPS, so the worker never holds a key. No TLS interception. Its log keeps kind, host, method, name and status only.
package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/auth"
)

// Port is where smithy proxy serve listens by default.
const Port = 8877

// DefaultAddr is serve's default listen address: loopback only, so the bound keys stay on this machine.
var DefaultAddr = "127.0.0.1:8877"

// MaxBody caps a secret call's response, read whole so the key can be scrubbed from it.
const MaxBody = 32 << 20

// Entry is one log line: metadata only, never a path, body or key.
type Entry struct {
	Kind   string // connect or secret
	Name   string // the secret, on kind secret
	Host   string
	Method string
	Status int
}

// Ask is a write held for the user's approval; Path (with its query) is shown to the user only,
// never logged.
type Ask struct{ Name, Host, Method, Path string }

// Proxy serves CONNECT tunnels and /s/<name>/<path> secret calls.
type Proxy struct {
	Allow   []string // hosts tunnels may reach; "*.example.com" matches subdomains only
	Secrets func(name string) (auth.ProxySecret, bool)
	Client  *http.Client // secret calls; nil = a client that follows no redirects
	Dial    func(ctx context.Context, network, addr string) (net.Conn, error)
	Log     func(Entry)
	Approve func(context.Context, Ask) bool // an ask secret's write; nil = denied
	// Guard, when set, decides an allowed tunnel's host (the fetch guard: guard.fetch_block asks).
	Guard func(ctx context.Context, host string) (bool, string)

	mu      sync.Mutex
	pending map[string]bool // secrets with an ask waiting: one at a time each
}

var secretName, hostChars = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`), regexp.MustCompile(`^[a-z0-9.-]+$`)

// stripped are the worker headers never forwarded on a secret call: credentials, cookies and
// hop-by-hop headers (Accept-Encoding too, so the response arrives decoded and can be scrubbed).
var stripped = []string{"Authorization", "Proxy-Authorization", "Cookie", "X-Api-Key", "Api-Key", "X-Auth-Token", "Private-Token",
	"Connection", "Keep-Alive", "Proxy-Connection", "Te", "Trailer", "Transfer-Encoding", "Upgrade", "Accept-Encoding", "Forwarded", "X-Forwarded-For"}

func (p *Proxy) log(e Entry) {
	if p.Log != nil {
		p.Log(e)
	}
}

// Allowed reports whether host matches the allow list.
func Allowed(allow []string, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if net.ParseIP(host) != nil {
		return false
	}
	for _, a := range allow {
		a = strings.ToLower(a)
		if s, ok := strings.CutPrefix(a, "*."); ok {
			if strings.HasSuffix(host, "."+s) {
				return true
			}
		} else if host == a {
			return true
		}
	}
	return false
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.connect(w, r)
		return
	}
	if !strings.HasPrefix(r.RequestURI, "/s/") {
		host := r.URL.Hostname()
		p.log(Entry{Kind: "connect", Host: host, Method: r.Method, Status: http.StatusForbidden})
		http.Error(w, "smithy proxy: only CONNECT to allowed hosts and /s/<secret>/<path>", http.StatusForbidden)
		return
	}
	p.secret(w, r)
}

func (p *Proxy) connect(w http.ResponseWriter, r *http.Request) {
	host, port, err := net.SplitHostPort(r.Host)
	deny := func() {
		p.log(Entry{Kind: "connect", Host: host, Method: "CONNECT", Status: http.StatusForbidden})
		http.Error(w, "smithy proxy: host not allowed (auto.sandbox_domains)", http.StatusForbidden)
	}
	if err != nil || port != "443" || !Allowed(p.Allow, host) {
		if err != nil {
			host = r.Host
		}
		deny()
		return
	}
	if p.Guard != nil {
		if ok, why := p.Guard(r.Context(), host); !ok {
			p.log(Entry{Kind: "connect", Host: host, Method: "CONNECT", Status: http.StatusForbidden})
			http.Error(w, "smithy proxy: "+why, http.StatusForbidden)
			return
		}
	}
	dial := p.Dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: 15 * time.Second}).DialContext
	}
	up, err := dial(r.Context(), "tcp", net.JoinHostPort(host, port))
	if err != nil {
		p.log(Entry{Kind: "connect", Host: host, Method: "CONNECT", Status: http.StatusBadGateway})
		http.Error(w, "smithy proxy: dial failed", http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		up.Close()
		http.Error(w, "smithy proxy: no hijack", http.StatusInternalServerError)
		return
	}
	down, buf, err := hj.Hijack()
	if err != nil {
		up.Close()
		return
	}
	p.log(Entry{Kind: "connect", Host: host, Method: "CONNECT", Status: http.StatusOK})
	_, _ = io.WriteString(down, "HTTP/1.1 200 Connection established\r\n\r\n")
	if n := buf.Reader.Buffered(); n > 0 {
		b, _ := buf.Peek(n)
		_, _ = up.Write(b)
	}
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if c, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
		done <- struct{}{}
	}
	go pipe(up, down)
	go pipe(down, up)
	<-done
	<-done
	up.Close()
	down.Close()
}

// parse splits /s/<name>[@host]/<rest> (escaped) into its parts.
func parse(escaped string) (name, host, rest string, ok bool) {
	s := strings.TrimPrefix(escaped, "/s/")
	head, rest, found := strings.Cut(s, "/")
	if !found {
		return "", "", "", false
	}
	name, host, _ = strings.Cut(head, "@")
	if !secretName.MatchString(name) || (host != "" && !hostChars.MatchString(host)) {
		return "", "", "", false
	}
	return name, host, "/" + rest, true
}

func (p *Proxy) hold(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending[name] {
		return false
	}
	if p.pending == nil {
		p.pending = map[string]bool{}
	}
	p.pending[name] = true
	return true
}

func (p *Proxy) release(name string) {
	p.mu.Lock()
	delete(p.pending, name)
	p.mu.Unlock()
}

func writes(m string) bool { return m != "GET" && m != "HEAD" && m != "OPTIONS" }

func (p *Proxy) secret(w http.ResponseWriter, r *http.Request) {
	name, host, rest, ok := parse(r.URL.EscapedPath())
	e := Entry{Kind: "secret", Name: name, Host: host, Method: r.Method}
	refuse := func(code int, why string) {
		e.Status = code
		p.log(e)
		http.Error(w, "smithy proxy: "+why, code)
	}
	if !ok {
		e.Name, e.Host = "", ""
		refuse(http.StatusNotFound, "use /s/<secret>/<path> or /s/<secret>@<host>/<path>")
		return
	}
	s, found := auth.ProxySecret{}, false
	if p.Secrets != nil {
		s, found = p.Secrets(name)
	}
	if !found {
		refuse(http.StatusNotFound, "no bound secret "+name+" (smithy auth bind)")
		return
	}
	switch {
	case host == "" && len(s.Hosts) == 1:
		host = s.Hosts[0]
		e.Host = host
	case host == "":
		refuse(http.StatusForbidden, name+" is bound to several hosts: use /s/"+name+"@<host>/<path>")
		return
	case !slices.Contains(s.Hosts, host):
		refuse(http.StatusForbidden, name+" is not bound to "+host)
		return
	}
	if !slices.Contains(s.Methods, r.Method) {
		if !s.Ask || !writes(r.Method) || p.Approve == nil {
			refuse(http.StatusForbidden, r.Method+" with "+name+" is not allowed (smithy auth bind, or smithy auth ask)")
			return
		}
		if !p.hold(name) {
			refuse(http.StatusTooManyRequests, "an approval for "+name+" is already waiting")
			return
		}
		ask := Ask{Name: name, Host: host, Method: r.Method, Path: rest}
		if r.URL.RawQuery != "" {
			ask.Path += "?" + r.URL.RawQuery
		}
		ok := p.Approve(r.Context(), ask)
		p.release(name)
		if !ok {
			refuse(http.StatusForbidden, r.Method+" with "+name+" was not approved")
			return
		}
	}
	u := &url.URL{Scheme: "https", Host: host, RawQuery: r.URL.RawQuery}
	if err := setPath(u, rest); err != nil {
		refuse(http.StatusBadRequest, "bad path")
		return
	}
	out, err := http.NewRequestWithContext(r.Context(), r.Method, u.String(), r.Body)
	if err != nil {
		refuse(http.StatusBadRequest, "bad request")
		return
	}
	out.ContentLength = r.ContentLength
	for k, v := range r.Header {
		if !slices.ContainsFunc(stripped, func(s string) bool { return strings.EqualFold(s, k) }) && !strings.EqualFold(k, s.Header) {
			out.Header[k] = v
		}
	}
	v := s.Value
	if s.Prefix != "" {
		v = s.Prefix + " " + v
	}
	out.Header.Set(s.Header, v)
	c := p.Client
	if c == nil {
		c = &http.Client{Timeout: 5 * time.Minute}
	}
	cc := *c
	cc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := cc.Do(out) //nolint:gosec // G704: the upstream comes from the proxy config, not the request
	if err != nil {
		refuse(http.StatusBadGateway, "upstream request failed")
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil || len(body) > MaxBody {
		refuse(http.StatusBadGateway, "upstream response unreadable or over 32 MiB")
		return
	}
	body = scrub(body, name, s.Value)
	for k, vs := range resp.Header {
		if strings.EqualFold(k, "Set-Cookie") || strings.EqualFold(k, "Content-Length") || slices.ContainsFunc(stripped, func(s string) bool { return strings.EqualFold(s, k) }) {
			continue
		}
		for _, x := range vs {
			w.Header().Add(k, string(scrub([]byte(x), name, s.Value)))
		}
	}
	e.Status = resp.StatusCode
	p.log(e)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body) //nolint:gosec // G705: proxied upstream bytes, passed through unchanged
}

func setPath(u *url.URL, escaped string) error {
	p, err := url.PathUnescape(escaped)
	if err != nil {
		return err
	}
	u.Path, u.RawPath = p, escaped
	return nil
}

// scrub replaces the key, raw, URL-escaped and base64-encoded, with [secret:<name>].
func scrub(b []byte, name, key string) []byte {
	if key == "" {
		return b
	}
	mark := []byte("[secret:" + name + "]")
	kb := []byte(key)
	for _, k := range []string{key, url.QueryEscape(key), url.PathEscape(key), base64.StdEncoding.EncodeToString(kb),
		base64.RawStdEncoding.EncodeToString(kb), base64.URLEncoding.EncodeToString(kb), base64.RawURLEncoding.EncodeToString(kb)} {
		b = bytes.ReplaceAll(b, []byte(k), mark)
	}
	return b
}
