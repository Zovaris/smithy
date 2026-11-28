// Package events keeps ~/.smithy/events.jsonl: a bounded, append-only log of what the forge did
// (polls, queued and skipped issues, launches, stage and CI changes, reports, merges), one JSON
// line each, for `smithy stats`. It never records issue bodies, keys or tokens.
package events

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/proof"
	"github.com/Zovaris/smithy/internal/text"
	"github.com/Zovaris/smithy/internal/usage"
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
	// Account is the pool account a run used (smithy pool), on kinds "usage" and "limited".
	Account string `json:"account,omitempty"`
	// Usage is set on kind "usage": what one finished agent run reported (tokens, cost when known).
	Usage *usage.Usage `json:"usage,omitempty"`
	// Agent, Model and Session name what reported a "usage" event, when known.
	Agent   string `json:"agent,omitempty"`
	Model   string `json:"model,omitempty"`
	Session string `json:"session,omitempty"`
}

// Spend is the known cost per local day of the "usage" events, the last days days (today last).
func Spend(es []Event, now time.Time, days int) []float64 {
	out := make([]float64, days)
	first := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
	for _, e := range es {
		if e.Kind != "usage" || e.Usage == nil || e.Usage.CostUSD == nil {
			continue
		}
		t := e.T.In(now.Location())
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
		if i := int(d.Sub(first).Hours()/24 + 0.5); !d.Before(first) && i < days {
			out[i] += *e.Usage.CostUSD
		}
	}
	return out
}

// MaxBytes is the size at which the log rotates to events.jsonl.1 (the one older file kept).
var MaxBytes int64 = 5 << 20

// Off turns Append into a no-op (set by callers that must not write, such as demo runs).
var Off = os.Getenv("SMITHY_EVENTS") == "off"

var mu sync.Mutex

// File is the log's path.
func File() string { return filepath.Join(layout.Home(), "events.jsonl") }

// flat keeps one short line (text.Clean), at most n runes.
func flat(s string, n int) string {
	s = text.Clean(s)
	if r := []rune(s); len(r) > n {
		s = string(r[:n-1]) + "…"
	}
	return s
}

// ProxyFile is the egress proxy's own log (asks, approvals, refusals): the only part of the smithy
// home the proxy sidecar can write. Read merges it in.
func ProxyFile() string { return filepath.Join(layout.Home(), "proxy", "events.jsonl") }

// Tap, when set, receives every event Append writes in this process (smithy daemon mirrors them
// into its store).
var Tap func(Event)

// Append writes one event; errors are returned but callers may ignore them (the log is advisory).
func Append(e Event) error {
	if Tap != nil && !Off {
		t := e
		if t.T.IsZero() {
			t.T = time.Now()
		}
		Tap(t)
	}
	return appendTo(File(), e)
}

// AppendProxy writes one event to ProxyFile.
func AppendProxy(e Event) error { return appendTo(ProxyFile(), e) }

func appendTo(f string, e Event) error {
	if Off {
		return nil
	}
	if e.T.IsZero() {
		e.T = time.Now()
	}
	e.T = e.T.UTC().Truncate(time.Second)
	e.Kind, e.Batch, e.Item, e.Source, e.ID = flat(e.Kind, 32), flat(e.Batch, 80), flat(e.Item, 80), flat(e.Source, 32), flat(e.ID, 80)
	e.PR, e.Detail, e.Account = flat(e.PR, 200), flat(proof.Redact(e.Detail), 160), flat(e.Account, 80)
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	if fi, err := os.Stat(f); err == nil && fi.Size()+int64(len(b)) > MaxBytes {
		if os.Rename(f, f+".1") == nil {
			_ = os.Chmod(f+".1", 0o600)
		}
	}
	fh, err := os.OpenFile(f, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_ = fh.Chmod(0o600)
	_, err = fh.Write(append(b, '\n'))
	if cerr := fh.Close(); err == nil {
		err = cerr
	}
	return err
}

// Read returns the events at or after since, oldest first, at most max (the newest kept).
func Read(since time.Time, max int) ([]Event, error) {
	var out []Event
	for _, f := range []string{File() + ".1", File(), ProxyFile() + ".1", ProxyFile()} {
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
	sort.SliceStable(out, func(i, j int) bool { return out[i].T.Before(out[j].T) })
	if max > 0 && len(out) > max {
		out = out[len(out)-max:]
	}
	return out, nil
}

// DecisionID is the id smithy daemon gives the decision e asks or answers ("" when none):
// secret:<id> for proxy asks, merge:<batch>/<item> for a ready item, decide:<batch>/<item>/<key>
// for a needs_decision item (key is the card's decision_key). Telegram shows the same id.
func DecisionID(e Event, key string) string {
	switch {
	case (e.Kind == "secret_ask" || e.Kind == "secret_approved" || e.Kind == "secret_denied") && e.ID != "":
		return "secret:" + e.ID
	case e.Kind == "stage" && e.Batch != "" && e.Item != "" && e.Detail == "ready":
		return "merge:" + e.Batch + "/" + e.Item
	case e.Kind == "stage" && e.Batch != "" && e.Item != "" && e.Detail == "needs_decision" && key != "":
		return "decide:" + e.Batch + "/" + e.Item + "/" + key
	}
	return ""
}
