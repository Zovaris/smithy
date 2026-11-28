package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const simple = `{"name":"hello","steps":[{"id":"a","kind":"run","run":"echo hi"}]}`

func store(t *testing.T) Store {
	t.Helper()
	s := Store{Dir: t.TempDir()}
	os.WriteFile(filepath.Join(s.Dir, "hello.json"), []byte(simple), 0o600)
	return s
}

func addSched(t *testing.T, s Store, js string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "s.json")
	os.WriteFile(src, []byte(js), 0o600)
	if _, err := s.Add(src); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulePinsWorkflowAndCalls(t *testing.T) {
	s := store(t)
	os.WriteFile(filepath.Join(s.Dir, "outer.json"), []byte(`{"name":"outer","steps":[{"id":"c","kind":"call","workflow":"hello"}]}`), 0o600)
	addSched(t, s, `{"name":"daily","workflow":"outer","cron":"0 17 * * *"}`)
	l, _ := s.Schedules()
	if len(l) != 1 || l[0].Enabled {
		t.Fatalf("an added schedule starts disabled: %+v", l)
	}
	if err := s.EnableSchedule("daily", at("2026-11-27 10:00")); err != nil {
		t.Fatal(err)
	}
	if len(s.Due(at("2026-11-27 16:59"))) != 0 || len(s.Due(at("2026-11-27 17:00"))) != 1 {
		t.Fatal("due")
	}
	if l, _ = s.Schedules(); !l[0].Enabled || l[0].Next == nil {
		t.Fatalf("%+v", l[0])
	}
	os.WriteFile(filepath.Join(s.Dir, "hello.json"), []byte(strings.Replace(simple, "echo hi", "echo changed", 1)), 0o600)
	if l, _ = s.Schedules(); l[0].Enabled || !l[0].Changed || len(s.Due(at("2026-11-27 17:00"))) != 0 {
		t.Fatalf("a change to a called workflow must stop the schedule: %+v", l[0])
	}
	s.EnableSchedule("daily", at("2026-11-27 10:00"))
	w, _, _ := s.Get("outer")
	w.Description = "edited"
	s.Save(w)
	if l, _ = s.Schedules(); l[0].Enabled {
		t.Fatal("saving the workflow must stop its schedule")
	}
	s.EnableSchedule("daily", at("2026-11-27 10:00"))
	os.WriteFile(l[0].File, []byte(`{"name":"daily","workflow":"outer","cron":"* * * * *"}`), 0o600)
	if l, _ = s.Schedules(); l[0].Enabled {
		t.Fatal("editing the schedule file must stop it")
	}
	if wl, _ := s.List(); len(wl) != 2 || wl[1].Name != "outer" || wl[1].Schedules[0] != "daily" {
		t.Fatalf("list: %+v", wl)
	}
}

func TestScheduleValidate(t *testing.T) {
	bad := []string{
		`{"name":"x","workflow":"hello"}`,
		`{"name":"x","workflow":"hello","cron":"@daily","webhook":true}`,
		`{"name":"x","workflow":"../y","cron":"@daily"}`,
		`{"name":"x","workflow":"hello","github":{"event":"push"}}`,
		`{"name":"x","workflow":"hello","github":{"event":"issue_labeled"}}`,
		`{"name":"x","workflow":"hello","cron":"bad"}`,
	}
	for _, b := range bad {
		if _, err := ParseSchedule([]byte(b)); err == nil {
			t.Errorf("want error: %s", b)
		}
	}
	if _, err := Parse([]byte(`{"name":"x","schedule":"@daily","steps":[{"id":"a","kind":"run","run":"ls"}]}`)); err == nil || !strings.Contains(err.Error(), "moved to schedules") {
		t.Fatalf("legacy field: %v", err)
	}
}

func TestStoreHistoryBySchedule(t *testing.T) {
	s := store(t)
	s.Keep = 3
	addSched(t, s, `{"name":"daily","workflow":"hello","cron":"@daily"}`)
	for i := 0; i < 5; i++ {
		r := Record{Workflow: "hello", Started: at("2026-11-27 10:00").Add(time.Duration(i) * time.Minute), Status: StatusOK}
		if i%2 == 0 {
			r.Schedule = "daily"
		}
		s.Record(r)
	}
	h, _ := s.History("hello", 10)
	sh, _ := s.ScheduleHistory("daily", 10)
	if len(h) != 3 || !h[0].Started.Equal(at("2026-11-27 10:04")) || len(sh) != 2 {
		t.Fatalf("%d %d", len(h), len(sh))
	}
}

func TestMissedRunCatchUp(t *testing.T) {
	s := store(t)
	addSched(t, s, `{"name":"daily","workflow":"hello","cron":"0 17 * * *"}`)
	s.EnableSchedule("daily", at("2026-11-20 10:00"))
	if len(s.Due(at("2026-11-27 10:00"))) != 0 || len(s.Due(at("2026-11-27 17:30"))) != 1 {
		t.Fatal("catch-up")
	}
}

func TestHookToken(t *testing.T) {
	s := store(t)
	addSched(t, s, `{"name":"hook","workflow":"hello","webhook":true}`)
	addSched(t, s, `{"name":"cron","workflow":"hello","cron":"@daily"}`)
	if _, ok := s.CheckHook("hook", "x"); ok {
		t.Fatal("disabled schedule opened")
	}
	s.EnableSchedule("hook", time.Now())
	s.EnableSchedule("cron", time.Now())
	tok := s.HookToken("hook")
	if _, ok := s.CheckHook("hook", tok); !ok || len(tok) < 32 {
		t.Fatal("right token refused")
	}
	if _, ok := s.CheckHook("hook", tok+"x"); ok {
		t.Fatal("wrong token opened")
	}
	if _, ok := s.CheckHook("cron", s.HookToken("cron")); ok {
		t.Fatal("a cron schedule opened as a webhook")
	}
	s.DisableSchedule("hook")
	s.EnableSchedule("hook", time.Now())
	if s.HookToken("hook") != tok {
		t.Fatal("token should survive a re-enable")
	}
}

func TestMigrate(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	old := []byte(`{"name":"dep","schedule":"0 17 * * *","webhook":true,"steps":[{"id":"a","kind":"run","run":"ls"}]}`)
	os.WriteFile(filepath.Join(s.Dir, "dep.json"), old, 0o600)
	os.WriteFile(filepath.Join(s.Dir, "plain.json"), []byte(simple), 0o600)
	st := state{Enabled: map[string]string{"dep": hash(old)}, Hooks: map[string]string{"dep": "tok-old-123456"}}
	b, _ := json.Marshal(st)
	os.WriteFile(filepath.Join(s.Dir, "state.json"), b, 0o600)
	moved := s.Migrate(time.Now())
	if len(moved) != 1 || moved[0] != "dep" {
		t.Fatalf("moved %v", moved)
	}
	if _, _, err := s.Get("dep"); err != nil {
		t.Fatalf("workflow after migrate: %v", err)
	}
	l, _ := s.Schedules()
	if len(l) != 2 || l[0].Cron != "0 17 * * *" || !l[0].Enabled || !l[1].Webhook || !l[1].Enabled {
		t.Fatalf("schedules: %+v", l)
	}
	if s.HookToken("dep") != "tok-old-123456" {
		t.Fatal("token lost")
	}
	if again := s.Migrate(time.Now()); len(again) != 0 {
		t.Fatal("migrate twice")
	}
}
