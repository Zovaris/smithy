package notify

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/events"
)

const token = "123456:ABCdefGHIjklMNOpqrSTUvwxYZ0123456789"

type fake struct {
	mu   sync.Mutex
	msgs []url.Values
	path []string
	fail int
}

func setup(t *testing.T) (*fake, *httptest.Server) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMITHY_HOME", home)
	t.Setenv("SMITHY_CONFIG", filepath.Join(home, "config.json"))
	t.Setenv("TELEGRAM_API_KEY", token)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"notify":{"telegram":{"chat":"42"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fake{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		v, _ := url.ParseQuery(string(b))
		f.mu.Lock()
		defer f.mu.Unlock()
		f.path = append(f.path, r.URL.Path)
		if f.fail > 0 {
			f.fail--
			w.WriteHeader(401)
			fmt.Fprintf(w, `{"ok":false,"description":"Unauthorized bot%s"}`, token)
			return
		}
		f.msgs = append(f.msgs, v)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	old := APIBase
	APIBase = srv.URL
	t.Cleanup(func() { APIBase = old })
	return f, srv
}

func appendEv(t *testing.T, e events.Event) {
	t.Helper()
	if err := events.Append(e); err != nil {
		t.Fatal(err)
	}
}

func TestSendPostsToBotPath(t *testing.T) {
	f, _ := setup(t)
	if err := Test(); err != nil {
		t.Fatal(err)
	}
	if len(f.msgs) != 1 || f.msgs[0].Get("chat_id") != "42" || !strings.Contains(f.msgs[0].Get("text"), "TEST") {
		t.Fatalf("msgs %v", f.msgs)
	}
	if f.path[0] != "/bot"+token+"/sendMessage" {
		t.Fatalf("path %q", f.path[0])
	}
}

func TestErrorsNeverCarryTheToken(t *testing.T) {
	f, _ := setup(t)
	f.fail = 1
	err := Test()
	if err == nil || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "ABCdef") {
		t.Fatalf("err %v", err)
	}
	APIBase = "http://127.0.0.1:1"
	err = Test()
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("err %v", err)
	}
}

func TestFirstFlushStartsAtNowThenMapsEvents(t *testing.T) {
	f, _ := setup(t)
	appendEv(t, events.Event{T: time.Now().Add(-time.Hour), Kind: "stage", Batch: "b", Item: "old", Detail: "ready"})
	n := New(nil)
	n.Flush(time.Now())
	if len(f.msgs) != 0 {
		t.Fatalf("history sent: %v", f.msgs)
	}
	appendEv(t, events.Event{Kind: "stage", Batch: "b", Item: "T-1", PR: "https://github.com/o/r/pull/1", Detail: "ready"})
	appendEv(t, events.Event{Kind: "stage", Batch: "b", Item: "T-2", Detail: "working"})
	appendEv(t, events.Event{Kind: "stage", Batch: "b", Item: "T-3", Detail: "blocked"})
	appendEv(t, events.Event{Kind: "stage", Batch: "b", Item: "T-1", PR: "https://github.com/o/r/pull/1", Detail: "ready"})
	appendEv(t, events.Event{Kind: "queued", ID: "T-9", Detail: "secret body text"})
	n.Flush(time.Now().Add(time.Second))
	if len(f.msgs) != 2 {
		t.Fatalf("want 2 msgs (ready once, blocked), got %d: %v", len(f.msgs), f.msgs)
	}
	m := f.msgs[0].Get("text")
	if !strings.Contains(m, "PR ready") || !strings.Contains(m, "T-1") || !strings.Contains(f.msgs[0].Get("reply_markup"), "pull/1") {
		t.Fatalf("msg %q", m)
	}
	n.Flush(time.Now().Add(2 * time.Second))
	if len(f.msgs) != 2 {
		t.Fatalf("resent: %d", len(f.msgs))
	}
}

func TestLimitedNotifiesOnlyWhenEveryAccountIs(t *testing.T) {
	f, _ := setup(t)
	n := New(nil)
	n.Flush(time.Now())
	appendEv(t, events.Event{Kind: "limited", Account: "a", Detail: "until 2026-11-23T15:00:00Z"})
	appendEv(t, events.Event{Kind: "limited", Detail: "every pool account is limited; earliest reset 2026-11-23 15:00"})
	n.Flush(time.Now().Add(time.Second))
	if len(f.msgs) != 1 || !strings.Contains(f.msgs[0].Get("text"), "earliest reset 2026-11-23 15:00") {
		t.Fatalf("msgs %v", f.msgs)
	}
}

