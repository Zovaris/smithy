package workflow

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const simple = `{"name":"hello","schedule":"0 17 * * *","steps":[{"id":"a","kind":"run","run":"echo hi"}]}`

func TestStoreEnablePinsHash(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	src := filepath.Join(t.TempDir(), "hello.json")
	os.WriteFile(src, []byte(simple), 0o644)
	if _, err := s.Add(src); err != nil {
		t.Fatal(err)
	}
	l, _ := s.List()
	if len(l) != 1 || l[0].Enabled {
		t.Fatalf("added workflow must start disabled: %+v", l)
	}
	now := at("2026-11-27 10:00")
	if err := s.Enable("hello", now); err != nil {
		t.Fatal(err)
	}
	if l[0].Next != nil {
		t.Fatal("a disabled workflow has no next run")
	}
	if l, _ = s.List(); !l[0].Enabled || l[0].Next == nil {
		t.Fatal("not enabled")
	}
	if got := s.Due(at("2026-11-27 16:59")); len(got) != 0 {
		t.Fatalf("due early: %v", got)
	}
	if got := s.Due(at("2026-11-27 17:00")); len(got) != 1 {
		t.Fatalf("not due: %v", got)
	}
	os.WriteFile(filepath.Join(s.Dir, "hello.json"), []byte(simple[:len(simple)-1]+`,"description":"x"}`), 0o644)
	l, _ = s.List()
	if l[0].Enabled || !l[0].Changed {
		t.Fatalf("a changed file must stop running: %+v", l[0])
	}
	if got := s.Due(at("2026-11-27 17:00")); len(got) != 0 {
		t.Fatal("changed file is due")
	}
	s.Disable("hello")
	if l, _ = s.List(); l[0].Enabled || l[0].Changed {
		t.Fatalf("%+v", l[0])
	}
}

func TestStoreSaveTurnsOff(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	w, _ := Parse([]byte(simple))
	if err := s.Save(w); err != nil {
		t.Fatal(err)
	}
	s.Enable("hello", time.Now())
	w.Description = "edited in smithy"
	if err := s.Save(w); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.List(); l[0].Enabled || l[0].Changed {
		t.Fatalf("an edit needs a new enable: %+v", l[0])
	}
}

func TestStoreHistory(t *testing.T) {
	s := Store{Dir: t.TempDir(), Keep: 3}
	for i := 0; i < 5; i++ {
		r := Record{Workflow: "hello", Started: at("2026-11-27 10:00").Add(time.Duration(i) * time.Minute), Status: StatusOK}
		if err := s.Record(r); err != nil {
			t.Fatal(err)
		}
	}
	h, err := s.History("hello", 10)
	if err != nil || len(h) != 3 || !h[0].Started.Equal(at("2026-11-27 10:04")) {
		t.Fatalf("%v %d %+v", err, len(h), h)
	}
	l, _ := s.List()
	_ = l
}

func TestStoreMissedRunCatchUp(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	w, _ := Parse([]byte(simple))
	s.Save(w)
	s.Enable("hello", at("2026-11-20 10:00"))
	if got := s.Due(at("2026-11-27 10:00")); len(got) != 0 {
		t.Fatal("a run missed days ago must not fire at startup")
	}
	if got := s.Due(at("2026-11-27 17:30")); len(got) != 1 {
		t.Fatal("a run missed within the hour should fire")
	}
}

func TestStoreRejectsBadName(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, n := range []string{"../x", "a/b", ""} {
		if err := s.Enable(n, time.Now()); err == nil {
			t.Errorf("%q", n)
		}
	}
}
