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
const Usage = `usage: smithy notify test [--all] | status
Telegram notifications, sent by smithy auto serve from events.jsonl. Setup:
  smithy auth set telegram            (the bot token from @BotFather, on stdin)
  smithy config set notify.telegram.chat <chat id>
  smithy config set notify.kinds pr_ready,blocked,stuck,decision,orchestrator_failed,serve_down,cap_reached,limited
test sends one test message (--all: one sample of every kind); status shows the token (masked), chat, kinds, last send and last error.
Harness build: smithy daemon sends instead, through its chat connectors: notify.telegram.chat is then
the numeric id of a supergroup with topics on (one topic per session); decision buttons answer the
decision, a reply in a session's topic goes to that session. connect.telegram.kinds narrows what
it sends; connect.telegram.users (Telegram user ids) is who it hears, and unset it hears nobody.`

// Kinds are every notification kind, in the default notify.kinds order.
var Kinds = []string{"pr_ready", "blocked", "stuck", "decision", "orchestrator_failed", "serve_down", "cap_reached", "limited"}

var label = map[string]string{"pr_ready": "PR ready", "blocked": "blocked", "stuck": "stuck", "decision": "decision pending",
	"orchestrator_failed": "orchestrator failed", "serve_down": "serve stopped", "cap_reached": "daily cap reached",
	"limited": "every pool account limited"}

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
	Retry    []retry              `json:"retry,omitempty"`
}

// retry is an alert whose send failed; it is tried again each flush for RetryFor.
type retry struct {
	Kind, Key, Title, PR, Reason, Dedupe string
	Batch, Stage                         string `json:",omitempty"`
	First                                time.Time
}

// RetryFor is how long a failed alert is retried before it is dropped (logged once).
const RetryFor = 24 * time.Hour

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

// button is one inline-keyboard URL button.
type button struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

// send posts one HTML message (link previews off) with optional URL buttons. Errors never contain the token.
func send(tok, chat, msg string, kb []button) error {
	form := url.Values{"chat_id": {chat}, "text": {msg}, "parse_mode": {"HTML"}, "link_preview_options": {`{"is_disabled":true}`}}
	if len(kb) > 0 {
		b, _ := json.Marshal(map[string][][]button{"inline_keyboard": {kb}})
		form.Set("reply_markup", string(b))
	}
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
	m, kb := render(item{kind: "test", reason: "notifications reach this chat"})
	return send(tok, chat, m, kb)
}

// TestAll sends the test message and one sample of every kind, so the design can be checked.
func TestAll() error {
	tok, chat, err := creds()
	if err != nil {
		return err
	}
	cost, pr, issue := 1.84, "https://github.com/Zovaris/smithy/pulls", "https://github.com/Zovaris/smithy/issues"
	samples := []item{
		{kind: "test", reason: "notifications reach this chat"},
		{kind: "pr_ready", key: "#44", title: "Add <dark mode> & tidy the header", repo: "acme/web", stage: "ready", model: "strong", speed: "normal", cost: &cost, dur: 42 * time.Minute, pr: pr, issue: issue},
		{kind: "blocked", key: "#45", title: "Migrate billing to v2", repo: "acme/api", stage: "failed", model: "mid", speed: "fast", dur: 18 * time.Minute, reason: "go test ./billing: 3 failures", issue: issue},
		{kind: "stuck", key: "#46", title: "Speed up search", repo: "acme/api", stage: "working", reason: "no heartbeat since 25m", issue: issue},
		{kind: "decision", key: "#47", title: "Pick a queue", repo: "acme/api", stage: "needs_decision", question: "Use Postgres SKIP LOCKED or Redis streams for the job queue?", issue: issue},
		{kind: "orchestrator_failed", reason: "warden exited 1: no task files in dir"},
		{kind: "serve_down", reason: "serve stopped"},
		{kind: "cap_reached", reason: "3 of 3 issues launched today"},
		{kind: "limited", reason: "earliest reset 15:00"},
	}
	for _, it := range samples {
		m, kb := render(it)
		if err := send(tok, chat, m, kb); err != nil {
			return err
		}
	}
	return nil
}

// enabled is the set of kinds notify.kinds turns on.
func enabled() map[string]bool {
	on := map[string]bool{}
	for _, k := range strings.Split(config.Get("notify.kinds", strings.Join(Kinds, ",")), ",") {
		on[strings.TrimSpace(k)] = true
	}
	return on
}

type item struct {
	kind, batch, key, title, pr, reason, dedupe string
	issue, repo, stage, model, speed, question  string
	// id is the smithy daemon decision this alert asks (events.DecisionID), answered by that id.
	id    string
	cost  *float64
	dur   time.Duration
	first time.Time
}

