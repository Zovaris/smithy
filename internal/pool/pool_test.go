package pool

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func home(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("SMITHY_HOME", d)
	t.Setenv("SMITHY_EVENTS", "")
	return d
}

func TestAcquirePicksLeastRecentlyUsedAndSkipsLimited(t *testing.T) {
	home(t)
	for _, n := range []string{"a", "b", "c"} {
		if _, err := Add(n, "claude", ""); err != nil {
			t.Fatal(err)
		}
	}
	Add("x", "codex", "")
	now := time.Now()
	var got []string
	for i := 0; i < 3; i++ {
		a, err := Acquire("claude", now.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, a.Name)
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("order = %v", got)
	}
	MarkLimited("a", now.Add(time.Hour))
	if a, _ := Acquire("claude", now.Add(5*time.Minute)); a.Name != "b" {
		t.Fatalf("got %s, want b (a limited, b least recent)", a.Name)
	}
	MarkLimited("b", now.Add(3*time.Hour))
	MarkLimited("c", now.Add(2*time.Hour))
	_, err := Acquire("claude", now.Add(6*time.Minute))
	var lim *Limited
	if !errors.As(err, &lim) || !lim.Until.Equal(now.Add(time.Hour).UTC()) {
		t.Fatalf("err = %v", err)
	}
	if a, _ := Acquire("pool", now); a.Name != "x" {
		t.Fatalf("pool picked %s, want the codex account", a.Name)
	}
	if _, err := Acquire("gemini", now); !errors.Is(err, ErrNone) {
		t.Fatalf("err = %v", err)
	}
	if a, _ := Find("x"); a.Env() != "CODEX_HOME="+a.Dir {
		t.Fatalf("env = %s", a.Env())
	}
}

func TestAddRejectsUnknownHarnessAndDuplicates(t *testing.T) {
	h := home(t)
	if _, err := Add("a", "gemini", ""); err == nil {
		t.Fatal("gemini accepted")
	}
	a, err := Add("a", "claude", "")
	if err != nil || a.Dir != filepath.Join(h, "accounts", "a") {
		t.Fatalf("%+v %v", a, err)
	}
	if fi, err := os.Stat(a.Dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir %v %v", fi, err)
	}
	if _, err := Add("a", "claude", ""); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := Remove("a"); err != nil {
		t.Fatal(err)
	}
	if as, _ := Load(); len(as) != 0 {
		t.Fatalf("accounts = %v", as)
	}
}

func TestReset(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	for text, want := range map[string]time.Time{
		"You've hit your limit · resets 3pm (UTC)":                         time.Date(2026, 11, 23, 15, 0, 0, 0, time.UTC),
		"5-hour limit reached ∙ resets 9am (UTC)":                          time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC),
		"You've hit your usage limit. Try again at 10:30 AM.":              time.Date(2026, 11, 23, 10, 30, 0, 0, time.Local),
		"usage limit reached":                                              now.Add(DefaultLimit),
		"You've hit your usage limit, try again at Nov 24th, 2026 1:05 PM": time.Date(2026, 11, 24, 13, 5, 0, 0, time.Local),
	} {
		got := Reset(text, now)
		if text == "You've hit your usage limit, try again at Nov 24th, 2026 1:05 PM" {
			if got.Hour() != 13 || got.Minute() != 5 {
				t.Errorf("%q → %v", text, got)
			}
			continue
		}
		if want.Location() == time.Local && !want.After(now) {
			want = want.AddDate(0, 0, 1)
		}
		if !got.Equal(want) {
			t.Errorf("%q → %v, want %v", text, got, want)
		}
	}
}

func TestDetectOnlyCountsErrorResults(t *testing.T) {
	d := t.TempDir()
	log := filepath.Join(d, "l")
	now := time.Now()
	os.WriteFile(log, []byte(`{"type":"result","is_error":false,"result":"I explained what to do when you hit your usage limit"}`+"\n"), 0o600)
	if _, ok := Detect(log, "claude", 0, now); ok {
		t.Fatal("a successful answer mentioning limits counted")
	}
	os.WriteFile(log, []byte(`{"type":"result","is_error":true,"result":"Claude AI usage limit reached"}`+"\n"), 0o600)
	if u, ok := Detect(log, "claude", 1, now); !ok || !u.Equal(now.Add(DefaultLimit)) {
		t.Fatalf("claude limit: %v %v", u, ok)
	}
	os.WriteFile(log, []byte("working\nERROR: You've hit your usage limit. Try again at 4:15 PM.\n"), 0o600)
	if _, ok := Detect(log, "codex", 0, now); ok {
		t.Fatal("a successful text run counted")
	}
	if u, ok := Detect(log, "codex", 1, now); !ok || u.Hour() != 16 || u.Minute() != 15 {
		t.Fatalf("codex limit: %v %v", u, ok)
	}
}

