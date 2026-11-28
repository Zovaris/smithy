package workflow

import (
	"context"
	"testing"
	"time"
)

func TestSchedulerTickRunsDueOnce(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	w, _ := Parse([]byte(simple))
	s.Save(w)
	s.Enable("hello", at("2026-11-27 10:00"))
	f := &fake{shell: map[string]struct {
		out  string
		exit int
	}{"echo hi": {"hi\n", 0}}}
	sc := &Scheduler{Store: s, Env: f.env()}
	sc.Tick(context.Background(), at("2026-11-27 17:00"))
	sc.Wait()
	sc.Tick(context.Background(), at("2026-11-27 17:00").Add(30*time.Second))
	sc.Wait()
	if len(f.ran) != 1 {
		t.Fatalf("ran %d times", len(f.ran))
	}
	h, _ := s.History("hello", 5)
	if len(h) != 1 || h[0].Trigger != "schedule" || h[0].Steps[0].Output != "hi\n" {
		t.Fatalf("%+v", h)
	}
}

func TestSchedulerRunNowDisabledAndBusy(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	w, _ := Parse([]byte(simple))
	s.Save(w)
	f := &fake{shell: map[string]struct {
		out  string
		exit int
	}{}}
	sc := &Scheduler{Store: s, Env: f.env()}
	r, err := sc.RunNow(context.Background(), "hello", "cli")
	if err != nil || r.Status != StatusOK || r.Trigger != "cli" {
		t.Fatalf("a disabled workflow may still run by hand: %v %+v", err, r)
	}
	sc.claim("hello")
	if _, err := sc.RunNow(context.Background(), "hello", "cli"); err == nil {
		t.Fatal("want busy error")
	}
}
