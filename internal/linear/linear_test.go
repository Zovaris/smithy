package linear

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/config"
)

const testKey = "lin_api_SECRET123"

type call struct {
	Auth      string
	Query     string
	Variables map[string]any
}

// fake serves recorded responses in order and records each request.
func fake(t *testing.T, responses ...string) *[]call {
	t.Helper()
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var c call
		json.Unmarshal(b, &c)
		c.Auth = r.Header.Get("Authorization")
		calls = append(calls, c)
		w.Header().Set("X-RateLimit-Requests-Remaining", "42")
		if len(calls) > len(responses) {
			t.Errorf("unexpected request %d: %s", len(calls), c.Query)
			w.Write([]byte(`{"data":{}}`))
			return
		}
		w.Write([]byte(responses[len(calls)-1]))
	}))
	t.Cleanup(srv.Close)
	old := Endpoint
	Endpoint = srv.URL
	t.Cleanup(func() { Endpoint = old })
	return &calls
}

func setup(t *testing.T, settings map[string]string) *bytes.Buffer {
	t.Helper()
	h := t.TempDir()
	t.Setenv("SMITHY_HOME", h)
	t.Setenv("SMITHY_CONFIG", filepath.Join(h, "config.json"))
	t.Setenv("LINEAR_API_KEY", testKey)
	t.Chdir(t.TempDir())
	for k, v := range settings {
		if err := config.Set(config.UserFile(), k, v); err != nil {
			t.Fatal(err)
		}
	}
	var w bytes.Buffer
	old := Warn
	Warn, warnOnce = &w, &sync.Once{}
	t.Cleanup(func() { Warn = old })
	return &w
}