func TestDetectIgnoresRateLimitInATestLog(t *testing.T) {
	log := filepath.Join(t.TempDir(), "l")
	now := time.Now()
	os.WriteFile(log, []byte("--- FAIL: TestRetry (0.01s)\n    client_test.go:40: got error rate_limit, want nil\nFAIL\nexit status 1\n"), 0o600)
	for _, k := range []string{"claude", "codex", "opencode"} {
		if _, ok := Detect(log, k, 1, now); ok {
			t.Errorf("%s: a failing test log mentioning rate_limit counted", k)
		}
	}
	os.WriteFile(log, []byte(`{"type":"result","is_error":true,"result":"API Error: rate_limit_error"}`+"\n"), 0o600)
	if _, ok := Detect(log, "claude", 1, now); ok {
		t.Error("bare rate_limit in a claude error counted")
	}
	os.WriteFile(log, []byte("ERROR: You've hit your usage limit.\nline\nline\nline\nline\nline\nline\n"), 0o600)
	if _, ok := Detect(log, "codex", 1, now); ok {
		t.Error("codex limit sentence far from the end counted")
	}
}

func TestResetDateWeekdayAndUnknownZone(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC) // a Monday
	if got := Reset("resets Nov 28, 3pm (UTC)", now); !got.Equal(time.Date(2026, 11, 28, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("date form → %v", got)
	}
	if got := Reset("resets Wed 9am (UTC)", now); !got.Equal(time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("weekday form → %v", got)
	}
	if got := Reset("resets Monday 8am (UTC)", now); !got.Equal(time.Date(2026, 11, 30, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("past weekday → %v", got)
	}
	var warned []string
	old := Warn
	Warn = func(s string) { warned = append(warned, s) }
	defer func() { Warn = old }()
	if got := Reset("resets 3pm (Mars/Olympus)", now); !got.Equal(now.Add(DefaultLimit)) || len(warned) != 1 {
		t.Errorf("unknown zone → %v, warnings %q", got, warned)
	}
}

func TestAddRejectsUnsafeNamesAndDirs(t *testing.T) {
	h := home(t)
	for _, n := range []string{".", "..", ".hidden", "-x", "a/b", `a\b`, ""} {
		if _, err := Add(n, "claude", ""); err == nil {
			t.Errorf("name %q accepted", n)
		}
	}
	if _, err := Add("ok", "claude", h+"/x/../y"); err == nil {
		t.Error("dir with .. accepted")
	}
	loose := filepath.Join(h, "loose")
	os.MkdirAll(loose, 0o755)
	if _, err := Add("ok", "claude", loose); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(loose); fi.Mode().Perm() != 0o700 {
		t.Errorf("existing dir mode %v, want 0700", fi.Mode().Perm())
	}
}

func TestMainListAndLogin(t *testing.T) {
	h := home(t)
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\necho \"login $1 $CODEX_HOME\"\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var out, errb bytes.Buffer
	if rc := Main([]string{"list"}, &out, &errb); rc != 0 || !strings.Contains(out.String(), "no accounts") {
		t.Fatalf("rc %d out %q", rc, out.String())
	}
	if rc := Main([]string{"add", "cx", "codex"}, &out, &errb); rc != 0 {
		t.Fatalf("add rc %d: %s", rc, errb.String())
	}
	out.Reset()
	if rc := Main([]string{"login", "cx"}, &out, &errb); rc != 0 || !strings.Contains(out.String(), "login login "+filepath.Join(h, "accounts", "cx")) {
		t.Fatalf("login rc %d out %q err %q", rc, out.String(), errb.String())
	}
	MarkLimited("cx", time.Now().Add(time.Hour))
	out.Reset()
	if rc := Main([]string{"list"}, &out, &errb); rc != 0 || !strings.Contains(out.String(), "limited") {
		t.Fatalf("list %q", out.String())
	}
	if rc := Main([]string{"clear", "cx"}, &out, &errb); rc != 0 {
		t.Fatal(errb.String())
	}
	if !State()["cx"].LimitedUntil.IsZero() {
		t.Fatal("limit not cleared")
	}
	if rc := Main([]string{"bogus"}, &out, &errb); rc != 2 {
		t.Fatalf("bogus rc %d", rc)
	}
}
