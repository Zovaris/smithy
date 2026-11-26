//go:build harness

package daemon

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func newRemoteTest(t *testing.T) (*Server, *httptest.Server, *httptest.Server) {
	t.Helper()
	sv, local := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	sv.Pair = &Pairing{File: filepath.Join(t.TempDir(), "devices.json")}
	sv.Version = "0.23.0"
	remote := httptest.NewServer(sv.RemoteHandler())
	t.Cleanup(remote.Close)
	return sv, local, remote
}

func TestHelloIsPublicAndSaysLittle(t *testing.T) {
	_, local, remote := newRemoteTest(t)
	for _, base := range []string{local.URL, remote.URL} {
		res := do(t, "GET", base+"/v1/hello", "", nil)
		var h map[string]any
		json.NewDecoder(res.Body).Decode(&h)
		if res.StatusCode != 200 || h["smithy"] != true || h["version"] != "0.23.0" || len(h) != 2 {
			t.Fatalf("%s: %d %v", base, res.StatusCode, h)
		}
	}
}

func pairCode(t *testing.T, base string, hdr map[string]string) (int, string) {
	t.Helper()
	res := do(t, "POST", base+"/v1/pair/code", "{}", hdr)
	var b struct{ Code string }
	json.NewDecoder(res.Body).Decode(&b)
	return res.StatusCode, b.Code
}

func TestPairFlowOverTheRemoteListener(t *testing.T) {
	_, local, remote := newRemoteTest(t)
	if code, _ := pairCode(t, local.URL, nil); code != 401 {
		t.Fatalf("a code needs the daemon token: %d", code)
	}
	if code, _ := pairCode(t, local.URL, map[string]string{LoginHeader: "me@example.com"}); code != 403 {
		t.Fatalf("a tailnet login cannot mint codes: %d", code)
	}
	st, c := pairCode(t, local.URL, auth)
	if st != 200 || c == "" {
		t.Fatalf("%d %q", st, c)
	}
	if res := do(t, "GET", remote.URL+"/v1/state", "", nil); res.StatusCode != 401 {
		t.Fatalf("no token, no API: %d", res.StatusCode)
	}
	if res := do(t, "GET", remote.URL+"/v1/state", "", map[string]string{LoginHeader: "me@example.com"}); res.StatusCode != 401 {
		t.Fatalf("the login header means nothing on a direct listener: %d", res.StatusCode)
	}
	res := do(t, "POST", remote.URL+"/v1/pair", `{"code":"`+c+`","name":"laptop"}`, nil)
	var p struct{ Token string }
	json.NewDecoder(res.Body).Decode(&p)
	if res.StatusCode != 200 || !strings.HasPrefix(p.Token, "smd_") {
		t.Fatalf("%d %+v", res.StatusCode, p)
	}
	dev := map[string]string{"Authorization": "Bearer " + p.Token}
	if code, _ := pairCode(t, remote.URL, dev); code != 404 {
		t.Fatalf("codes are minted on the loopback listener only: %d", code)
	}
	if res := do(t, "GET", remote.URL+"/v1/state", "", dev); res.StatusCode != 200 {
		t.Fatalf("device token on the remote listener: %d", res.StatusCode)
	}
	if res := do(t, "GET", remote.URL+"/v1/state", "", auth); res.StatusCode != 401 {
		t.Fatalf("the daemon token never leaves loopback: %d", res.StatusCode)
	}
	if code, _ := pairCode(t, local.URL, dev); code != 403 {
		t.Fatalf("a device cannot mint more codes: %d", code)
	}
	if res := do(t, "POST", remote.URL+"/v1/pair", `{"code":"`+c+`","name":"again"}`, nil); res.StatusCode != 403 {
		t.Fatalf("a used code: %d", res.StatusCode)
	}
	if res := do(t, "POST", remote.URL+"/v1/pair", `code=x`, map[string]string{"Content-Type": "text/plain"}); res.StatusCode != 415 {
		t.Fatalf("pair takes JSON only: %d", res.StatusCode)
	}
}

