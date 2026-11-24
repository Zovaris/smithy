package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/auth"
)

// key is built from parts so it is not secret-shaped in the source.
var key = strings.Join([]string{"lin", "api", "TESTVALUE0123456789"}, "_")

type rig struct {
	p        *Proxy
	srv      *httptest.Server
	got      chan *http.Request
	log      []Entry
	mu       sync.Mutex
	approved bool
	asked    int
	lastAsk  Ask
	hold     chan struct{}
}

// newRig starts a TLS upstream that echoes the auth header and the key, and a proxy whose
// outbound dials all land on it.
func newRig(t *testing.T, secrets map[string]auth.ProxySecret) *rig {
	t.Helper()
	r := &rig{got: make(chan *http.Request, 4)}
	r.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		r.got <- q.Clone(context.Background())
		w.Header().Set("X-Echo", q.Header.Get("Authorization"))
		w.Header().Set("Set-Cookie", "session=abc")
		io.WriteString(w, `{"you":"`+q.Header.Get("Authorization")+`","host":"`+q.Host+`"}`)
	}))
	t.Cleanup(r.srv.Close)
	addr := r.srv.Listener.Addr().String()
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	tr := &http.Transport{DialContext: dial, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	r.p = &Proxy{
		Allow:   []string{"github.com", "*.githubusercontent.com"},
		Secrets: func(n string) (auth.ProxySecret, bool) { s, ok := secrets[n]; return s, ok },
		Client:  &http.Client{Transport: tr},
		Dial:    dial,
		Log:     func(e Entry) { r.mu.Lock(); r.log = append(r.log, e); r.mu.Unlock() },
		Approve: func(_ context.Context, a Ask) bool {
			r.mu.Lock()
			r.asked++
			r.lastAsk = a
			hold := r.hold
			r.mu.Unlock()
			if hold != nil {
				<-hold
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.approved
		},
	}
	return r
}

func linear() map[string]auth.ProxySecret {
	return map[string]auth.ProxySecret{"linear": {Value: key, Header: "Authorization", Hosts: []string{"api.linear.app"}, Methods: []string{"GET", "HEAD"}}}
}

func (r *rig) do(t *testing.T, method, target string, h http.Header) *httptest.ResponseRecorder {
	t.Helper()
	q := httptest.NewRequest(method, target, strings.NewReader(`{"query":"{viewer{id}}"}`))
	for k, v := range h {
		q.Header[k] = v
	}
	w := httptest.NewRecorder()
	r.p.ServeHTTP(w, q)
	return w
}

func TestSecretForwardedToBoundHostWithStoredKeyOnly(t *testing.T) {
	r := newRig(t, linear())
	w := r.do(t, "GET", "/s/linear/graphql?x=1", http.Header{"Authorization": {"Bearer worker-own"}, "Cookie": {"c=1"}, "X-Api-Key": {"k"}, "Accept": {"application/json"}})
	if w.Code != 200 {
		t.Fatalf("status %d %s", w.Code, w.Body)
	}
	q := <-r.got
	if q.Header.Get("Authorization") != key || q.Host != "api.linear.app" || q.URL.Path != "/graphql" || q.URL.RawQuery != "x=1" {
		t.Errorf("upstream got auth %q host %q path %q", q.Header.Get("Authorization"), q.Host, q.URL)
	}
	if len(q.Header.Values("Authorization")) != 1 || q.Header.Get("Cookie") != "" || q.Header.Get("X-Api-Key") != "" || q.Header.Get("Accept") != "application/json" {
		t.Errorf("worker headers leaked or dropped: %v", q.Header)
	}
	body := w.Body.String()
	if strings.Contains(body, key) || strings.Contains(w.Header().Get("X-Echo"), key) || w.Header().Get("Set-Cookie") != "" {
		t.Errorf("key or cookie reached the worker: %q %v", body, w.Header())
	}
	if !strings.Contains(body, "[secret:linear]") {
		t.Errorf("not scrubbed in place: %q", body)
	}
	if len(r.log) != 1 || r.log[0] != (Entry{Kind: "secret", Name: "linear", Host: "api.linear.app", Method: "GET", Status: 200}) {
		t.Errorf("log %+v", r.log)
	}
}

func TestSecretPrefixAndOtherHeader(t *testing.T) {
	s := map[string]auth.ProxySecret{"gh": {Value: key, Header: "Authorization", Prefix: "Bearer", Hosts: []string{"api.github.com"}, Methods: []string{"GET"}}}
	r := newRig(t, s)
	if w := r.do(t, "GET", "/s/gh/user", nil); w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	if q := <-r.got; q.Header.Get("Authorization") != "Bearer "+key {
		t.Errorf("auth %q", q.Header.Get("Authorization"))
	}
}

func TestSecretRefusals(t *testing.T) {
	two := linear()
	s := two["linear"]
	s.Hosts = append(s.Hosts, "linear.app")
	two["linear"] = s
	for _, c := range []struct{ name, method, target string }{
		{"unknown secret", "GET", "/s/nope/x"},
		{"other host", "GET", "/s/linear@example.com/graphql"},
		{"write not bound", "POST", "/s/linear/graphql"},
		{"no path", "GET", "/s/linear"},
		{"bad name", "GET", "/s/../x"},
		{"not a secret path", "GET", "/x"},
		{"plain http proxying", "GET", "http://example.com/"},
	} {
		r := newRig(t, two)
		if w := r.do(t, c.method, c.target, nil); w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
			t.Errorf("%s: status %d", c.name, w.Code)
		}
		if len(r.got) != 0 {
			t.Errorf("%s: forwarded", c.name)
		}
	}
	r := newRig(t, two)
	if w := r.do(t, "GET", "/s/linear@linear.app/x", nil); w.Code != 200 {
		t.Errorf("bound second host: %d", w.Code)
	}
	if q := <-r.got; q.Host != "linear.app" {
		t.Errorf("host %q", q.Host)
	}
	if w := r.do(t, "GET", "/s/linear/x", nil); w.Code != http.StatusForbidden {
		t.Errorf("two hosts and none named: %d", w.Code)
	}
}

