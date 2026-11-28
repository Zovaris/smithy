package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/proxy"
	"github.com/Zovaris/smithy/internal/search"
	"github.com/Zovaris/smithy/internal/text"
)

// SearchTool searches files under root (the run's working folder) with smithy search's engines and caps.
func SearchTool(root string) Tool {
	return Tool{Name: "search", Description: "Search file contents under the working folder (ripgrep when bundled; git ls-files; a walker). Returns path:line:text, capped.",
		Schema: Object(map[string]any{"pattern": Str("regular expression, or text with fixed"), "path": Str("subfolder relative to the working folder"),
			"fixed": Bool("pattern is plain text"), "ignore_case": Bool("ignore case"), "max": Int("most matches, 1-500 (default 200)")}, "pattern"),
		Call: func(_ context.Context, a Args) (string, error) {
			pat, err := a.Line("pattern", 1000, true)
			if err != nil {
				return "", err
			}
			sub, err := a.Line("path", 1000, false)
			if err != nil {
				return "", err
			}
			dir, err := inside(root, sub)
			if err != nil {
				return "", err
			}
			fixed, err := a.Bool("fixed")
			if err != nil {
				return "", err
			}
			ic, err := a.Bool("ignore_case")
			if err != nil {
				return "", err
			}
			max, err := a.Int("max", 1, 500, search.DefaultMaxMatches)
			if err != nil {
				return "", err
			}
			r, err := search.Run(search.Options{Root: dir, Pattern: pat, Fixed: fixed, IgnoreCase: ic, MaxMatches: max, MaxBytes: MaxOutput / 2, Rg: search.FindRg(binDir())})
			if err != nil {
				return "", err
			}
			var b strings.Builder
			for _, m := range r.Matches {
				fmt.Fprintf(&b, "%s:%d:%s\n", m.Path, m.Line, m.Text)
			}
			if len(r.Matches) == 0 {
				b.WriteString("no matches\n")
			}
			if r.Truncated {
				b.WriteString("[truncated: narrow the pattern or path]\n")
			}
			return b.String(), nil
		}}
}

func binDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Dir(exe)
}

// inside is sub joined to root, refused when it leaves root (also through a symlink).
func inside(root, sub string) (string, error) {
	if sub == "" {
		return root, nil
	}
	if filepath.IsAbs(sub) {
		return "", errors.New("path must be relative to the working folder")
	}
	p := filepath.Join(root, sub)
	rr, err1 := filepath.EvalSymlinks(root)
	rp, err2 := filepath.EvalSymlinks(p)
	if err1 != nil || err2 != nil {
		return "", errors.New("path is not a folder under the working folder")
	}
	if rel, err := filepath.Rel(rr, rp); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path must stay under the working folder")
	}
	return rp, nil
}

var (
	secretRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	hostRe   = regexp.MustCompile(`^[a-z0-9.-]+$`)
	methods  = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"}
)

// SecretTool calls a service with a key bound by smithy auth bind, through p (smithy's egress
// proxy, run in process): the proxy adds the key for its bound hosts, holds an ask secret's
// writes for the user, and scrubs the key from the response. The key never reaches the agent.
func SecretTool(p *proxy.Proxy) Tool {
	return Tool{Name: "secret_call", Description: "Call an HTTPS API with secret:<name> (a key the user bound with smithy auth bind). You never see the key; a write may wait for the user's approval or be refused.",
		Schema: Object(map[string]any{"secret": Str("the secret's name"), "host": Str("the bound host, when the secret has several"),
			"method": Str("GET (default), HEAD, POST, PUT, PATCH or DELETE"), "path": Str("path and query, starting with /"),
			"body": Str("request body"), "content_type": Str("body content type (default application/json)")}, "secret", "path"),
		Call: func(ctx context.Context, a Args) (string, error) {
			name, err := a.Line("secret", 64, true)
			if err != nil {
				return "", err
			}
			if !secretRe.MatchString(name) {
				return "", errors.New("secret must match [a-z][a-z0-9_-]*")
			}
			host, err := a.Line("host", 253, false)
			if err != nil {
				return "", err
			}
			if host != "" && !hostRe.MatchString(host) {
				return "", errors.New("host must be a plain host name")
			}
			method, err := a.Line("method", 10, false)
			if err != nil {
				return "", err
			}
			if method = strings.ToUpper(method); method == "" {
				method = "GET"
			}
			if !slices.Contains(methods, method) {
				return "", fmt.Errorf("method must be one of %s", strings.Join(methods, ", "))
			}
			path, err := a.Line("path", 4096, true)
			if err != nil {
				return "", err
			}
			if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, " #") {
				return "", errors.New("path must start with a single / and hold no spaces or #")
			}
			body, err := a.String("body", 1<<20, false)
			if err != nil {
				return "", err
			}
			ct, err := a.Line("content_type", 200, false)
			if err != nil {
				return "", err
			}
			target := "/s/" + name
			if host != "" {
				target += "@" + host
			}
			req := httptest.NewRequestWithContext(ctx, method, "http://smithy-proxy"+target+path, strings.NewReader(body))
			req.RequestURI = target + path
			if body != "" {
				if ct == "" {
					ct = "application/json"
				}
				req.Header.Set("Content-Type", ct)
			}
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)
			res := rec.Result()
			out, _ := io.ReadAll(io.LimitReader(res.Body, MaxOutput))
			if res.StatusCode >= 400 && strings.HasPrefix(string(out), "smithy proxy:") {
				return "", fmt.Errorf("status %d: %s", res.StatusCode, strings.TrimSpace(string(out)))
			}
			return fmt.Sprintf("status %d %s\ncontent-type: %s\n\n%s", res.StatusCode, http.StatusText(res.StatusCode), text.Clean(res.Header.Get("Content-Type")), out), nil
		}}
}

