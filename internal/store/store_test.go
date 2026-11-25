//go:build harness

package store

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
)

func open(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAppendSinceSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smithy.db")
	s := open(t, path)
	for i, kind := range []string{"a", "b", "c"} {
		seq, err := s.Append(Event{Kind: kind, Session: "s1", Data: json.RawMessage(`{"n":1}`)})
		if err != nil {
			t.Fatal(err)
		}
		if seq != int64(i+1) {
			t.Fatalf("seq %d, want %d", seq, i+1)
		}
	}
	s.Close()

	s = open(t, path)
	got, err := s.Since(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Seq != 2 || got[0].Kind != "b" || got[1].Seq != 3 {
		t.Fatalf("Since(1) = %+v", got)
	}
	if got[0].Session != "s1" || string(got[0].Data) != `{"n":1}` || got[0].Time.IsZero() {
		t.Fatalf("fields lost: %+v", got[0])
	}
	if all, _ := s.Since(0); len(all) != 3 {
		t.Fatalf("Since(0) = %d events", len(all))
	}
	if none, _ := s.Since(3); len(none) != 0 {
		t.Fatalf("Since(3) = %d events", len(none))
	}
}

func TestWALAndMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smithy.db")
	s := open(t, path)
	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode %q, %v", mode, err)
	}
	v, err := s.Version()
	if err != nil || v != len(migrations) {
		t.Fatalf("version %d, %v; want %d", v, err, len(migrations))
	}
	s.Close()
	s = open(t, path)
	if v2, _ := s.Version(); v2 != v {
		t.Fatalf("reopen re-ran migrations: %d", v2)
	}
}

func TestEmptyDataIsNull(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "smithy.db"))
	if _, err := s.Append(Event{Kind: "k"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Since(0)
	if string(got[0].Data) != "null" {
		t.Fatalf("data %q", got[0].Data)
	}
	if _, err := s.Append(Event{Kind: "k", Data: json.RawMessage("{bad")}); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestConcurrentAppendsGetUniqueSeqs(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "smithy.db"))
	var wg sync.WaitGroup
	seen := sync.Map{}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seq, err := s.Append(Event{Kind: "k"})
			if err != nil {
				t.Error(err)
				return
			}
			if _, dup := seen.LoadOrStore(seq, true); dup {
				t.Errorf("duplicate seq %d", seq)
			}
		}()
	}
	wg.Wait()
}
