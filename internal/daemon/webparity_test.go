//go:build harness

package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/remote"
)

func TestSessionShellRunsInTheSessionFolder(t *testing.T) {
	dir := t.TempDir()
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) { return "ok", nil })
	code, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+dir+`","message":"hi"}`)
	if code != 201 {
		t.Fatalf("start: %d %v", code, m)
	}
	id := m["id"].(string)
	waitStatus(t, sv, id, StatusDone)
	os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o600)
	code, m = post(t, url+"/v1/sessions/"+id+"/shell", `{"cmd":"ls; exit 3"}`)
	if code != 200 || !strings.Contains(fmt.Sprint(m["lines"]), "marker.txt") || m["code"].(float64) != 3 {
		t.Fatalf("shell: %d %v", code, m)
	}
	code, m = post(t, url+"/v1/sessions/"+id+"/shell", `{"cmd":"echo out; echo bad >&2"}`)
	ls, _ := m["lines"].([]any)
	marked := 0
	for _, l := range ls {
		if x := l.(map[string]any); (x["err"] == true) == (x["text"] == "bad") {
			marked++
		}
	}
	if code != 200 || len(ls) != 2 || marked != 2 || m["took_ms"] == nil {
		t.Fatalf("stderr lines not marked: %d %v", code, m)
	}
	if code, _ := post(t, url+"/v1/sessions/nope/shell", `{"cmd":"ls"}`); code != 404 {
		t.Fatalf("unknown session: %d", code)
	}
	if code, _ := post(t, url+"/v1/sessions/"+id+"/shell", `{"cmd":"  "}`); code != 400 {
		t.Fatalf("empty command: %d", code)
	}
	if res := do(t, "POST", url+"/v1/sessions/"+id+"/shell", `{"cmd":"ls"}`, map[string]string{LoginHeader: "me@example.com"}); res.StatusCode != 403 {
		t.Fatalf("a tailnet login cannot run a shell: %d", res.StatusCode)
	}
}

func TestSessionShellLandsInTheTranscriptCapped(t *testing.T) {
	dir := t.TempDir()
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) { return "ok", nil })
	_, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+dir+`","message":"hi"}`)
	id := m["id"].(string)
	waitStatus(t, sv, id, StatusDone)
	code, m := post(t, url+"/v1/sessions/"+id+"/shell", `{"cmd":"seq 2000; printf '\\033[31mred\\033[0m' >&2"}`)
	if code != 200 || m["id"] == "" || m["cut"] != true || len(m["lines"].([]any)) != api.ShellMaxLines {
		t.Fatalf("shell: %d cut=%v lines=%d", code, m["cut"], len(m["lines"].([]any)))
	}
	evs, _ := sv.st.Since(0)
	var got []ShellRun
	for _, e := range evs {
		if e.Kind == KindSessionShell && e.Session == id {
			var r ShellRun
			if json.Unmarshal(e.Data, &r) != nil {
				t.Fatalf("bad event %s", e.Data)
			}
			got = append(got, r)
		}
	}
	if len(got) != 1 || got[0].ID != m["id"] || got[0].Cmd == "" || !strings.HasSuffix(got[0].Dir, filepath.Base(dir)) || !got[0].Cut || got[0].Lines[0].Text != "1" || len(got[0].Lines) != api.ShellMaxLines {
		t.Fatalf("%d events, first cut=%v", len(got), len(got) > 0 && got[0].Cut)
	}
	_, m = post(t, url+"/v1/sessions/"+id+"/shell", `{"cmd":"printf '\\033[31mred\\033[0m\\r\\n' >&2"}`)
	if l := m["lines"].([]any)[0].(map[string]any); l["text"] != "red" || l["err"] != true {
		t.Fatalf("escapes kept: %v", m)
	}
}

var onePNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")

func TestImagesSavedForTheNextMessage(t *testing.T) {
	_, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	body := `{"data":"` + base64.StdEncoding.EncodeToString(onePNG) + `"}`
	code, m := post(t, hs.URL+"/v1/images", body)
	if code != 200 {
		t.Fatalf("image: %d %v", code, m)
	}
	p := m["path"].(string)
	b, err := os.ReadFile(p)
	if err != nil || string(b) != string(onePNG) || filepath.Ext(p) != ".png" {
		t.Fatalf("saved %q: %v", p, err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if code, _ := post(t, hs.URL+"/v1/images", `{"data":"`+base64.StdEncoding.EncodeToString([]byte("not an image"))+`"}`); code != 400 {
		t.Fatalf("not an image: %d", code)
	}
	if res := do(t, "POST", hs.URL+"/v1/images", body, map[string]string{LoginHeader: "me@example.com"}); res.StatusCode != 403 {
		t.Fatalf("a tailnet login cannot write files: %d", res.StatusCode)
	}
}

func TestServersAddPairsAndRemoves(t *testing.T) {
	other, _ := newTest(t, filepath.Join(t.TempDir(), "o.db"))
	other.Pair = &Pairing{File: filepath.Join(t.TempDir(), "devices.json")}
	otherRemote := httptest.NewServer(other.RemoteHandler())
	defer otherRemote.Close()
	oc, _ := other.Pair.NewCode()

	sv, local, _ := newRemoteTest(t)
	sv.ServersFile = filepath.Join(t.TempDir(), "servers.json")
	code, m := post(t, local.URL+"/v1/servers", `{"name":"home","url":"`+otherRemote.URL+`","code":"`+oc+`"}`)
	if code != 200 || m["name"] != "home" {
		t.Fatalf("add: %d %v", code, m)
	}
	l, err := remote.Load(sv.ServersFile)
	if err != nil || len(l.Servers) != 1 || l.Servers[0].Token == "" {
		t.Fatalf("saved: %+v %v", l, err)
	}
	if code, m := post(t, local.URL+"/v1/servers", `{"name":"home2","url":"`+otherRemote.URL+`","code":"AAAA-BBBB"}`); code != 400 || !strings.Contains(m["error"].(string), "pairing refused") {
		t.Fatalf("a bad code: %d %v", code, m)
	}
	ss, _ := sv.servers()
	if len(ss) != 1 || ss[0].Name != "home" {
		t.Fatalf("the list reads the file: %+v", ss)
	}
	res := do(t, "DELETE", local.URL+"/v1/servers/home", "", auth)
	var d map[string]any
	json.NewDecoder(res.Body).Decode(&d)
	if res.StatusCode != 200 {
		t.Fatalf("remove: %d %v", res.StatusCode, d)
	}
	if l, _ := remote.Load(sv.ServersFile); len(l.Servers) != 0 {
		t.Fatalf("still saved: %+v", l)
	}
	if res := do(t, "POST", local.URL+"/v1/servers", `{"name":"x","url":"http://a","code":"c"}`, map[string]string{LoginHeader: "me@example.com"}); res.StatusCode != 403 {
		t.Fatalf("a tailnet login cannot add servers: %d", res.StatusCode)
	}
}
