// Package events keeps ~/.smithy/events.jsonl: a bounded, append-only log of what the forge did
// (polls, queued and skipped issues, launches, stage and CI changes, reports, merges), one JSON
// line each, for `smithy tui`. It never records issue bodies, keys or tokens.
package events

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/ghx"
)

// Event is one line of the log.
type Event struct {
	T      time.Time `json:"t"`
	Kind   string    `json:"kind"`
	Batch  string    `json:"batch,omitempty"`
	Item   string    `json:"item,omitempty"`
	Source string    `json:"source,omitempty"`
	ID     string    `json:"id,omitempty"`
	PR     string    `json:"pr,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

// MaxBytes is the size at which the log rotates to events.jsonl.1 (the one older file kept).
var MaxBytes int64 = 5 << 20

// Off turns Append into a no-op (set by callers that must not write, such as demo runs).
var Off = os.Getenv("SMITHY_EVENTS") == "off"

var mu sync.Mutex

// File is the log's path.
func File() string { return filepath.Join(ghx.Home(), "events.jsonl") }

// flat keeps one short line: control characters dropped, at most n runes.
func flat(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n-1]) + "…"
	}
	return s
}

// Append writes one event; errors are returned but callers may ignore them (the log is advisory).
func Append(e Event) error {
	if Off {
		return nil
	}
	if e.T.IsZero() {
		e.T = time.Now()
	}
	e.T = e.T.UTC().Truncate(time.Second)
	e.Kind, e.Batch, e.Item, e.Source, e.ID = flat(e.Kind, 32), flat(e.Batch, 80), flat(e.Item, 80), flat(e.Source, 32), flat(e.ID, 80)
	e.PR, e.Detail = flat(e.PR, 200), flat(e.Detail, 160)
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	f := File()
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	if fi, err := os.Stat(f); err == nil && fi.Size()+int64(len(b)) > MaxBytes {
		_ = os.Rename(f, f+".1")
	}
	fh, err := os.OpenFile(f, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = fh.Write(append(b, '\n'))
	if cerr := fh.Close(); err == nil {
		err = cerr
	}
	return err
}

// Read returns the events at or after since, oldest first, at most max (the newest kept).
func Read(since time.Time, max int) ([]Event, error) {
	var out []Event
	for _, f := range []string{File() + ".1", File()} {
		fh, err := os.Open(f)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return out, err
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var e Event
			if json.Unmarshal(sc.Bytes(), &e) != nil || e.T.Before(since) {
				continue
			}
			out = append(out, e)
		}
		fh.Close()
	}
	if max > 0 && len(out) > max {
		out = out[len(out)-max:]
	}
	return out, nil
}
