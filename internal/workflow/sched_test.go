package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTickCronOnce(t *testing.T) {
	s := store(t)
	addSched(t, s, `{"name":"daily","workflow":"hello","cron":"0 17 * * *","inputs":{"x":"1"}}`)
	s.EnableSchedule("daily", at("2026-11-27 10:00"))
	f := &fake{shell: map[string]shellOut{"echo hi": {"hi\n", 0}}}
	sc := &Scheduler{Store: s, Env: f.env()}
	sc.Tick(context.Background(), at("2026-11-27 17:00"))
	sc.Wait()
	sc.Tick(context.Background(), at("2026-11-27 17:00").Add(30*time.Second))
	sc.Wait()
	h, _ := s.ScheduleHistory("daily", 5)
	if len(f.ran) != 1 || len(h) != 1 || h[0].Trigger != TriggerSchedule || h[0].Inputs["x"] != "1" {
		t.Fatalf("ran=%d %+v", len(f.ran), h)
	}
}

func TestTickGitHubNewEventsOnly(t *testing.T) {
	s := store(t)
	addSched(t, s, `{"name":"dep","workflow":"hello","github":{"event":"dependabot_alert"}}`)
	s.EnableSchedule("dep", time.Now())
	var mu sync.Mutex
	evs := []Event{{ID: "1", Title: "old"}}
	f := &fake{}
	sc := &Scheduler{Store: s, Env: f.env(), PollEvery: time.Nanosecond, GitHub: func(context.Context, string, GitHubTrigger) ([]Event, error) {
		mu.Lock()
		defer mu.Unlock()
		return evs, nil
	}}
	now := time.Now()
	sc.Tick(context.Background(), now)
	sc.Wait()
	mu.Lock()
	evs = append(evs, Event{ID: "2", Title: "lodash high"})
	mu.Unlock()
	sc.Tick(context.Background(), now.Add(time.Second))
	sc.Wait()
	sc.Tick(context.Background(), now.Add(2*time.Second))
	sc.Wait()
	h, _ := s.ScheduleHistory("dep", 5)
	if len(h) != 1 || h[0].Trigger != TriggerGitHub || !strings.Contains(h[0].Inputs["event"], "lodash high") || strings.Contains(h[0].Inputs["event"], "old") {
		t.Fatalf("%+v", h)
	}
}

func TestTickWatch(t *testing.T) {
	s := store(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1"), 0o600)
	addSched(t, s, `{"name":"w","workflow":"hello","watch":"`+dir+`"}`)
	s.EnableSchedule("w", time.Now())
	sc := &Scheduler{Store: s, Env: (&fake{}).env()}
	sc.Tick(context.Background(), time.Now())
	sc.Wait()
	sc.Tick(context.Background(), time.Now())
	sc.Wait()
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("2"), 0o600)
	sc.Tick(context.Background(), time.Now())
	sc.Wait()
	h, _ := s.ScheduleHistory("w", 5)
	if len(h) != 1 || h[0].Trigger != TriggerWatch || !strings.HasSuffix(h[0].Inputs["changed"], "b.txt") {
		t.Fatalf("%+v", h)
	}
}

func TestStartAndWebhook(t *testing.T) {
	s := store(t)
	os.WriteFile(filepath.Join(s.Dir, "greet.json"), []byte(`{"name":"greet","inputs":{"who":"x"},"outputs":{"o":"hi {{inputs.who}}"},"steps":[{"id":"a","kind":"set","vars":{"v":"1"}}]}`), 0o600)
	addSched(t, s, `{"name":"hook","workflow":"greet","webhook":true}`)
	sc := &Scheduler{Store: s, Env: (&fake{}).env()}
	r, err := sc.Start(context.Background(), "greet", Invocation{Trigger: "cli", Inputs: map[string]string{"who": "ana"}})
	if err != nil || r.Outputs["o"] != "hi ana" {
		t.Fatalf("%v %+v", err, r)
	}
	sch, _, _ := s.GetSchedule("hook")
	r, err = sc.Webhook(context.Background(), sch, map[string]string{"who": "bo", "evil": "1"})
	if err != nil || r.Outputs["o"] != "hi bo" || r.Inputs["evil"] != "" || r.Schedule != "hook" {
		t.Fatalf("%v %+v", err, r)
	}
	sc.claim("greet")
	if _, err := sc.Start(context.Background(), "greet", Invocation{}); err == nil {
		t.Fatal("busy")
	}
}
