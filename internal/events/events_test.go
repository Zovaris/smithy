package events

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/usage"
)

func TestSpendSumsKnownCostPerLocalDay(t *testing.T) {
	now := time.Date(2026, 11, 23, 15, 0, 0, 0, time.Local)
	c1, c2 := 0.5, 1.25
	es := []Event{
		{T: now.Add(-time.Hour), Kind: "usage", Usage: &usage.Usage{CostUSD: &c1}},
		{T: now.Add(-2 * time.Hour), Kind: "usage", Usage: &usage.Usage{CostUSD: &c2}},
		{T: now.AddDate(0, 0, -1), Kind: "usage", Usage: &usage.Usage{CostUSD: &c1}},
		{T: now, Kind: "usage", Usage: &usage.Usage{Output: 9}},
		{T: now.AddDate(0, 0, -9), Kind: "usage", Usage: &usage.Usage{CostUSD: &c2}},
		{T: now, Kind: "launched"},
	}
	got := Spend(es, now, 3)
	if len(got) != 3 || got[2] != 1.75 || got[1] != 0.5 || got[0] != 0 {
		t.Fatalf("spend = %v", got)
	}
}

func TestAppendKeepsUsage(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	c := 0.3
	if err := Append(Event{Kind: "usage", Item: "a", Usage: &usage.Usage{Input: 5, CostUSD: &c}}); err != nil {
		t.Fatal(err)
	}
	es, _ := Read(time.Time{}, 0)
	if len(es) != 1 || es[0].Usage == nil || es[0].Usage.Input != 5 || *es[0].Usage.CostUSD != 0.3 {
		t.Fatalf("events = %+v", es)
	}
}

func TestAppendReadRotate(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	old := MaxBytes
	MaxBytes = 400
	defer func() { MaxBytes = old }()
	t0 := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		if err := Append(Event{T: t0.Add(time.Duration(i) * time.Minute), Kind: "queued", Source: "github", ID: "4\x1b[2J2", Detail: strings.Repeat("x", 500)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(File() + ".1"); err != nil {
		t.Fatal("no rotation")
	}
	fi, _ := os.Stat(File())
	if fi.Size() > MaxBytes {
		t.Fatalf("log is %d bytes", fi.Size())
	}
	es, err := Read(t0.Add(5*time.Minute), 0)
	if err != nil || len(es) == 0 || es[len(es)-1].T != t0.Add(9*time.Minute) {
		t.Fatalf("read = %v %v", es, err)
	}
	for _, e := range es {
		if strings.ContainsRune(e.ID, 0x1b) || len([]rune(e.Detail)) > 160 {
			t.Fatalf("not cleaned: %+v", e)
		}
	}
	if es, _ := Read(time.Time{}, 2); len(es) != 2 {
		t.Fatalf("max = %d", len(es))
	}
}

func TestAppendStripsWholeEscapeSequences(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	if err := Append(Event{Kind: "queued", ID: "4\x1b[2J2", Detail: "a\x1b]8;;http://x\x07b"}); err != nil {
		t.Fatal(err)
	}
	es, _ := Read(time.Time{}, 0)
	if len(es) != 1 || es[0].ID != "42" || es[0].Detail != "ab" {
		t.Fatalf("%+v", es)
	}
}

func TestAppendRedactsSecretsInDetail(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	tok := "xoxb-12345-" + strings.Repeat("c", 20)
	if err := Append(Event{Kind: "orchestrator_failed", Detail: "post failed with " + tok}); err != nil {
		t.Fatal(err)
	}
	es, _ := Read(time.Time{}, 0)
	if len(es) != 1 || strings.Contains(es[0].Detail, tok) {
		t.Fatalf("secret kept in the events log: %+v", es)
	}
}

func TestLogIsPrivateAfterOpenAndRotation(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	old := MaxBytes
	MaxBytes = 200
	defer func() { MaxBytes = old }()
	if err := os.MkdirAll(filepath.Dir(File()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(File(), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := Append(Event{Kind: "x", Detail: strings.Repeat("y", 100)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{File(), File() + ".1"} {
		fi, err := os.Stat(f)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", f, fi.Mode(), err)
		}
	}
}

func TestOff(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	Off = true
	defer func() { Off = false }()
	if err := Append(Event{Kind: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(File()); !os.IsNotExist(err) {
		t.Fatal("Off still wrote")
	}
}

func TestProxyEventsOwnFileMergedOnRead(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	now := time.Now()
	Append(Event{T: now, Kind: "launch"})
	if err := AppendProxy(Event{T: now.Add(-time.Hour), Kind: "secret_ask", ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ProxyFile()); err != nil || filepath.Dir(ProxyFile()) != filepath.Join(layout.Home(), "proxy") {
		t.Fatalf("proxy file %s: %v", ProxyFile(), err)
	}
	es, _ := Read(time.Time{}, 0)
	if len(es) != 2 || es[0].Kind != "secret_ask" || es[1].Kind != "launch" {
		t.Errorf("merged %+v", es)
	}
}

func TestTapSeesAppendedEvents(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	var got []Event
	Tap = func(e Event) { got = append(got, e) }
	defer func() { Tap = nil }()
	Append(Event{Kind: "serve_down", Detail: "x"})
	AppendProxy(Event{Kind: "proxy_ask"})
	if len(got) != 1 || got[0].Kind != "serve_down" || got[0].T.IsZero() {
		t.Fatalf("%+v", got)
	}
}