var cardPart = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Asker records a decision on smithy daemon and reads its outcome back from the daemon's own record.
type Asker interface {
	AskMulti(ctx context.Context, id, question string, options []string, multi bool) error
	Outcome(ctx context.Context, id string) (answer string, answered bool, err error)
}

// AskTool asks the user a question through the warden card <batch>/<item> (card is
// "batch/item"), or, with no card (a plain chat outside warden), as ask:<key> with no card: the decision is recorded on smithy daemon (connect returns it, or an error when
// none runs, and then ask refuses), which routes it to the TUI, web and chat; the card shows it as
// needs_decision. The answer is read only from the daemon, never from the card, which the worker
// can write. It waits, polling every poll, until the answer or ctx ends.
func AskTool(card string, poll time.Duration, connect func() (Asker, error)) Tool {
	var one sync.Mutex
	return Tool{Name: "ask", Description: "Ask the user a decision and wait for the answer (TUI, web or chat). Only for real decisions: give the options and their trade-offs.",
		Schema: Object(map[string]any{"question": Str("one question"), "options": List("up to 6 short options; the user may also answer in their own words"),
			"multi":           Bool("true when several options may be picked together (the answer lists them, comma-separated)"),
			"timeout_minutes": Int("how long to wait, 1-240 (default 60)")}, "question"),
		Call: func(ctx context.Context, a Args) (string, error) {
			batch, item, ok := strings.Cut(card, "/")
			onCard := card != ""
			if onCard && (!ok || !cardPart.MatchString(batch) || !cardPart.MatchString(item) || batch == "." || batch == ".." || item == "." || item == "..") {
				return "", errors.New("ask: bad warden card " + strconv.Quote(card))
			}
			q, err := a.Line("question", 500, true)
			if err != nil {
				return "", err
			}
			opts, err := a.Strings("options", 6, 200)
			if err != nil {
				return "", err
			}
			multi, err := a.Bool("multi")
			if err != nil {
				return "", err
			}
			mins, err := a.Int("timeout_minutes", 1, 240, 60)
			if err != nil {
				return "", err
			}
			if connect == nil {
				return "", errors.New("ask needs smithy daemon running (it carries the answer); decide by the task's rules and say so in your report")
			}
			asker, err := connect()
			if err != nil {
				return "", errors.New("ask needs smithy daemon running (" + err.Error() + "); decide by the task's rules and say so in your report")
			}
			if !one.TryLock() {
				return "", errors.New("another question is already waiting for the user; wait for its answer")
			}
			defer one.Unlock()
			b := make([]byte, 4)
			rand.Read(b)
			key := "mcp-" + hex.EncodeToString(b)
			js := func(v any) string { b, _ := json.Marshal(v); return string(b) }
			pairs := []string{"state=needs_decision", "decision_key=" + key, "question=" + js(q), "impact=medium"}
			if len(opts) > 0 {
				pairs = append(pairs, "options="+js(opts))
			}
			want := "ask:" + key
			if onCard {
				want = "decide:" + batch + "/" + item + "/" + key
			}
			if err := asker.AskMulti(ctx, want, q, opts, multi); err != nil {
				return "", errors.New("ask: the daemon refused the question (" + err.Error() + "); decide by the task's rules and say so in your report")
			}
			setCard := func(p []string) error {
				if !onCard {
					return nil
				}
				_, err := cards.Set(batch, item, p)
				return err
			}
			if err := setCard(pairs); err != nil {
				return "", err
			}
			ctx, cancel := context.WithTimeout(ctx, time.Duration(mins)*time.Minute)
			defer cancel()
			t := time.NewTicker(poll)
			defer t.Stop()
			for {
				if ans, ok, err := asker.Outcome(ctx, want); err == nil && ok {
					_ = setCard([]string{"state=working", "decision_key="})
					return text.Clean(ans), nil
				}
				select {
				case <-ctx.Done():
					_ = setCard([]string{"state=working", "decision_key="})
					return "", errors.New("no answer after " + strconv.Itoa(mins) + " minutes; decide by the task's rules and say so in your report")
				case <-t.C:
				}
			}
		}}
}
