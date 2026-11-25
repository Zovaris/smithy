package bench

import (
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Zovaris/smithy/internal/proc"
)

// exit ends the process after an interrupt; tests replace it.
var exit = os.Exit

var (
	runningMu sync.Mutex
	running   = map[*exec.Cmd]bool{}
)

// track runs cmd as its own process tree and remembers it, so an interrupt can kill it.
func track(cmd *exec.Cmd) func() {
	proc.Tree(cmd)
	runningMu.Lock()
	running[cmd] = true
	runningMu.Unlock()
	return func() {
		runningMu.Lock()
		delete(running, cmd)
		runningMu.Unlock()
	}
}

// onInterrupt kills the running agent and oracle trees, removes work (unless keep) and exits 130
// on SIGINT or SIGTERM. The returned stop ends the watch.
func onInterrupt(work string, keep bool) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
		case <-done:
			return
		}
		runningMu.Lock()
		for cmd := range running {
			_ = proc.Kill(cmd)
		}
		runningMu.Unlock()
		if !keep {
			os.RemoveAll(work)
		}
		exit(130)
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
