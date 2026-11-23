package events

import (
	"os"
	"strings"
	"testing"
	"time"
)

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
