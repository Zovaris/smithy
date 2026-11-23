// Package notify sends Telegram messages about events.jsonl: a PR ready, an item blocked, stuck or
// waiting on a decision, an orchestrator failure, serve stopping and the daily cap. The binary
// sends them (serve, after each cycle), never an agent. A message holds the kind, the item key
// and title (80 runes), the PR url and a short reason; never an issue body or a key. Each
// (item, kind) is sent once; at most PerMinute messages a minute, the overflow folded into one
// summary. State (cursor, sent keys, last error) is in ~/.smithy/notify.json.
package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/text"
)

// Usage is the text for `smithy notify --help`.
const Usage = `usage: smithy notify test | status
Telegram notifications, sent by smithy auto serve from events.jsonl. Setup:
  smithy auth set telegram            (the bot token from @BotFather, on stdin)
  smithy config set notify.telegram.chat <chat id>
  smithy config set notify.kinds pr_ready,blocked,stuck,decision,orchestrator_failed,serve_down,cap_reached
test sends one test message; status shows the token (masked), chat, kinds, last send and last error.`

// Kinds are every notification kind, in the default notify.kinds order.
var Kinds = []string{"pr_ready", "blocked", "stuck", "decision", "orchestrator_failed", "serve_down", "cap_reached"}

var label = map[string]string{"pr_ready": "PR ready", "blocked": "blocked", "stuck": "stuck", "decision": "decision pending",
	"orchestrator_failed": "orchestrator failed", "serve_down": "serve stopped", "cap_reached": "daily cap reached"}

// APIBase is the Telegram Bot API origin (a var for tests only; never a setting, so the token
// can't be sent elsewhere by a config edit).
var APIBase = "https://api.telegram.org"

// PerMinute caps messages sent in any 60 s.
const PerMinute = 20

var (
	tokenRe = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)
	chatRe  = regexp.MustCompile(`^(-?[0-9]+|@[A-Za-z][A-Za-z0-9_]{4,})$`)
	client  = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
)

// File is the state file.
func File() string { return filepath.Join(ghx.Home(), "notify.json") }

type state struct {
	Cursor   time.Time            `json:"cursor"`
	Sent     map[string]time.Time `json:"sent"`
	Window   []time.Time          `json:"window,omitempty"`
	LastSent time.Time            `json:"last_sent,omitempty"`
	LastErr  string               `json:"last_error,omitempty"`
	ErrAt    time.Time            `json:"error_at,omitempty"`
}

func load() (state, bool) {
	var s state
	b, err := os.ReadFile(File())
	ok := err == nil && json.Unmarshal(b, &s) == nil
	if s.Sent == nil {
		s.Sent = map[string]time.Time{}
	}
	return s, ok
}

func (s state) save() error {
	b, _ := json.MarshalIndent(s, "", "  ")
	f := File()
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}

// creds returns the bot token and chat id; ok is false when either is missing or malformed.
func creds() (tok, chat string, err error) {
	tok, _, err = auth.Get("telegram")
	if err != nil {
		return "", "", err
	}
	chat = strings.TrimSpace(config.Get("notify.telegram.chat", ""))
	switch {
	case tok == "":
		return "", "", errors.New("no bot token (smithy auth set telegram)")
	case !tokenRe.MatchString(tok):
		return "", "", errors.New("the stored telegram token is not a bot token (<digits>:<letters>)")
	case chat == "":
		return "", "", errors.New("no chat (smithy config set notify.telegram.chat <id>)")
	case !chatRe.MatchString(chat):
		return "", "", fmt.Errorf("notify.telegram.chat must be a numeric chat id or @channel, got %q", text.Clean(chat))
	}
	return tok, chat, nil
}

// scrub removes the token from s and keeps one short line.
func scrub(s, tok string) string {
	if tok != "" {
		s = strings.ReplaceAll(s, tok, "<token>")
		if i := strings.IndexByte(tok, ':'); i > 0 {
			s = strings.ReplaceAll(s, tok[i+1:], "<token>")
		}
	}
	return trim(text.Clean(s), 120)
}