func TestRemoteListenerWithoutPairing(t *testing.T) {
	sv, _ := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	remote := httptest.NewServer(sv.RemoteHandler())
	defer remote.Close()
	if res := do(t, "POST", remote.URL+"/v1/pair", `{"code":"AAAA-AAAA","name":"x"}`, nil); res.StatusCode != 403 {
		t.Fatalf("%d", res.StatusCode)
	}
	if res := do(t, "GET", remote.URL+"/v1/state", "", auth); res.StatusCode != 401 {
		t.Fatalf("only device tokens: %d", res.StatusCode)
	}
}

func TestServersProxy(t *testing.T) {
	other, otherHS := newTest(t, filepath.Join(t.TempDir(), "o.db"))
	other.Version = "9.9.9"
	_ = otherHS
	other.Pair = &Pairing{File: filepath.Join(t.TempDir(), "devices.json")}
	oc, _ := other.Pair.NewCode()
	devTok, err := other.Pair.Redeem(oc, "this")
	if err != nil {
		t.Fatal(err)
	}
	otherRemote := httptest.NewServer(other.RemoteHandler())
	defer otherRemote.Close()
	sv, local, remote := newRemoteTest(t)
	sv.Servers = func() ([]RemoteServer, error) {
		return []RemoteServer{{Name: "home", URL: otherRemote.URL, Token: devTok}}, nil
	}
	res := do(t, "GET", local.URL+"/v1/servers", "", auth)
	var l struct {
		Servers []struct {
			Name, URL, Version string
			Online             bool
			Token              string
		}
	}
	json.NewDecoder(res.Body).Decode(&l)
	if res.StatusCode != 200 || len(l.Servers) != 1 || !l.Servers[0].Online || l.Servers[0].Version != "9.9.9" || l.Servers[0].Token != "" {
		t.Fatalf("%d %+v", res.StatusCode, l)
	}
	res = do(t, "GET", local.URL+"/v1/servers/home/v1/state", "", auth)
	var st State
	json.NewDecoder(res.Body).Decode(&st)
	if res.StatusCode != 200 || st.Version != "9.9.9" {
		t.Fatalf("the proxy reaches home with its token: %d %+v", res.StatusCode, st)
	}
	if res := do(t, "GET", local.URL+"/v1/servers/nope/v1/state", "", auth); res.StatusCode != 404 {
		t.Fatalf("unknown server: %d", res.StatusCode)
	}
	if res := do(t, "GET", local.URL+"/v1/servers/home/v1/state", "", map[string]string{LoginHeader: "me@example.com"}); res.StatusCode != 403 {
		t.Fatalf("a tailnet login cannot use this machine's saved tokens: %d", res.StatusCode)
	}
	sc, _ := sv.Pair.NewCode()
	myTok, _ := sv.Pair.Redeem(sc, "laptop")
	if res := do(t, "GET", remote.URL+"/v1/servers/home/v1/state", "", map[string]string{"Authorization": "Bearer " + myTok}); res.StatusCode != 404 {
		t.Fatalf("no proxy on the remote listener: %d", res.StatusCode)
	}
	if res := do(t, "GET", local.URL+"/v1/servers/home/etc/passwd", "", auth); res.StatusCode != 404 {
		t.Fatalf("only /v1 paths go through: %d", res.StatusCode)
	}
}

func TestDevicesCannotChangeLocalSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SMITHY_CONFIG", filepath.Join(t.TempDir(), "config"))
	sv, _, remote := newRemoteTest(t)
	c, _ := sv.Pair.NewCode()
	tok, err := sv.Pair.Redeem(c, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	dev := map[string]string{"Authorization": "Bearer " + tok, "Origin": remote.URL}
	for _, r := range []struct{ method, path, body string }{
		{"POST", "/v1/guard/suggested", "{}"},
		{"PUT", "/v1/setup/kit", `{"mcp":false}`},
		{"POST", "/v1/setup/skills/install", "{}"},
	} {
		res := do(t, r.method, remote.URL+r.path, r.body, dev)
		var e struct{ Error string }
		json.NewDecoder(res.Body).Decode(&e)
		if res.StatusCode != 403 || !strings.Contains(e.Error, "daemon token") {
			t.Fatalf("%s %s from a device: %d %q", r.method, r.path, res.StatusCode, e.Error)
		}
	}
}