func TestKindsToggle(t *testing.T) {
	f, _ := setup(t)
	home := os.Getenv("SMITHY_HOME")
	os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"notify":{"telegram":{"chat":"42"},"kinds":"blocked"}}`), 0o600)
	n := New(nil)
	n.Flush(time.Now())
	appendEv(t, events.Event{Kind: "stage", Item: "T-1", Detail: "ready"})
	appendEv(t, events.Event{Kind: "stage", Item: "T-2", Detail: "failed"})
	n.Flush(time.Now().Add(time.Second))
	if len(f.msgs) != 1 || !strings.Contains(f.msgs[0].Get("text"), "T-2") {
		t.Fatalf("msgs %v", f.msgs)
	}
}

func TestRateLimitBatchesOverflow(t *testing.T) {
	f, _ := setup(t)
	n := New(nil)
	n.Flush(time.Now())
	for i := 0; i < 30; i++ {
		appendEv(t, events.Event{Kind: "stage", Item: fmt.Sprintf("T-%d", i), Detail: "blocked"})
	}
	n.Flush(time.Now().Add(time.Second))
	if len(f.msgs) != PerMinute {
		t.Fatalf("want %d msgs, got %d", PerMinute, len(f.msgs))
	}
	last := f.msgs[len(f.msgs)-1].Get("text")
	if !strings.Contains(last, "11 more") {
		t.Fatalf("summary %q", last)
	}
	appendEv(t, events.Event{Kind: "stage", Item: "T-99", Detail: "blocked"})
	n.Flush(time.Now().Add(2 * time.Second))
	if len(f.msgs) != PerMinute {
		t.Fatalf("sent past the limit: %d", len(f.msgs))
	}
	n.Flush(time.Now().Add(2 * time.Minute))
	if len(f.msgs) != PerMinute+1 {
		t.Fatalf("held message not sent after the window: %d", len(f.msgs))
	}
}

func TestFailureLoggedOnceAndUnconfiguredIsQuiet(t *testing.T) {
	f, _ := setup(t)
	var logs []string
	n := New(func(s string) { logs = append(logs, s) })
	n.Flush(time.Now())
	f.fail = 5
	appendEv(t, events.Event{Kind: "stage", Item: "T-1", Detail: "blocked"})
	appendEv(t, events.Event{Kind: "stage", Item: "T-2", Detail: "blocked"})
	n.Flush(time.Now().Add(time.Second))
	if len(logs) != 1 || strings.Contains(logs[0], token) {
		t.Fatalf("logs %v", logs)
	}
	st := Status()
	if !strings.Contains(strings.Join(st, "\n"), "last error") {
		t.Fatalf("status %v", st)
	}
	t.Setenv("TELEGRAM_API_KEY", "")
	logs = nil
	appendEv(t, events.Event{Kind: "stage", Item: "T-3", Detail: "blocked"})
	n.Flush(time.Now().Add(2 * time.Second))
	if len(logs) != 0 {
		t.Fatalf("unconfigured logged: %v", logs)
	}
}

func TestOutageRetriesNextCycle(t *testing.T) {
	f, _ := setup(t)
	var logs []string
	n := New(func(s string) { logs = append(logs, s) })
	t0 := time.Now()
	n.Flush(t0)
	f.fail = 1
	appendEv(t, events.Event{Kind: "stage", Item: "T-1", Detail: "blocked"})
	appendEv(t, events.Event{Kind: "stage", Item: "T-2", Detail: "ready"})
	n.Flush(t0.Add(time.Second))
	if len(f.msgs) != 0 {
		t.Fatalf("sent during outage: %v", f.msgs)
	}
	n.Flush(t0.Add(time.Minute))
	if len(f.msgs) != 2 {
		t.Fatalf("want both alerts after the outage, got %d", len(f.msgs))
	}
	n.Flush(t0.Add(2 * time.Minute))
	if len(f.msgs) != 2 {
		t.Fatalf("resent: %d", len(f.msgs))
	}
}

func TestRetryDroppedAfterADay(t *testing.T) {
	f, _ := setup(t)
	var logs []string
	n := New(func(s string) { logs = append(logs, s) })
	t0 := time.Now()
	n.Flush(t0)
	f.fail = 100
	appendEv(t, events.Event{Kind: "stage", Item: "T-1", Detail: "blocked"})
	n.Flush(t0.Add(time.Second))
	n.Flush(t0.Add(25 * time.Hour))
	f.fail = 0
	n.Flush(t0.Add(26 * time.Hour))
	if len(f.msgs) != 0 {
		t.Fatalf("expired alert sent: %v", f.msgs)
	}
	dropped := 0
	for _, l := range logs {
		if strings.Contains(l, "dropped") {
			dropped++
		}
	}
	if dropped != 1 {
		t.Fatalf("logs %v", logs)
	}
}

func TestFailedSendDoesNotUseRateWindow(t *testing.T) {
	f, _ := setup(t)
	n := New(nil)
	t0 := time.Now()
	n.Flush(t0)
	f.fail = 1
	appendEv(t, events.Event{Kind: "stage", Item: "T-1", Detail: "blocked"})
	n.Flush(t0.Add(time.Second))
	st, _ := load()
	if len(st.Window) != 0 {
		t.Fatalf("window %v", st.Window)
	}
}

func TestStatusWarnsUnknownKinds(t *testing.T) {
	setup(t)
	home := os.Getenv("SMITHY_HOME")
	os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"notify":{"telegram":{"chat":"42"},"kinds":"blocked,nope"}}`), 0o600)
	s := strings.Join(Status(), "\n")
	if !strings.Contains(s, "unknown notify.kinds: nope") {
		t.Fatalf("status %q", s)
	}
}