func trim(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// send posts one message. Errors never contain the token.
func send(tok, chat, msg string) error {
	form := url.Values{"chat_id": {chat}, "text": {msg}, "disable_web_page_preview": {"true"}}
	resp, err := client.PostForm(APIBase+"/bot"+tok+"/sendMessage", form)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("telegram: %s", scrub(err.Error(), tok))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	var r struct {
		OK   bool   `json:"ok"`
		Desc string `json:"description"`
	}
	if json.Unmarshal(b, &r) != nil || !r.OK {
		return fmt.Errorf("telegram: HTTP %d %s", resp.StatusCode, scrub(r.Desc, tok))
	}
	return nil
}

// Test sends one test message.
func Test() error {
	tok, chat, err := creds()
	if err != nil {
		return err
	}
	return send(tok, chat, "smithy · test\nnotifications reach this chat")
}

// enabled is the set of kinds notify.kinds turns on.
func enabled() map[string]bool {
	on := map[string]bool{}
	for _, k := range strings.Split(config.Get("notify.kinds", strings.Join(Kinds, ",")), ",") {
		on[strings.TrimSpace(k)] = true
	}
	return on
}

type item struct{ kind, key, title, pr, reason, dedupe string }

func message(it item) string {
	lines := []string{"smithy · " + label[it.kind]}
	if h := strings.TrimSpace(trim(text.Clean(it.key), 40) + "  " + trim(text.Clean(it.title), 80)); h != "" {
		lines = append(lines, h)
	}
	if it.pr != "" {
		lines = append(lines, trim(text.Clean(it.pr), 120))
	}
	if it.reason != "" {
		lines = append(lines, trim(text.Clean(it.reason), 100))
	}
	return strings.Join(lines, "\n")
}

// classify maps one event to a notification, or ok false.
func classify(e events.Event) (item, bool) {
	key := e.Item
	if key == "" {
		key = e.ID
	}
	it := item{key: key, pr: e.PR, reason: e.Detail}
	switch {
	case e.Kind == "stage" && e.Detail == "ready":
		it.kind = "pr_ready"
	case e.Kind == "stage" && (e.Detail == "blocked" || e.Detail == "failed"):
		it.kind = "blocked"
	case e.Kind == "stage" && e.Detail == "needs_decision":
		it.kind = "decision"
	case e.Kind == "orchestrator_failed", e.Kind == "serve_down":
		it.kind = e.Kind
		it.dedupe = e.Kind + "|" + e.T.Format(time.RFC3339)
		return it, true
	case e.Kind == "cap_reached":
		it.kind = e.Kind
		it.dedupe = e.Kind + "|" + e.T.Local().Format("2006-01-02")
		return it, true
	default:
		return it, false
	}
	it.dedupe = it.kind + "|" + e.Batch + "|" + key + "|" + e.PR
	return it, true
}

// stuck lists items whose worker stopped beating, from the newest batches.
func stuck() []item {
	var out []item
	b := cards.Batches()
	if len(b) > 5 {
		b = b[:5]
	}
	for _, batch := range b {
		rows, err := cards.Rows(batch)
		if err != nil {
			continue
		}
		for _, r := range rows {
			if r.Stage != "stuck" || r.State == "blocked" || r.State == "failed" {
				continue
			}
			beat := ""
			if r.LastBeat != nil {
				beat = *r.LastBeat
			}
			out = append(out, item{kind: "stuck", key: r.ID, pr: r.PR, reason: "no heartbeat since " + r.Beat,
				dedupe: "stuck|" + batch + "|" + r.ID + "|" + beat})
		}
	}
	return out
}

// Notifier flushes new events to Telegram; one flush runs at a time.
type Notifier struct {
	mu  sync.Mutex
	log func(string)
}

// New returns a Notifier that reports a failure once through log (nil: silent).
func New(log func(string)) *Notifier { return &Notifier{log: log} }

// Final waits for a running flush, then flushes once more (serve calls it when it stops).
func (n *Notifier) Final(now time.Time) {
	n.mu.Lock()
	n.mu.Unlock()
	n.Flush(now)
}

// Flush sends what happened since the last flush. It never panics or returns an error: a failed
// send is logged once (until a send succeeds or the error changes) and shown by status.
func (n *Notifier) Flush(now time.Time) {
	if !n.mu.TryLock() {
		return
	}
	defer n.mu.Unlock()
	defer func() { _ = recover() }()
	st, ok := load()
	tok, chat, cerr := creds()
	if !ok || cerr != nil {
		st.Cursor = now.UTC().Truncate(time.Second)
		_ = st.save()
		return
	}
	on := enabled()
	evs, _ := events.Read(st.Cursor.Add(-48*time.Hour), 0)
	titles := map[string]string{}
	var pending []item
	cursor := st.Cursor
	for _, e := range evs {
		if e.Kind == "launched" && e.Detail != "" {
			titles[e.Batch+"|"+e.Item] = e.Detail
		}
		if e.T.Before(st.Cursor) {
			continue
		}
		if e.T.After(cursor) {
			cursor = e.T
		}
		if it, ok := classify(e); ok && on[it.kind] {
			it.title = titles[e.Batch+"|"+e.Item]
			pending = append(pending, it)
		}
	}
	if on["stuck"] {
		pending = append(pending, stuck()...)
	}
	seen := map[string]bool{}
	var todo []item
	for _, it := range pending {
		if _, done := st.Sent[it.dedupe]; done || seen[it.dedupe] {
			continue
		}
		seen[it.dedupe] = true
		todo = append(todo, it)
	}
	var win []time.Time
	for _, t := range st.Window {
		if now.Sub(t) < time.Minute {
			win = append(win, t)
		}
	}
	budget := PerMinute - len(win)
	if len(todo) > 0 && (budget < 1 || (len(todo) > budget && budget < 2)) {
		st.Window = win
		_ = st.save()
		return
	}
	one := func(msg string) {
		win = append(win, now)
		if err := send(tok, chat, msg); err != nil {
			if err.Error() != st.LastErr && n.log != nil {
				n.log("notify: " + err.Error())
			}
			st.LastErr, st.ErrAt = err.Error(), now
			return
		}
		st.LastErr, st.LastSent = "", now
	}
	individual := todo
	if len(todo) > budget {
		individual = todo[:budget-1]
	}
	for _, it := range individual {
		one(message(it))
	}
	if rest := todo[len(individual):]; len(rest) > 0 {
		count := map[string]int{}
		for _, it := range rest {
			count[label[it.kind]]++
		}
		var parts []string
		for k, c := range count {
			parts = append(parts, fmt.Sprintf("%d %s", c, k))
		}
		sort.Strings(parts)
		one(fmt.Sprintf("smithy · %d more\n%s\nsmithy tui shows them", len(rest), strings.Join(parts, ", ")))
	}
	for _, it := range todo {
		st.Sent[it.dedupe] = now
	}
	for k, t := range st.Sent {
		if now.Sub(t) > 7*24*time.Hour {
			delete(st.Sent, k)
		}
	}
	st.Cursor, st.Window = cursor, win
	_ = st.save()
}

// Status is the setup and health lines for `smithy notify status` and the TUI System view.
func Status() []string {
	var out []string
	tok, from, err := auth.Get("telegram")
	switch {
	case err != nil:
		out = append(out, "token  error: "+text.Clean(err.Error()))
	case tok == "":
		out = append(out, "token  not set (smithy auth set telegram)")
	default:
		out = append(out, "token  "+auth.Mask(tok)+" ("+from+")")
	}
	if c := strings.TrimSpace(config.Get("notify.telegram.chat", "")); c != "" {
		out = append(out, "chat "+text.Clean(c))
	} else {
		out = append(out, "chat   not set (smithy config set notify.telegram.chat <id>)")
	}
	out = append(out, "kinds  "+text.Clean(config.Get("notify.kinds", strings.Join(Kinds, ","))))
	if _, _, err := creds(); err == nil {
		out = append(out, "state  on (smithy auto serve sends)")
	} else {
		out = append(out, "state  off: "+scrub(err.Error(), tok))
	}
	st, _ := load()
	if !st.LastSent.IsZero() {
		out = append(out, "last sent "+st.LastSent.Local().Format("2006-01-02 15:04"))
	}
	if st.LastErr != "" {
		out = append(out, "last error "+st.ErrAt.Local().Format("2006-01-02 15:04")+" "+scrub(st.LastErr, tok))
	}
	return out
}

// Main runs `smithy notify`.
func Main(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 1 && args[0] == "test":
		if err := Test(); err != nil {
			fmt.Fprintln(stderr, "smithy notify:", err)
			return 1
		}
		fmt.Fprintln(stdout, "sent a test message")
		return 0
	case len(args) == 1 && args[0] == "status":
		for _, l := range Status() {
			fmt.Fprintln(stdout, l)
		}
		return 0
	case len(args) == 1 && (args[0] == "-h" || args[0] == "--help"):
		fmt.Fprintln(stdout, Usage)
		return 0
	}
	fmt.Fprintln(stderr, Usage)
	return 2
}
