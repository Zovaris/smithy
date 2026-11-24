package wait

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/cards"
)

func TestBatchWatcherWakesOnDeadWorker(t *testing.T) {
	sandbox(t)
	writeCard(t, "b1", "ok", `{"state":"working","updated":"`+stamp(0)+`"}`)
	w, err := newBatch("b1", false)
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(cards.Home(), "b1", "a.log")
	out, err := os.Create(log)
	if err != nil {
		t.Fatal(err)
	}
	fake := exec.Command("sh", "-c", "echo 'error: invalid model x'; exit 1")
	fake.Stdout = out
	_ = fake.Run()
	out.Close()
	if err := cards.Launch("b1", "a", fake.Process.Pid); err != nil {
		t.Fatal(err)
	}
	got, _ := w.poll()
	if len(got) != 1 || !strings.HasPrefix(got[0], "batch b1 a  stuck") || !strings.Contains(got[0], "worker exited: error: invalid model x") {
		t.Fatalf("got %q", got)
	}
	again, _ := newBatch("b1", false)
	if got, _ := again.poll(); got != nil {
		t.Errorf("a new wait woke again on the reaped card: %q", got)
	}
}

func TestBatchWatcherKeepsLiveAndReportedWorkers(t *testing.T) {
	sandbox(t)
	live := exec.Command("sleep", "30")
	if err := live.Start(); err != nil {
		t.Fatal(err)
	}
	defer live.Process.Kill()
	if err := cards.Launch("b1", "live", live.Process.Pid); err != nil {
		t.Fatal(err)
	}
	gone := exec.Command("true")
	_ = gone.Run()
	writeCard(t, "b1", "done", `{"state":"pr_open","pr":"u","pid":`+strconv.Itoa(gone.Process.Pid)+`,"updated":"`+stamp(0)+`"}`)
	w, err := newBatch("b1", false)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := w.poll(); got != nil {
		t.Errorf("got %q", got)
	}
}
