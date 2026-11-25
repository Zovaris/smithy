//go:build !windows

package bench

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestInterruptRemovesTheTempRepos(t *testing.T) {
	codes := make(chan int, 1)
	old := exit
	exit = func(code int) { codes <- code; select {} }
	defer func() { exit = old }()
	work := make(chan string, 1)
	agent := func(tk Task, dir, prompt, log string) error {
		work <- filepath.Dir(filepath.Dir(dir))
		syscall.Kill(os.Getpid(), syscall.SIGINT)
		select {}
	}
	go Run(Options{Agent: "claude", Tasks: []string{"bug-sum-last"}, Out: t.TempDir()}, agent)
	dir := <-work
	select {
	case code := <-codes:
		if code != 130 {
			t.Fatalf("exit code %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("interrupt not handled")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("temp repos %s left behind", dir)
	}
}
