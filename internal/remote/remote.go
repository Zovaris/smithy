// Package remote is the client side of reaching other smithy daemons: the saved servers, which
// one a command talks to, Tailscale discovery, per-server status and pairing.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/layout"
)

// Local is the reserved name of the daemon on this machine.
const Local = "local"

// Server is one saved daemon. Token is the device token pairing gave; empty behind tailscale serve.
type Server struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Token string `json:"token,omitempty"`
}

// List is the servers file.
type List struct {
	Default string   `json:"default,omitempty"`
	Servers []Server `json:"servers"`
}

// File is <smithy home>/servers.json (0600: it holds device tokens).
func File() string { return filepath.Join(layout.Home(), "servers.json") }

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)

// CleanURL checks an http(s) daemon URL and drops a trailing slash.
func CleanURL(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("bad server url %q: http(s)://host[:port]", s)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("bad server url %q: no query or fragment", s)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// Load reads path; a missing file is an empty list. A file others can read is refused.
func Load(path string) (List, error) {
	var l List
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return l, fmt.Errorf("%s is readable by others (mode %v); chmod 600 it", path, fi.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return l, err
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return l, fmt.Errorf("%s: %w", path, err)
	}
	return l, nil
}

func save(path string, l List) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(l, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func edit(path string, f func(*List) error) error {
	return config.WithLock(path, func() error {
		l, err := Load(path)
		if err != nil {
			return err
		}
		if err := f(&l); err != nil {
			return err
		}
		return save(path, l)
	})
}

// Add saves s, replacing a server of the same name.
func Add(path string, s Server) error {
	s.Name = strings.ToLower(strings.TrimSpace(s.Name))
	if s.Name == Local || !nameRe.MatchString(s.Name) {
		return fmt.Errorf("bad server name %q: one lower-case word, not %q", s.Name, Local)
	}
	u, err := CleanURL(s.URL)
	if err != nil {
		return err
	}
	s.URL = u
	return edit(path, func(l *List) error {
		if i := slices.IndexFunc(l.Servers, func(x Server) bool { return x.Name == s.Name }); i >= 0 {
			l.Servers[i] = s
		} else {
			l.Servers = append(l.Servers, s)
		}
		return nil
	})
}

// Remove drops the named server (and the default when it was that one).
func Remove(path, name string) error {
	return edit(path, func(l *List) error {
		i := slices.IndexFunc(l.Servers, func(x Server) bool { return x.Name == name })
		if i < 0 {
			return fmt.Errorf("no saved server %q", name)
		}
		l.Servers = slices.Delete(l.Servers, i, i+1)
		if l.Default == name {
			l.Default = ""
		}
		return nil
	})
}

// Use makes name the default; "local" (or "") goes back to this machine.
func Use(path, name string) error {
	return edit(path, func(l *List) error {
		if name == "" || name == Local {
			l.Default = ""
			return nil
		}
		if !slices.ContainsFunc(l.Servers, func(x Server) bool { return x.Name == name }) {
			return fmt.Errorf("no saved server %q", name)
		}
		l.Default = name
		return nil
	})
}

// Resolve picks the server: flag (--server), then env (SMITHY_SERVER), then the saved default,
// then local. Each is a saved name, "local" or a URL. Local comes back as Server{Name: "local"}.
func Resolve(path, flag, env string) (Server, error) {
	l, err := Load(path)
	if err != nil {
		return Server{}, err
	}
	pick := flag
	if pick == "" {
		pick = env
	}
	if pick == "" {
		pick = l.Default
	}
	if pick == "" || pick == Local {
		return Server{Name: Local}, nil
	}
	if strings.Contains(pick, "://") {
		u, err := CleanURL(pick)
		if err != nil {
			return Server{}, err
		}
		for _, s := range l.Servers {
			if s.URL == u {
				return s, nil
			}
		}
		return Server{URL: u}, nil
	}
	for _, s := range l.Servers {
		if s.Name == pick {
			return s, nil
		}
	}
	return Server{}, fmt.Errorf("no saved server %q (smithy servers lists them)", pick)
}

// Hello is GET /v1/hello: the one unauthenticated route, so discovery can tell a smithy daemon.
type Hello struct {
	Smithy  bool   `json:"smithy"`
	Version string `json:"version"`
}

// Peer is an online Tailscale peer.
type Peer struct {
	Host string `json:"host"`
	DNS  string `json:"dns"`
	IP   string `json:"ip"`
	OS   string `json:"os,omitempty"`
}

// ParseTailscale reads `tailscale status --json`: the online peers, sorted by host.
func ParseTailscale(b []byte) ([]Peer, error) {
	var st struct {
		Peer map[string]struct {
			HostName     string
			DNSName      string
			TailscaleIPs []string
			Online       bool
			OS           string
		}
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("tailscale status: %w", err)
	}
	var out []Peer
	for _, p := range st.Peer {
		if !p.Online {
			continue
		}
		ip := ""
		for _, a := range p.TailscaleIPs {
			if !strings.Contains(a, ":") {
				ip = a
				break
			}
		}
		out = append(out, Peer{Host: p.HostName, DNS: strings.TrimSuffix(p.DNSName, "."), IP: ip, OS: p.OS})
	}
	slices.SortFunc(out, func(a, b Peer) int { return strings.Compare(a.Host, b.Host) })
	return out, nil
}

// Found is a peer that answered as a smithy daemon at URL.
type Found struct {
	Peer
	URL     string `json:"url"`
	Version string `json:"version"`
}

// Discoverer finds Tailscale peers running smithy. Zero fields take the defaults.
type Discoverer struct {
	Cache      string        // default <home>/cache/tailscale-peers.json
	TTL        time.Duration // default 5m
	Timeout    time.Duration // per probe, default 2s
	Status     func(context.Context) ([]byte, error)
	Candidates func(Peer) []string // URLs to probe; default tailscale serve then the listen port
	HTTP       *http.Client
}

type cacheFile struct {
	At    time.Time `json:"at"`
	Found []Found   `json:"found"`
}

// DefaultCandidates are a peer's tailscale serve URL, then a direct daemon.listen on 7478.
func DefaultCandidates(p Peer) []string {
	var out []string
	if p.DNS != "" {
		out = append(out, "https://"+p.DNS)
	}
	if p.IP != "" {
		out = append(out, "http://"+p.IP+":7478")
	}
	return out
}

// Discover lists the peers running smithy, from the cache while it is fresh.
func (d Discoverer) Discover(ctx context.Context) ([]Found, error) {
	if d.Cache == "" {
		d.Cache = filepath.Join(layout.Home(), "cache", "tailscale-peers.json")
	}
	if d.TTL == 0 {
		d.TTL = 5 * time.Minute
	}
	if d.Timeout == 0 {
		d.Timeout = 2 * time.Second
	}
	if d.Candidates == nil {
		d.Candidates = DefaultCandidates
	}
	if d.Status == nil {
		return nil, errors.New("tailscale: no status source")
	}
	if b, err := os.ReadFile(d.Cache); err == nil {
		var c cacheFile
		if json.Unmarshal(b, &c) == nil && time.Since(c.At) < d.TTL {
			return c.Found, nil
		}
	}
	raw, err := d.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("tailscale status: %w", err)
	}
	peers, err := ParseTailscale(raw)
	if err != nil {
		return nil, err
	}
	res := make([]*Found, len(peers))
	var wg sync.WaitGroup
	for i, p := range peers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res[i] = d.probePeer(ctx, p)
		}()
	}
	wg.Wait()
	var out []Found
	for _, f := range res {
		if f != nil {
			out = append(out, *f)
		}
	}
	if err := os.MkdirAll(filepath.Dir(d.Cache), 0o700); err == nil {
		b, _ := json.Marshal(cacheFile{At: time.Now(), Found: out})
		_ = os.WriteFile(d.Cache, b, 0o600)
	}
	return out, nil
}