var header = map[string]string{"pr_ready": "✅ <b>PR ready</b>", "blocked": "⛔ <b>Blocked</b>", "stuck": "⏳ <b>Stuck</b>",
	"decision": "🤔 <b>Decision needed</b>", "orchestrator_failed": "💥 <b>Orchestrator failed</b>", "serve_down": "🛑 <b>Serve stopped</b>",
	"cap_reached": "💸 <b>Daily cap reached</b>", "limited": "🔒 <b>Every pool account limited</b>", "test": "🧪 <b>smithy test</b>"}

// esc escapes text for Telegram HTML (only &, < and > need it, per the Bot API docs).
var esc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace

// clean is one escaped line of untrusted text, at most n runes before escaping.
func clean(s string, n int) string { return esc(trim(text.Clean(s), n)) }

// webURL keeps s only when it is an absolute http(s) URL.
func webURL(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.String()
}

var repoRe = regexp.MustCompile(`^https?://[^/]+/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)/(pull|issues)/`)

func dur(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// render is a short HTML message (at most 9 lines) and its URL buttons; every value is escaped.
func render(it item) (string, []button) {
	if style() == "plain" {
		return renderPlain(it)
	}
	h := header[it.kind]
	if it.kind == "blocked" && it.stage == "failed" {
		h = "⛔ <b>Failed</b>"
	}
	lines := []string{h}
	if it.key != "" || it.title != "" {
		l := "<b>" + clean(it.key, 40) + "</b>"
		if it.title != "" {
			l += " <i>" + clean(it.title, 80) + "</i>"
		}
		lines = append(lines, strings.TrimPrefix(l, "<b></b> "))
	}
	repo := it.repo
	for _, u := range []string{it.pr, it.issue} {
		if m := repoRe.FindStringSubmatch(u); repo == "" && m != nil {
			repo = m[1]
		}
	}
	var where []string
	if repo != "" {
		where = append(where, "📁 <code>"+clean(repo, 60)+"</code>")
	}
	if it.stage != "" {
		where = append(where, "stage <b>"+clean(it.stage, 20)+"</b>")
	}
	if len(where) > 0 {
		lines = append(lines, strings.Join(where, " · "))
	}
	var run []string
	if it.model != "" || it.speed != "" {
		run = append(run, "🧠 "+strings.Trim(clean(it.model, 20)+" · "+clean(it.speed, 20), " ·"))
	}
	if it.cost != nil {
		run = append(run, fmt.Sprintf("💲 $%.2f", *it.cost))
	}
	if it.dur > 0 {
		run = append(run, "⏱ "+dur(it.dur))
	}
	if len(run) > 0 {
		lines = append(lines, strings.Join(run, "  "))
	}
	if it.question != "" {
		lines = append(lines, "<blockquote>"+clean(it.question, 300)+"</blockquote>",
			"Answer in <code>smithy tui</code> › Needs you, or <code>smithy decisions pending</code>")
	}
	if it.reason != "" {
		lines = append(lines, "<blockquote>"+clean(it.reason, 200)+"</blockquote>")
	}
	if it.id != "" {
		lines = append(lines, "Decision <code>"+clean(it.id, 200)+"</code>")
	}
	_, kb := renderButtons(it)
	return strings.Join(lines, "\n"), kb
}

func renderButtons(it item) (string, []button) {
	var kb []button
	if u := webURL(it.pr); u != "" {
		kb = append(kb, button{"Open PR", u})
	}
	if u := webURL(it.issue); u != "" {
		kb = append(kb, button{"Open issue", u})
	}
	return "", kb
}

// enrich fills an item's issue, title, duration, model, speed, question and cost from the auto
// state, its card and the usage events; every source is optional.
func enrich(it *item, evs []events.Event, now time.Time) {
	if it.batch == "" || it.key == "" || filepath.Base(it.batch) != it.batch || filepath.Base(it.key) != it.key {
		return
	}
	var st struct {
		Launched map[string][]struct {
			Key, Title, URL, Batch, Item string
			At                           time.Time
		} `json:"launched"`
	}
	if b, err := os.ReadFile(filepath.Join(ghx.Home(), "auto", "state.json")); err == nil && json.Unmarshal(b, &st) == nil {
		for _, day := range st.Launched {
			for _, l := range day {
				if l.Batch == it.batch && l.Item == it.key {
					it.issue = l.URL
					if it.title == "" {
						it.title = l.Title
					}
					if !l.At.IsZero() {
						it.dur = now.Sub(l.At)
					}
				}
			}
		}
	}
	var card map[string]any
	if b, err := os.ReadFile(filepath.Join(cards.Home(), it.batch, it.key+".json")); err == nil && json.Unmarshal(b, &card) == nil {
		str := func(k string) string { s, _ := card[k].(string); return s }
		it.model, it.speed = str("model"), str("speed")
		if it.kind == "decision" {
			it.question = str("question")
			it.id = events.DecisionID(events.Event{Kind: "stage", Batch: it.batch, Item: it.key, Detail: "needs_decision"}, str("decision_key"))
		}
		if it.reason == "" {
			it.reason = str("reason")
		}
	}
	for _, e := range evs {
		if e.Kind == "usage" && e.Batch == it.batch && e.Item == it.key && e.Usage != nil && e.Usage.CostUSD != nil {
			c := *e.Usage.CostUSD
			if it.cost != nil {
				c += *it.cost
			}
			it.cost = &c
		}
	}
}

// classify maps one event to a notification, or ok false.
func classify(e events.Event) (item, bool) {
	key := e.Item
	if key == "" {
		key = e.ID
	}
	it := item{batch: e.Batch, key: key, pr: e.PR, reason: e.Detail}
	if e.Kind == "stage" {
		it.stage, it.reason = e.Detail, ""
	}
	switch {
	case e.Kind == "stage" && e.Detail == "ready":
		it.kind = "pr_ready"
		it.id = events.DecisionID(e, "")
	case e.Kind == "stage" && (e.Detail == "blocked" || e.Detail == "failed"):
		it.kind = "blocked"
	case e.Kind == "stage" && e.Detail == "needs_decision":
		it.kind = "decision"
	case e.Kind == "orchestrator_failed", e.Kind == "serve_down":
		it.kind = e.Kind
		it.dedupe = e.Kind + "|" + e.T.Format(time.RFC3339)
		return it, true
	case e.Kind == "limited" && e.Account == "":
		it.kind = e.Kind
		it.dedupe = e.Kind + "|" + e.Detail
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
			out = append(out, item{kind: "stuck", batch: batch, key: r.ID, pr: r.PR, stage: r.State, reason: "no heartbeat since " + r.Beat,
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

// FinalWait bounds Final.
var FinalWait = 15 * time.Second

// Final waits for a running flush, then flushes once more (serve calls it when it stops); it
// returns after FinalWait at most, leaving an unfinished flush behind.
func (n *Notifier) Final(now time.Time) {
	done := make(chan struct{})
	go func() {
		n.mu.Lock()   //nolint:staticcheck // SA2001: barrier that waits for a running flush
		n.mu.Unlock() //nolint:staticcheck // same barrier
		n.Flush(now)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(FinalWait):
	}
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
	for _, r := range st.Retry {
		if now.Sub(r.First) > RetryFor {
			if n.log != nil {
				n.log(fmt.Sprintf("notify: dropped %s %s after %s of failed sends", r.Kind, text.Clean(r.Key), RetryFor))
			}
			st.Sent[r.Dedupe] = now
			continue
		}
		pending = append(pending, item{kind: r.Kind, batch: r.Batch, key: r.Key, title: r.Title, pr: r.PR, stage: r.Stage, reason: r.Reason, dedupe: r.Dedupe, first: r.First})
	}
	st.Retry = nil
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
	keep := func(its []item) {
		for _, it := range its {
			if it.first.IsZero() {
				it.first = now
			}
			st.Retry = append(st.Retry, retry{Kind: it.kind, Batch: it.batch, Key: it.key, Title: it.title, PR: it.pr, Stage: it.stage, Reason: it.reason, Dedupe: it.dedupe, First: it.first})
		}
	}
	if len(todo) > 0 && (budget < 1 || (len(todo) > budget && budget < 2)) {
		keep(todo)
		st.Cursor, st.Window = cursor, win
		_ = st.save()
		return
	}
	one := func(msg string, kb []button) bool {
		if err := send(tok, chat, msg, kb); err != nil {
			if err.Error() != st.LastErr && n.log != nil {
				n.log("notify: " + err.Error())
			}
			st.LastErr, st.ErrAt = err.Error(), now
			return false
		}
		win = append(win, now)
		st.LastErr, st.LastSent = "", now
		return true
	}
	individual := todo
	if len(todo) > budget {
		individual = todo[:budget-1]
	}
	for i, it := range individual {
		enrich(&it, evs, now)
		if !one(render(it)) {
			keep(todo[i:])
			todo = todo[:i]
			individual = todo
			break
		}
	}
	if rest := todo[len(individual):]; len(rest) > 0 {
		count := map[string]int{}
		for _, it := range rest {
			count[label[it.kind]]++
		}
		var parts []string
		for k, c := range count {
			parts = append(parts, fmt.Sprintf("• %d %s", c, esc(k)))
		}
		sort.Strings(parts)
		if len(parts) > 8 {
			parts = parts[:8]
		}
		msg := fmt.Sprintf("📬 <b>%d more alerts</b> (rate limit)\n%s\n<i>smithy tui shows them</i>", len(rest), strings.Join(parts, "\n"))
		if !one(msg, nil) {
			keep(rest)
			todo = individual
		}
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
	var unknown []string
	for k := range enabled() {
		if _, ok := label[k]; !ok && k != "" {
			unknown = append(unknown, text.Clean(k))
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		out = append(out, "warn   unknown notify.kinds: "+strings.Join(unknown, ",")+" (ignored)")
	}
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
	case len(args) == 2 && args[0] == "test" && args[1] == "--all":
		if err := TestAll(); err != nil {
			fmt.Fprintln(stderr, "smithy notify:", err)
			return 1
		}
		fmt.Fprintf(stdout, "sent %d sample messages\n", len(Kinds)+1)
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

// style is notify.style: emoji (icons per line) or plain (typographic, no emoji).
func style() string {
	if config.Get("notify.style", "plain") == "emoji" {
		return "emoji"
	}
	return "plain"
}

var plainTag = map[string]string{"pr_ready": "READY", "blocked": "BLOCKED", "stuck": "STUCK", "decision": "DECIDE",
	"orchestrator_failed": "FAILED", "serve_down": "STOPPED", "cap_reached": "CAP", "limited": "LIMITED", "test": "TEST"}

var plainTitle = map[string]string{"pr_ready": "PR ready for review", "blocked": "Blocked", "stuck": "No heartbeat",
	"decision": "Decision needed", "orchestrator_failed": "Orchestrator failed", "serve_down": "Serve stopped",
	"cap_reached": "Daily cap reached", "limited": "All pool accounts limited", "test": "smithy"}

// renderPlain: a code-font status tag, a bold title line, one monospace meta line, the reason quoted.
func renderPlain(it item) (string, []button) {
	tag, title := plainTag[it.kind], plainTitle[it.kind]
	if it.kind == "blocked" && it.stage == "failed" {
		tag, title = "FAILED", "Failed"
	}
	lines := []string{"<code>" + tag + "</code>  <b>" + title + "</b>"}
	if it.key != "" || it.title != "" {
		lines = append(lines, strings.TrimSpace("<b>"+clean(it.key, 40)+"</b> "+clean(it.title, 80)))
	}
	repo := it.repo
	for _, u := range []string{it.pr, it.issue} {
		if m := repoRe.FindStringSubmatch(u); repo == "" && m != nil {
			repo = m[1]
		}
	}
	var meta []string
	for _, v := range []string{clean(repo, 60), clean(it.stage, 20), strings.Trim(clean(it.model, 20)+"/"+clean(it.speed, 20), "/")} {
		if v != "" {
			meta = append(meta, v)
		}
	}
	if it.cost != nil {
		meta = append(meta, fmt.Sprintf("$%.2f", *it.cost))
	}
	if it.dur > 0 {
		meta = append(meta, dur(it.dur))
	}
	if len(meta) > 0 {
		lines = append(lines, "<code>"+strings.Join(meta, " · ")+"</code>")
	}
	if it.question != "" {
		lines = append(lines, "<blockquote>"+clean(it.question, 300)+"</blockquote>",
			"<i>Answer in</i> <code>smithy tui</code> <i>› Needs you, or</i> <code>smithy decisions pending</code>")
	}
	if it.reason != "" {
		lines = append(lines, "<blockquote>"+clean(it.reason, 200)+"</blockquote>")
	}
	if it.id != "" {
		lines = append(lines, "Decision <code>"+clean(it.id, 200)+"</code>")
	}
	_, kb := renderButtons(it)
	return strings.Join(lines, "\n"), kb
}

// Alert is one event's notification, for other senders (the daemon's connectors).
type Alert struct{ Kind, Label, Key, PR, Reason, Dedupe string }

// Classify maps one event to an alert, or ok false (the same rules Flush uses).
func Classify(e events.Event) (Alert, bool) {
	it, ok := classify(e)
	if !ok {
		return Alert{}, false
	}
	return Alert{Kind: it.kind, Label: plainTitle[it.kind], Key: it.key, PR: webURL(it.pr), Reason: it.reason, Dedupe: it.dedupe}, true
}