func TestStatusMasksToken(t *testing.T) {
	setup(t)
	s := strings.Join(Status(), "\n")
	if strings.Contains(s, token) || !strings.Contains(s, "chat 42") {
		t.Fatalf("status %q", s)
	}
}

func TestMessageTrimsTitle(t *testing.T) {
	m, _ := render(item{kind: "pr_ready", key: "T-1", title: strings.Repeat("x", 200), pr: "https://x/pull/1", reason: strings.Repeat("y", 500)})
	for _, l := range strings.Split(m, "\n") {
		if len([]rune(l)) > 240 {
			t.Fatalf("long line %q", l)
		}
	}
	if n := strings.Count(m, "\n"); n > 11 {
		t.Fatalf("%d lines", n+1)
	}
}

func TestMessageEscapesUntrustedText(t *testing.T) {
	m, _ := render(item{kind: "blocked", key: "<b>1</b>", title: `</i><a href="x">hi & bye`, reason: "<script>"})
	for _, bad := range []string{"<script>", "<a href", "</i><a", "<b>1</b>"} {
		if strings.Contains(m, bad) {
			t.Fatalf("unescaped %q in %q", bad, m)
		}
	}
	if !strings.Contains(m, "&lt;script&gt;") || !strings.Contains(m, "hi &amp; bye") || !strings.HasPrefix(m, "<code>BLOCKED</code>") {
		t.Fatalf("msg %q", m)
	}
}

func TestButtonsOnlyHTTPURLs(t *testing.T) {
	_, kb := render(item{kind: "pr_ready", key: "1", pr: "https://github.com/o/r/pull/1", issue: "javascript:alert(1)"})
	if len(kb) != 1 || kb[0].Text != "Open PR" || kb[0].URL != "https://github.com/o/r/pull/1" {
		t.Fatalf("buttons %+v", kb)
	}
	_, kb = render(item{kind: "pr_ready", key: "1", pr: "ftp://x/1", issue: "https://github.com/o/r/issues/1"})
	if len(kb) != 1 || kb[0].Text != "Open issue" {
		t.Fatalf("buttons %+v", kb)
	}
}

func TestSendUsesHTMLNoPreviewAndKeyboard(t *testing.T) {
	f, _ := setup(t)
	n := New(nil)
	n.Flush(time.Now())
	appendEv(t, events.Event{Kind: "stage", Batch: "b", Item: "T-1", PR: "https://github.com/o/r/pull/1", Detail: "ready"})
	n.Flush(time.Now().Add(time.Second))
	if len(f.msgs) != 1 {
		t.Fatalf("msgs %v", f.msgs)
	}
	v := f.msgs[0]
	if v.Get("parse_mode") != "HTML" || !strings.Contains(v.Get("link_preview_options"), `"is_disabled":true`) ||
		!strings.Contains(v.Get("reply_markup"), `"url":"https://github.com/o/r/pull/1"`) {
		t.Fatalf("form %v", v)
	}
	if !strings.Contains(v.Get("text"), "o/r") {
		t.Fatalf("repo missing: %q", v.Get("text"))
	}
}

func TestDecisionShowsQuestionAndHowToAnswer(t *testing.T) {
	setup(t)
	m, _ := render(item{kind: "decision", key: "7", question: "Use <Postgres> or SQLite?"})
	if !strings.Contains(m, "Use &lt;Postgres&gt; or SQLite?") || !strings.Contains(m, "smithy decisions") || !strings.Contains(m, "Needs you") {
		t.Fatalf("msg %q", m)
	}
}

func TestTestAllSendsEveryKind(t *testing.T) {
	f, _ := setup(t)
	if err := TestAll(); err != nil {
		t.Fatal(err)
	}
	if len(f.msgs) != len(Kinds)+1 || len(f.msgs) > 10 {
		t.Fatalf("sent %d", len(f.msgs))
	}
}

func TestRenderPlainDefault(t *testing.T) {
	c := 1.84
	msg, kb := render(item{kind: "pr_ready", key: "#44", title: "Add <dark mode>", repo: "acme/web", stage: "ready", model: "strong", speed: "normal", cost: &c, pr: "https://github.com/acme/web/pull/44"})
	for _, want := range []string{"<code>READY</code>", "Add &lt;dark mode&gt;", "<code>acme/web · ready · strong/normal · $1.84</code>"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q in %s", want, msg)
		}
	}
	if strings.ContainsAny(msg, "✅📁🧠") || len(kb) != 1 {
		t.Fatalf("plain style: %s %v", msg, kb)
	}
}

func TestClassifyExported(t *testing.T) {
	a, ok := Classify(events.Event{Kind: "stage", Batch: "b", Item: "i", Detail: "failed", PR: "javascript:x"})
	if !ok || a.Kind != "blocked" || a.Label != "Blocked" || a.PR != "" || a.Dedupe == "" {
		t.Fatalf("%+v %v", a, ok)
	}
	if _, ok := Classify(events.Event{Kind: "usage"}); ok {
		t.Fatal("usage is no alert")
	}
}
