//go:build harness

package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/auto"
)

// DefaultIdleExit is daemon.idle_exit's default: a background daemon with no clients and no
// running work exits after this long.
const DefaultIdleExit = "30m"

// ParseIdle reads daemon.idle_exit: a Go duration ("30m", "2h"); "0" or "off" never exits.
func ParseIdle(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "0" || strings.EqualFold(s, "off") {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("daemon.idle_exit takes a duration like 30m, or 0/off (got %q)", s)
	}
	return d, nil
}

func (sv *Server) touch() { sv.seen.Store(time.Now().UnixNano()) }

// Clients is how many event streams (TUI, web, attach) are open.
func (sv *Server) Clients() int { return int(sv.clients.Load()) }

// WatchIdle calls stop once the daemon has had no open stream, no API request and no work
// (busy false, no planning) for limit, checking every tick. limit 0 never stops; ctx ends it.
func (sv *Server) WatchIdle(ctx context.Context, limit, tick time.Duration, busy func() bool, stop func()) {
	if limit <= 0 {
		<-ctx.Done()
		return
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if sv.clients.Load() > 0 || sv.planning.Load() || sv.SessionsWorking() || busy() {
			sv.touch()
			continue
		}
		if time.Since(time.Unix(0, sv.seen.Load())) >= limit {
			stop()
			return
		}
	}
}

// Ensure returns the live daemon, or starts one; fresh is true when it was just started.
func Ensure(running func() *Info, start func() (*Info, error)) (i *Info, fresh bool, err error) {
	if i := running(); i != nil {
		return i, false, nil
	}
	i, err = start()
	if err != nil {
		return nil, false, err
	}
	if i == nil {
		return nil, false, errors.New("daemon started but wrote no daemon.json")
	}
	return i, true, nil
}

// Stop is `smithy stop`: it ends the local daemon and waits up to wait for it to exit. With none
// running it says so and succeeds.
func Stop(w io.Writer, wait time.Duration) int {
	i := Running()
	if i == nil {
		fmt.Fprintln(w, "smithy stop: no daemon running")
		return 0
	}
	if err := terminate(i.PID); err != nil {
		fmt.Fprintf(w, "smithy stop: pid %d: %v\n", i.PID, err)
		return 1
	}
	deadline := time.Now().Add(wait)
	for auto.AliveAs(i.PID, i.PIDStart) {
		if time.Now().After(deadline) {
			fmt.Fprintf(w, "smithy stop: pid %d did not exit within %s; kill it with: kill -9 %d\n", i.PID, wait, i.PID)
			return 1
		}
		time.Sleep(50 * time.Millisecond)
	}
	if cur, _ := ReadInfo(); cur != nil && cur.PID == i.PID {
		os.Remove(InfoFile())
	}
	fmt.Fprintf(w, "smithy daemon stopped (pid %d)\n", i.PID)
	return 0
}

// StopUsage is the text for `smithy stop --help`.
const StopUsage = `usage: smithy stop
Stops the local background daemon (the one bare smithy starts, or smithy daemon started detached), waiting up to
10s for it to exit. Running workers keep going; the next smithy starts a new daemon. With no
daemon running it says so and exits 0.`
