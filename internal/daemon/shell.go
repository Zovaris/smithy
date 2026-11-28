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

	"github.com/Zovaris/smithy/internal/text"
)

// ShellLine is one line a `!cmd` printed; Err marks a stderr line.
type ShellLine struct {
	Text string `json:"text"`
	Err  bool   `json:"err,omitempty"`
}

// ShellRun is a finished `!cmd`: its lines in the order printed, the exit code (-1 when it never
// ran or was killed, with Err set), how long it took and whether the output was cut.
type ShellRun struct {
	Lines []ShellLine
	Code  int
	Err   string
	Took  time.Duration
	Cut   bool
}

// Output is the run's lines joined, the cut note last.
func (r ShellRun) Output() string {
	var b strings.Builder
	for i, l := range r.Lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l.Text)
	}
	if r.Cut {
		b.WriteString("\n… output cut at 256 KiB")
	}
	return b.String()
}

// RunShell runs line with the user's shell in dir, stdout and stderr kept apart line by line,
// at most shellMaxBytes of output and shellTimeout of time.
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
	run := ShellRun{Took: time.Since(start), Lines: o.done(), Cut: o.cut}
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
	if left := shellMaxBytes - o.n; left < len(p) {
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
		o.lines = append(o.lines, ShellLine{Text: string(append(o.part[i], p[:j]...)), Err: s.err})
		o.part[i], p = nil, p[j+1:]
	}
	return n, nil
}

func (o *shellOut) done() []ShellLine {
	for i, p := range o.part {
		if len(p) > 0 {
			o.lines = append(o.lines, ShellLine{Text: string(p), Err: i == 1})
		}
	}
	return o.lines
}
