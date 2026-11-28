//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/remote"
)

// RemoteServer is a saved server the web switcher can reach through this daemon.
type RemoteServer struct{ Name, URL, Token string }

type serverRow struct {
	Name     string            `json:"name"`
	URL      string            `json:"url"`
	Online   bool              `json:"online"`
	Version  string            `json:"version,omitempty"`
	Agents   []string          `json:"agents"`
	Sessions []json.RawMessage `json:"sessions"`
	Err      string            `json:"error,omitempty"`
}

// localOnly lets only the daemon token through: saved servers' tokens and this machine's
// settings never serve a tailnet login or a paired device.
func localOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Smithy-Who") != "local" {
		writeJSON(w, 403, map[string]string{"error": "this needs this machine's daemon token"})
		return false
	}
	return true
}

// listServers is GET /v1/servers: the saved servers with their status, agents and sessions.
func (sv *Server) listServers(w http.ResponseWriter, r *http.Request) {
	if !localOnly(w, r) {
		return
	}
	ss, err := sv.servers()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	rows := make([]serverRow, len(ss))
	var wg sync.WaitGroup
	for i, s := range ss {
		wg.Add(1)
		go func() { defer wg.Done(); rows[i] = probeServer(r.Context(), s) }()
	}
	wg.Wait()
	writeJSON(w, 200, map[string]any{"servers": rows})
}

func probeServer(ctx context.Context, s RemoteServer) serverRow {
	row := serverRow{Name: s.Name, URL: s.URL, Agents: []string{}, Sessions: []json.RawMessage{}}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	get := func(path string, v any) error {
		c := &Client{Base: s.URL, Token: s.Token}
		req, err := c.req(ctx, "GET", path, nil)
		if err != nil {
			return err
		}
		res, err := c.http().Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return &url.Error{Op: "GET", URL: path, Err: errStatus(res.Status)}
		}
		return json.NewDecoder(http.MaxBytesReader(nil, res.Body, 4<<20)).Decode(v)
	}
	var h struct {
		Smithy  bool   `json:"smithy"`
		Version string `json:"version"`
	}
	if err := get("/v1/hello", &h); err != nil || !h.Smithy {
		row.Err = "offline"
		return row
	}
	row.Online, row.Version = true, h.Version
	var setup struct {
		Agents []struct {
			Name  string `json:"name"`
			Ready bool   `json:"ready"`
		} `json:"agents"`
	}
	if err := get("/v1/setup", &setup); err != nil {
		row.Err = err.Error()
		return row
	}
	for _, a := range setup.Agents {
		if a.Ready {
			row.Agents = append(row.Agents, a.Name)
		}
	}
	var sl struct {
		Sessions []json.RawMessage `json:"sessions"`
	}
	if err := get("/v1/sessions", &sl); err != nil {
		row.Err = err.Error()
		return row
	}
	if sl.Sessions != nil {
		row.Sessions = sl.Sessions
	}
	return row
}

type errStatus string

func (e errStatus) Error() string { return string(e) }

func (sv *Server) servers() ([]RemoteServer, error) {
	if sv.Servers == nil {
		if sv.ServersFile == "" {
			return nil, nil
		}
		l, err := remote.Load(sv.ServersFile)
		out := make([]RemoteServer, 0, len(l.Servers))
		for _, s := range l.Servers {
			out = append(out, RemoteServer{Name: s.Name, URL: s.URL, Token: s.Token})
		}
		return out, err
	}
	return sv.Servers()
}

// proxyServer is /v1/servers/{name}/v1/...: the same API on a saved server, sent with its token.
func (sv *Server) proxyServer(w http.ResponseWriter, r *http.Request) {
	if !localOnly(w, r) {
		return
	}
	rest := "/" + r.PathValue("rest")
	if rest != "/v1" && !strings.HasPrefix(rest, "/v1/") || strings.Contains(rest, "..") {
		http.NotFound(w, r)
		return
	}
	ss, err := sv.servers()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	var target *RemoteServer
	for i := range ss {
		if ss[i].Name == r.PathValue("name") {
			target = &ss[i]
		}
	}
	if target == nil {
		writeJSON(w, 404, map[string]string{"error": "no saved server " + r.PathValue("name")})
		return
	}
	u, err := url.Parse(target.URL)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "bad server url"})
		return
	}
	rp := &httputil.ReverseProxy{
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host = u.Scheme, u.Host
			pr.Out.URL.Path = strings.TrimRight(u.Path, "/") + rest
			pr.Out.URL.RawPath = ""
			pr.Out.Host = u.Host
			for _, h := range []string{"Authorization", "Cookie", "Origin", "Referer", "X-Smithy-Who", "X-Smithy-Hook", LoginHeader, "Sec-Fetch-Site"} {
				pr.Out.Header.Del(h)
			}
			if target.Token != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+target.Token)
			}
		},
	}
	rp.ServeHTTP(w, r)
}
