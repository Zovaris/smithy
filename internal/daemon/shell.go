//go:build harness

package daemon

import (
	"bytes"
	"context"
	"errors"
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
// escapes stripped, at most api.ShellMaxLines lines, api.ShellMaxBytes of output and shellTimeout.
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

// shellOut gathers both streams into lines, each line tagged with the stream that ended it.
type shellOut struct {
	mu    sync.Mutex
	lines []ShellLine
	part  [2][]byte
	n     int
	cut   bool
}

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
	if left := api.ShellMaxBytes - o.n; left < len(p) {
		p, o.cut = p[:max(left, 0)], true
	}
	o.n += len(p)
	i := 0
	if s.err {
		i = 1
	}
	for len(p) > 0 {
		j := bytes.IndexByte(p, '\n')
		if j < 0 {
			o.part[i] = append(o.part[i], p...)
			break
		}
		o.add(string(append(o.part[i], p[:j]...)), s.err)
		o.part[i], p = nil, p[j+1:]
	}
	return n, nil
}

// add keeps one line, cleaned of terminal escapes, while under api.ShellMaxLines.
func (o *shellOut) add(s string, err bool) {
	if len(o.lines) >= api.ShellMaxLines {
		o.cut = true
		return
	}
	s = strings.TrimRight(text.StripEscapes(strings.TrimRight(s, "\r"), true), " ")
	o.lines = append(o.lines, ShellLine{Text: s, Err: err})
}

func (o *shellOut) done() []ShellLine {
	for i, p := range o.part {
		if len(p) > 0 {
			o.add(string(p), i == 1)
		}
	}
	return o.lines
}
