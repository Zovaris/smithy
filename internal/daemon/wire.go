//go:build harness

package daemon

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/text"
)

// Tail follows one append-only JSONL file that rotates to <Path>.1 (events.jsonl and the
// proxy's log). Its position lives in the store's sync_cursors under Source, saved with each line.
type Tail struct {
	Path, Source string
}

// maxLine drops a longer line (events.Append writes far shorter ones).
const maxLine = 1 << 20

// head fingerprints a file by its first complete line ("" when it has none yet).
func head(f string) string {
	fh, err := os.Open(f)
	if err != nil {
		return ""
	}
	defer fh.Close()
	b, err := bufio.NewReaderSize(fh, 4096).ReadSlice('\n')
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

func parseCursor(c string) (int64, string) {
	off, h, _ := strings.Cut(c, " ")
	n, err := strconv.ParseInt(off, 10, 64)
	if err != nil || n < 0 {
		return 0, ""
	}
	return n, h
}

// TailOnce appends every complete new line of t to the store as an "event" and maps it to
// decisions (Ingest). After a rotation it finishes <Path>.1 from the saved offset first.
func (sv *Server) TailOnce(t *Tail) error {
	c, err := sv.st.Cursor(t.Source)
	if err != nil {
		return err
	}
	off, h := parseCursor(c)
	fi, err := os.Stat(t.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if h != "" && (head(t.Path) != h || fi.Size() < off) {
		if head(t.Path+".1") == h {
			if err := sv.readFrom(t, t.Path+".1", off, h, true); err != nil {
				return err
			}
		}
		off, h = 0, ""
	}
	return sv.readFrom(t, t.Path, off, h, false)
}

func (sv *Server) readFrom(t *Tail, file string, off int64, h string, rotated bool) error {
	fh, err := os.Open(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer fh.Close()
	if _, err := fh.Seek(off, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReaderSize(fh, 64<<10)
	var long bool
	for {
		b, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			off += int64(len(b))
			long = true
			continue
		}
		if err != nil {
			return nil
		}
		if off == 0 && !rotated {
			s := sha256.Sum256(b)
			h = hex.EncodeToString(s[:8])
		}
		off += int64(len(b))
		cur := strconv.FormatInt(off, 10) + " " + h
		var e events.Event
		if long || len(b) > maxLine || json.Unmarshal(b, &e) != nil || e.Kind == "" {
			long = false
			if _, err := sv.st.AppendCursor(nil, t.Source, cur); err != nil {
				return err
			}
			continue
		}
		if err := sv.commit(e, t.Source, cur); err != nil {
			return err
		}
		sv.Ingest(e, t.Source)
	}
}

func (sv *Server) commit(e events.Event, source, cursor string) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	sv.mu.Lock()
	defer sv.mu.Unlock()
	seq, err := sv.st.AppendCursor(&store.Event{Kind: "event", Data: b}, source, cursor)
	if err != nil {
		return err
	}
	sv.lastSeq = seq
	close(sv.wake)
	sv.wake = make(chan struct{})
	return nil
}

// RunTails polls every tail each interval until ctx ends.
func (sv *Server) RunTails(ctx context.Context, every time.Duration, ts ...*Tail) {
	tk := time.NewTicker(every)
	defer tk.Stop()
	for {
		for _, t := range ts {
			if err := sv.TailOnce(t); err != nil && sv.log != nil {
				sv.log("tail " + t.Source + ": " + err.Error())
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
	}
}

func readCard(batch, item string) map[string]any {
	if !cardID(batch) || !cardID(item) {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(layout.WardenHome(), batch, item+".json"))
	var c map[string]any
	if err != nil || json.Unmarshal(b, &c) != nil {
		return nil
	}
	return c
}

func cardID(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }

// Ingest maps one worker event to a daemon decision: a proxy secret_ask, a ready item (merge or
// hold) or a needs_decision item. A secret answered at the proxy closes its decision. Asking an
// id that exists (pending or answered) is a no-op, so a replayed line never re-asks.
// Secret kinds count only from the proxy's own log (source ProxySource): a worker that can write
// events.jsonl must not ask, approve or deny in the proxy's name.
func (sv *Server) Ingest(e events.Event, source string) {
	switch e.Kind {
	case "secret_ask":
		if source == ProxySource {
			if err := sv.Ask(events.DecisionID(e, ""), "Allow "+line1(e.Detail, 300)+"?", []string{"approve", "deny"}); err != nil {
				_ = events.AppendProxy(events.Event{Kind: "secret_denied", ID: e.ID, Detail: "daemon could not record the ask: " + text.Clean(err.Error())})
			}
		}
	case "secret_approved", "secret_denied":
		if source != ProxySource {
			return
		}
		ans := map[string]string{"secret_approved": "approve", "secret_denied": "deny"}[e.Kind]
		sv.closeAnswered(events.DecisionID(e, ""), ans, "proxy")
	case "stage":
		card := readCard(e.Batch, e.Item)
		switch e.Detail {
		case "ready":
			pr := e.PR
			if pr == "" {
				pr = str(card, "pr")
			}
			if pr == "" {
				return
			}
			_ = sv.Ask(events.DecisionID(e, ""), fmt.Sprintf("Merge %s (%s/%s)?", line1(pr, 200), line1(e.Batch, 80), line1(e.Item, 80)), []string{"merge", "hold"})
		case "needs_decision":
			id := events.DecisionID(e, str(card, "decision_key"))
			if id == "" || str(card, "question") == "" {
				return
			}
			_ = sv.Ask(id, line1(str(card, "question"), 500), options(card["options"]))
		}
	}
}

// options keeps a card's options only when every one is a plain string (else the answer is free text).
func options(v any) []string {
	l, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, o := range l {
		s, ok := o.(string)
		if !ok || s == "" {
			return nil
		}
		out = append(out, line1(s, 80))
	}
	return out
}

// ProxySource is the tail of the proxy's log, the only source of secret asks and answers.
const ProxySource = "tail:proxy"

// line1 is untrusted text made one printable line (no escapes or newlines), at most n runes.
func line1(s string, n int) string {
	s = text.Clean(s)
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}

// closeAnswered records an answer the asker already has (it came from the asker), so it is never routed.
func (sv *Server) closeAnswered(id, answer, by string) {
	if id == "" {
		return
	}
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if d := sv.decisions[id]; d == nil || d.Outcome != nil {
		return
	}
	if o, err := sv.answerLocked(id, answer, by); err == nil && o.Won {
		sv.markRoutedLocked(id)
	}
}

func routable(id string) bool {
	return strings.HasPrefix(id, "secret:") || strings.HasPrefix(id, "merge:") || strings.HasPrefix(id, "decide:") || strings.HasPrefix(id, "intake:")
}

// RouteBack hands an answer to its asker: the proxy's approve/deny event for a secret (the path
// smithy proxy approve|deny takes, same id), the item's card (answer, answer_id, answered_by) for
// warden items.
func RouteBack(d Decision, o Outcome) error {
	kind, rest, _ := strings.Cut(d.ID, ":")
	by := text.Clean(o.By)
	switch kind {
	case "secret":
		k := map[string]string{"approve": "secret_approved", "deny": "secret_denied"}[o.Answer]
		if k == "" {
			return fmt.Errorf("secret answer must be approve or deny, got %q", o.Answer)
		}
		return events.AppendProxy(events.Event{Kind: k, ID: rest, Detail: "answered by " + by})
	case "merge", "decide":
		parts := strings.Split(rest, "/")
		if len(parts) < 2 {
			return fmt.Errorf("bad decision id %q", d.ID)
		}
		ans := text.Clean(o.Answer)
		if r := []rune(ans); len(r) > 500 {
			ans = string(r[:500])
		}
		q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
		_, err := cards.Set(parts[0], parts[1], []string{"answer=" + q(ans), "answer_id=" + q(d.ID), "answered_by=" + q(by)})
		return err
	}
	return nil
}
