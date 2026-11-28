//go:build harness

package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/text"
)

// ShellLine and ShellRun are the wire types of a `!cmd` run.
type (
	ShellLine = api.ShellLine
	ShellRun  = api.ShellRun
)

// RunShell runs line with the user's shell in dir, stdout and stderr kept apart line by line,
// escapes stripped, the head and tail of at most api.ShellMaxLines lines and api.ShellMaxBytes, and
// at most shellTimeout.
func RunShell(ctx context.Context, dir, line string) ShellRun {
	ctx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, sh, "-c", line)
	cmd.Dir = dir
	o := &shellOut{}
	cmd.Stdout, cmd.Stderr = o.stream(false), o.stream(true)
	start := time.Now()
	err := cmd.Run()
	run := ShellRun{Cmd: line, Dir: dir, TookMS: time.Since(start).Milliseconds(), Lines: o.done()}
	run.Cut = o.cut
	var ee *exec.ExitError
	switch {
	case ctx.Err() != nil:
		run.Code, run.Err = -1, "killed after "+shellTimeout.String()
	case errors.As(err, &ee):
		run.Code = ee.ExitCode()
	case err != nil:
		run.Code, run.Err = -1, text.Clean(err.Error())
	}
	return run
}

// shellOut gathers both streams into lines, each line tagged with the stream that ended it. It
// keeps the first and the last half of api.ShellMaxLines and api.ShellMaxBytes; done puts one
// "… N lines cut …" line between them.
type shellOut struct {
	mu       sync.Mutex
	head     []ShellLine
	tail     []ShellLine
	headB    int
	tailB    int
	headDone bool
	dropped  int
	part     [2][]byte
	over     [2]bool
	cut      bool
}

const (
	shellHalfLines = api.ShellMaxLines / 2
	shellHalfBytes = api.ShellMaxBytes / 2
)

type shellStream struct {
	o   *shellOut
	err bool
}

func (o *shellOut) stream(err bool) *shellStream { return &shellStream{o: o, err: err} }

func (s *shellStream) Write(p []byte) (int, error) {
	o := s.o
	o.mu.Lock()
	defer o.mu.Unlock()
	n := len(p)
	i := 0
	if s.err {
		i = 1
	}
	for len(p) > 0 {
		j := bytes.IndexByte(p, '\n')
		k := j
		if j < 0 {
			k = len(p)
		}
		room := max(shellHalfBytes-len(o.part[i]), 0)
		o.part[i] = append(o.part[i], p[:min(room, k)]...)
		o.over[i] = o.over[i] || k > room
		if j < 0 {
			break
		}
		o.flush(i)
		p = p[j+1:]
	}
	return n, nil
}

// add keeps one line, cleaned of terminal escapes: in the head while it has room, else in the
// tail, which drops its oldest lines past its half.
func (o *shellOut) add(s string, err bool) {
	s = strings.TrimRight(text.StripEscapes(strings.TrimRight(s, "\r"), true), " ")
	l := ShellLine{Text: s, Err: err}
	if !o.headDone && len(o.head) < shellHalfLines && o.headB+len(s) <= shellHalfBytes {
		o.head, o.headB = append(o.head, l), o.headB+len(s)
		return
	}
	o.headDone = true
	o.tail, o.tailB = append(o.tail, l), o.tailB+len(s)
	for len(o.tail) > shellHalfLines || o.tailB > shellHalfBytes {
		o.tailB -= len(o.tail[0].Text)
		o.tail = o.tail[1:]
		o.dropped++
	}
}

// flush ends stream i's line; one past the byte cap ends in "…" and marks the run cut.
func (o *shellOut) flush(i int) {
	t := string(o.part[i])
	if o.over[i] {
		t, o.cut = t+"…", true
	}
	o.add(t, i == 1)
	o.part[i], o.over[i] = nil, false
}

func (o *shellOut) done() []ShellLine {
	for i, p := range o.part {
		if len(p) > 0 || o.over[i] {
			o.flush(i)
		}
	}
	lines := o.head
	if o.dropped > 0 {
		o.cut = true
		lines = append(lines, ShellLine{Text: fmt.Sprintf("… %d lines cut …", o.dropped)})
	}
	return append(lines, o.tail...)
}