func (d Discoverer) probePeer(ctx context.Context, p Peer) *Found {
	urls := d.Candidates(p)
	got := make([]*Found, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if h, err := hello(ctx, d.HTTP, u, d.Timeout); err == nil {
				got[i] = &Found{Peer: p, URL: u, Version: h.Version}
			}
		}()
	}
	wg.Wait()
	for _, f := range got {
		if f != nil {
			return f
		}
	}
	return nil
}

func client(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return http.DefaultClient
}

func getJSON(ctx context.Context, hc *http.Client, base, path, token string, timeout time.Duration, v any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return err
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := client(hc).Do(r)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("%s: %s", path, res.Status)
	}
	return json.NewDecoder(http.MaxBytesReader(nil, res.Body, 4<<20)).Decode(v)
}

func hello(ctx context.Context, hc *http.Client, base string, timeout time.Duration) (Hello, error) {
	var h Hello
	if err := getJSON(ctx, hc, base, "/v1/hello", "", timeout, &h); err != nil {
		return h, err
	}
	if !h.Smithy {
		return h, errors.New("not a smithy daemon")
	}
	return h, nil
}

// SessionRow is one session in a server's status.
type SessionRow struct {
	ID     string `json:"id"`
	Agent  string `json:"agent"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// Status is what a server shows in the switcher.
type Status struct {
	Server   Server
	Online   bool
	Version  string
	Agents   []string // ready agents only
	Sessions []SessionRow
	Err      string
}

// Probe asks s for its version, ready agents and sessions within timeout.
func Probe(ctx context.Context, s Server, timeout time.Duration) Status {
	st := Status{Server: s}
	h, err := hello(ctx, nil, s.URL, timeout)
	if err != nil {
		st.Err = err.Error()
		return st
	}
	st.Online, st.Version = true, h.Version
	var setup struct {
		Agents []struct {
			Name  string `json:"name"`
			Ready bool   `json:"ready"`
		} `json:"agents"`
	}
	if err := getJSON(ctx, nil, s.URL, "/v1/setup", s.Token, timeout, &setup); err != nil {
		st.Err = err.Error()
		return st
	}
	for _, a := range setup.Agents {
		if a.Ready {
			st.Agents = append(st.Agents, a.Name)
		}
	}
	var sl struct {
		Sessions []SessionRow `json:"sessions"`
	}
	if err := getJSON(ctx, nil, s.URL, "/v1/sessions", s.Token, timeout, &sl); err != nil {
		st.Err = err.Error()
		return st
	}
	st.Sessions = sl.Sessions
	return st
}

// ProbeAll probes every server at once, keeping their order.
func ProbeAll(ctx context.Context, ss []Server, timeout time.Duration) []Status {
	out := make([]Status, len(ss))
	var wg sync.WaitGroup
	for i, s := range ss {
		wg.Add(1)
		go func() { defer wg.Done(); out[i] = Probe(ctx, s, timeout) }()
	}
	wg.Wait()
	return out
}

// NormalizeCode upper-cases a pairing code and puts the dash back: abcd2345 -> ABCD-2345.
func NormalizeCode(c string) string {
	c = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(c)))
	if len(c) == 8 {
		return c[:4] + "-" + c[4:]
	}
	return c
}

// Pair trades a one-time code for a device token at base.
func Pair(ctx context.Context, base, code, name string) (string, error) {
	b, _ := json.Marshal(map[string]string{"code": NormalizeCode(code), "name": name})
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/v1/pair", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	r.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		Token string `json:"token"`
		Error string `json:"error"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(nil, res.Body, 1<<16)).Decode(&out)
	if res.StatusCode != 200 || out.Token == "" {
		if out.Error == "" {
			out.Error = res.Status
		}
		return "", fmt.Errorf("pairing refused: %s", out.Error)
	}
	return out.Token, nil
}