func open(t *testing.T) *Source {
	t.Helper()
	s, err := Open(auto.Settings{Mode: "labeled", Label: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const page1 = `{"data":{"issues":{"nodes":[
 {"id":"uuid-1","identifier":"ENG-1","title":"One","description":"body 1","url":"https://linear.app/x/issue/ENG-1","team":{"key":"ENG"},"labels":{"nodes":[{"name":"auto"},{"name":"model:cheap"}]}},
 {"id":"uuid-9","identifier":"SEC-9","title":"Other team","description":"x","url":"https://linear.app/x/issue/SEC-9","team":{"key":"SEC"},"labels":{"nodes":[{"name":"auto"}]}}
],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}`

const page2 = `{"data":{"issues":{"nodes":[
 {"id":"uuid-2","identifier":"ENG-2","title":"Two","description":"","url":"https://linear.app/x/issue/ENG-2","team":{"key":"ENG"},"labels":{"nodes":[]}}
],"pageInfo":{"hasNextPage":false,"endCursor":"c2"}}}}`

func TestPollPagesFiltersAndDropsOtherTeams(t *testing.T) {
	warn := setup(t, map[string]string{"linear.team": "ENG", "linear.project": "Core", "linear.milestone": "M1",
		"linear.cycle": "current", "linear.initiative": "Growth"})
	calls := fake(t, page1, page2)
	since := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got, err := open(t).Poll(since)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "uuid-1" || got[0].ID != "ENG-1" || got[1].ID != "ENG-2" {
		t.Fatalf("issues = %+v", got)
	}
	if strings.Join(got[0].Labels, ",") != "auto,model:cheap" || got[0].Body != "body 1" {
		t.Errorf("issue 1 = %+v", got[0])
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %d", len(*calls))
	}
	c := (*calls)[0]
	if c.Auth != testKey {
		t.Errorf("Authorization = %q, want the raw key", c.Auth)
	}
	f, _ := json.Marshal(c.Variables["filter"])
	for _, want := range []string{
		`"team":{"key":{"in":["ENG"]}}`,
		`"state":{"type":{"in":["unstarted","backlog"]}}`,
		`"project":{"initiatives":{"some":{"name":{"in":["Growth"]}}},"name":{"in":["Core"]}}`,
		`"projectMilestone":{"name":{"in":["M1"]}}`,
		`"cycle":{"isActive":{"eq":true}}`,
		`"updatedAt":{"gt":"2026-01-02T03:02:05Z"}`,
	} {
		if !strings.Contains(string(f), want) {
			t.Errorf("filter %s lacks %s", f, want)
		}
	}
	if c.Variables["first"] != float64(50) || c.Variables["after"] != nil {
		t.Errorf("page 1 vars = %v", c.Variables)
	}
	if (*calls)[1].Variables["after"] != "c1" {
		t.Errorf("page 2 after = %v", (*calls)[1].Variables["after"])
	}
	if strings.Count(warn.String(), "rate limit low") != 1 {
		t.Errorf("want one rate-limit warning, got %q", warn.String())
	}
	if strings.Contains(warn.String(), testKey) {
		t.Error("key printed")
	}
}

func TestCycleNumberAndFirstPollWindow(t *testing.T) {
	setup(t, map[string]string{"linear.team": "ENG", "linear.cycle": "7"})
	defer func(f func() time.Time) { now = f }(now)
	now = func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }
	f, _ := json.Marshal(open(t).Filter(time.Time{}))
	if !strings.Contains(string(f), `"cycle":{"number":{"eq":7}}`) || !strings.Contains(string(f), `"updatedAt":{"gt":"2026-01-03T00:00:00Z"}`) || strings.Contains(string(f), "project") {
		t.Errorf("filter = %s", f)
	}
}

func TestMissingTeamRefuses(t *testing.T) {
	setup(t, nil)
	_, err := Open(auto.Settings{})
	if err == nil || !errors.Is(err, auto.ErrNoAccess) || !strings.Contains(err.Error(), "linear.team") {
		t.Fatalf("err = %v", err)
	}
	if _, code := auto.PollLines(auto.Settings{Mode: "labeled", Label: "auto"}, "linear"); code != 3 {
		t.Errorf("poll exit = %d, want 3", code)
	}
}

func TestMissingKeyNamesEnv(t *testing.T) {
	setup(t, map[string]string{"linear.team": "ENG", "linear.key_env": "MY_LINEAR"})
	t.Setenv("MY_LINEAR", "")
	_, err := Open(auto.Settings{})
	if err == nil || !errors.Is(err, auto.ErrNoAccess) || !strings.Contains(err.Error(), "MY_LINEAR") || !strings.Contains(err.Error(), "smithy auth set linear") {
		t.Fatalf("err = %v", err)
	}
}

func TestKeyRejectedNeverInError(t *testing.T) {
	setup(t, map[string]string{"linear.team": "ENG"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"errors":[{"message":"bad key ` + testKey + `"}]}`))
	}))
	defer srv.Close()
	Endpoint, srv.URL = srv.URL, Endpoint
	defer func() { Endpoint = srv.URL }()
	_, err := open(t).Poll(time.Time{})
	if err == nil || !errors.Is(err, auto.ErrNoAccess) || strings.Contains(err.Error(), testKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestReportMutations(t *testing.T) {
	setup(t, map[string]string{"linear.team": "ENG"})
	calls := fake(t,
		`{"data":{"issue":{"id":"uuid-1","team":{"key":"ENG"}}}}`,
		`{"data":{"workflowStates":{"nodes":[{"id":"s-todo","name":"Todo","type":"unstarted","position":0},{"id":"s-shipped","name":"Shipped","type":"completed","position":5},{"id":"s-done","name":"Done","type":"completed","position":2}]}}}`,
		`{"data":{"issueUpdate":{"success":true}}}`,
		`{"data":{"attachmentLinkURL":{"success":true}}}`,
		`{"data":{"commentCreate":{"success":true}}}`,
	)
	if err := open(t).Report("ENG-1", "completed", "https://github.com/o/r/pull/5"); err != nil {
		t.Fatal(err)
	}
	cs := *calls
	if len(cs) != 5 {
		t.Fatalf("calls = %d", len(cs))
	}
	in := func(i int) map[string]any { m, _ := cs[i].Variables["input"].(map[string]any); return m }
	if f, _ := json.Marshal(cs[1].Variables); !strings.Contains(cs[1].Query, "workflowStates") || !strings.Contains(string(f), `"team":"ENG"`) {
		t.Errorf("states = %+v", cs[1])
	}
	if !strings.Contains(cs[2].Query, "issueUpdate") || cs[2].Variables["id"] != "uuid-1" || in(2)["stateId"] != "s-done" {
		t.Errorf("update = %+v", cs[2])
	}
	if !strings.Contains(cs[3].Query, "attachmentLinkURL") || cs[3].Variables["url"] != "https://github.com/o/r/pull/5" || cs[3].Variables["issueId"] != "uuid-1" {
		t.Errorf("attach = %+v", cs[3])
	}
	if !strings.Contains(cs[4].Query, "commentCreate") || in(4)["issueId"] != "uuid-1" || !strings.Contains(in(4)["body"].(string), "pull/5") {
		t.Errorf("comment = %+v", cs[4])
	}
}

func TestReportNoCommentWhenStateMoveFails(t *testing.T) {
	setup(t, map[string]string{"linear.team": "ENG"})
	calls := fake(t,
		`{"data":{"issue":{"id":"uuid-1","team":{"key":"ENG"}}}}`,
		`{"data":{"workflowStates":{"nodes":[{"id":"s1","type":"started","position":1}]}}}`,
		`{"errors":[{"message":"boom"}]}`,
	)
	if err := open(t).Report("ENG-1", "started", ""); err == nil || len(*calls) != 3 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func TestPaginationStops(t *testing.T) {
	setup(t, map[string]string{"linear.team": "ENG"})
	stall := `{"data":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"same"}}}}`
	calls := fake(t, stall, stall, stall)
	if _, err := open(t).Poll(time.Time{}); err != nil || len(*calls) != 2 {
		t.Errorf("stalled cursor: err %v calls %d", err, len(*calls))
	}
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Write([]byte(`{"data":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"c` + strings.Repeat("x", n) + `"}}}}`))
	}))
	defer srv.Close()
	Endpoint = srv.URL
	if _, err := open(t).Poll(time.Time{}); err != nil || n != maxPages {
		t.Errorf("page cap: err %v pages %d", err, n)
	}
}

func TestTeamsUppercasedAndStates(t *testing.T) {
	setup(t, map[string]string{"linear.team": "eng, ops", "linear.states": ""})
	s := open(t)
	f, _ := json.Marshal(s.Filter(time.Time{}))
	if !strings.Contains(string(f), `"key":{"in":["ENG","OPS"]}`) || !strings.Contains(string(f), `"type":{"in":["unstarted","backlog"]}`) || !s.allowed("eng") {
		t.Errorf("filter = %s", f)
	}
	config.Set(config.UserFile(), "linear.states", "unstarted,bogus")
	if _, err := Open(auto.Settings{}); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("bad state: %v", err)
	}
}

func TestRateWarningOncePerProcess(t *testing.T) {
	warn := setup(t, map[string]string{"linear.team": "ENG"})
	empty := `{"data":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`
	fake(t, empty, empty)
	open(t).Poll(time.Time{})
	open(t).Poll(time.Time{})
	if strings.Count(warn.String(), "rate limit low") != 1 {
		t.Errorf("warnings = %q", warn.String())
	}
}

func TestReportBlockedCommentsOnly(t *testing.T) {
	setup(t, map[string]string{"linear.team": "ENG"})
	calls := fake(t, `{"data":{"issue":{"id":"uuid-1","team":{"key":"ENG"}}}}`, `{"data":{"commentCreate":{"success":true}}}`)
	if err := open(t).Report("ENG-1", "blocked", ""); err != nil || len(*calls) != 2 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func TestReportRefusesOtherTeam(t *testing.T) {
	setup(t, map[string]string{"linear.team": "ENG"})
	calls := fake(t, `{"data":{"issue":{"id":"uuid-9","team":{"key":"SEC"}}}}`)
	if err := open(t).Report("SEC-9", "started", ""); err == nil || len(*calls) != 1 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func TestRegistered(t *testing.T) {
	if _, ok := auto.Sources["linear"]; !ok {
		t.Error("linear not in auto.Sources")
	}
}