func TestAskHoldsWritesForApproval(t *testing.T) {
	s := linear()
	l := s["linear"]
	l.Ask = true
	s["linear"] = l
	r := newRig(t, s)
	if w := r.do(t, "POST", "/s/linear/graphql?op=x", nil); w.Code != http.StatusForbidden || r.asked != 1 {
		t.Fatalf("denied ask: %d asked %d", w.Code, r.asked)
	}
	if r.lastAsk != (Ask{Name: "linear", Host: "api.linear.app", Method: "POST", Path: "/graphql?op=x"}) {
		t.Errorf("ask %+v", r.lastAsk)
	}
	r.approved = true
	if w := r.do(t, "POST", "/s/linear/graphql", nil); w.Code != 200 {
		t.Fatalf("approved ask: %d", w.Code)
	}
	if q := <-r.got; q.Method != "POST" {
		t.Errorf("method %s", q.Method)
	}
	if w := r.do(t, "GET", "/s/linear/graphql", nil); w.Code != 200 || r.asked != 2 {
		t.Errorf("read asked: %d %d", w.Code, r.asked)
	}
}

func connect(t *testing.T, p *Proxy, target string) (int, net.Conn) {
	t.Helper()
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	c, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	io.WriteString(c, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n")
	c.SetDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, c
}

func TestConnectAllowList(t *testing.T) {
	r := newRig(t, nil)
	code, c := connect(t, r.p, "github.com:443")
	if code != 200 {
		t.Fatalf("github.com: %d", code)
	}
	tc := tls.Client(c, &tls.Config{InsecureSkipVerify: true, ServerName: "github.com"})
	io.WriteString(tc, "GET / HTTP/1.1\r\nHost: github.com\r\nConnection: close\r\n\r\n")
	if resp, err := http.ReadResponse(bufio.NewReader(tc), nil); err != nil || resp.StatusCode != 200 {
		t.Fatalf("tunnel: %v", err)
	}
	if code, _ := connect(t, r.p, "objects.githubusercontent.com:443"); code != 200 {
		t.Errorf("wildcard: %d", code)
	}
	for _, bad := range []string{"example.com:443", "github.com:22", "githubusercontent.com:443", "evilgithub.com:443", "127.0.0.1:443", "github.com"} {
		if code, _ := connect(t, r.p, bad); code != http.StatusForbidden {
			t.Errorf("%s: %d", bad, code)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.log[0] != (Entry{Kind: "connect", Host: "github.com", Method: "CONNECT", Status: 200}) || r.log[len(r.log)-1].Status != 403 {
		t.Errorf("log %+v", r.log)
	}
}

func TestScrubReplacesEveryForm(t *testing.T) {
	b := scrub([]byte("a "+key+" b "+key), "linear", key)
	if strings.Contains(string(b), key) || strings.Count(string(b), "[secret:linear]") != 2 {
		t.Errorf("%s", b)
	}
}

func TestAskOnePendingPerSecret(t *testing.T) {
	s := linear()
	l := s["linear"]
	l.Ask = true
	s["linear"] = l
	r := newRig(t, s)
	r.hold, r.approved = make(chan struct{}), true
	done := make(chan int)
	go func() { done <- r.do(t, "POST", "/s/linear/graphql", nil).Code }()
	for i := 0; i < 200; i++ {
		r.mu.Lock()
		n := r.asked
		r.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if w := r.do(t, "POST", "/s/linear/graphql", nil); w.Code != http.StatusTooManyRequests {
		t.Errorf("second pending ask: %d", w.Code)
	}
	close(r.hold)
	if c := <-done; c != 200 {
		t.Errorf("first ask: %d", c)
	}
	<-r.got
}

func TestScrubBase64(t *testing.T) {
	b := scrub([]byte(base64.StdEncoding.EncodeToString([]byte(key))+" "+base64.RawURLEncoding.EncodeToString([]byte(key))), "linear", key)
	if strings.Count(string(b), "[secret:linear]") != 2 {
		t.Errorf("%s", b)
	}
}

func TestDefaultDomainsReachOpenCodeModels(t *testing.T) {
	for _, h := range []string{"opencode.ai", "models.opencode.ai", "api.opencode.ai"} {
		if !Allowed(DefaultDomains, h) {
			t.Errorf("%s refused: opencode workers fetch their models there", h)
		}
	}
	if Allowed(DefaultDomains, "evilopencode.ai") {
		t.Error("suffix match without a dot")
	}
}
