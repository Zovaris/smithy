// Package decide asks an optional decision model (smithy decide) to flag rule breaks in a diff and
// to raise the impact of a worker's question. It only ever adds caution: when it is off, slow or
// failing, callers behave exactly as without it.
package decide

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/gitx"
)

const usage = `Optional decision model: flags likely rule breaks and raises question impact. Off until
decide.url and decide.model are set; prints nothing and exits 3 while off, so callers just skip it.
  smithy decide rules [--base <ref>] [--rules <file>]...   rule lines of the files vs the diff
  smithy decide impact "<question>"                          low | medium | high
  smithy decide status                                       on, off, or disabled and why
rules prints one line per rule with p >= 0.5: "flag p=0.94 <rule>" (>= 0.8) or "note p=0.61 <rule>".
Flags are leads for the standards reviewer, never a verdict. impact prints the level and both
probabilities; smithy cards set raises a needs_decision card's impact with it (never lowers it).
Default rule files: docs/agents/STANDARDS.md, AGENTS.md, CLAUDE.md (bullet lines, at most 30).
The wire format follows the URL: a path ending in /scores is the behavior format (Respan span-01),
anything else System One (Jev, Kev, Clef, span-02). Three failed calls in a row set
decide.enabled false and say so; smithy config set decide.enabled true turns it back on.
Settings: decide.url, decide.model, decide.key_env (the name of the env var holding the key,
never the key), decide.timeout (ms), decide.enabled. Answers are logged to ~/.smithy/decide.log.`

// Usage is the help text of smithy decide.
const Usage = usage

// ErrOff means no decision model is configured, or it was disabled.
var ErrOff = errors.New("decide is off")

// Client is a configured decision model.
type Client struct {
	URL, Model, Key string
	Timeout         time.Duration
	HTTP            *http.Client
}

// Question is one yes/no question: its id and what "yes" means.
type Question struct{ ID, Definition string }

func (c *Client) behaviorFormat() bool {
	u := strings.TrimRight(strings.SplitN(c.URL, "?", 2)[0], "/")
	return strings.HasSuffix(u, "/scores")
}

// FromConfig builds the client from settings; ErrOff when it isn't set up or was disabled.
func FromConfig() (*Client, error) {
	c := &Client{URL: config.Get("decide.url", ""), Model: config.Get("decide.model", "")}
	if c.URL == "" || c.Model == "" || config.Get("decide.enabled", "true") == "false" {
		return nil, ErrOff
	}
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "https" && !(u.Scheme == "http" && local(u.Hostname()))) {
		return nil, fmt.Errorf("decide.url must be https (or http to localhost), got %q", redactURL(c.URL))
	}
	if env := config.Get("decide.key_env", ""); env != "" {
		c.Key = os.Getenv(env)
		if c.Key == "" {
			return nil, fmt.Errorf("no key: %s is empty", env)
		}
	}
	ms, err2 := strconv.Atoi(config.Get("decide.timeout", "3000"))
	err = err2
	if err != nil || ms <= 0 {
		ms = 3000
	}
	c.Timeout = time.Duration(ms) * time.Millisecond
	return c, nil
}

func local(h string) bool { return h == "localhost" || h == "127.0.0.1" || h == "::1" }

func redactURL(s string) string {
	if i := strings.IndexByte(s, '?'); i >= 0 {
		return s[:i] + "?…"
	}
	return s
}

const maxFails = 3

func failFile() string { return filepath.Join(ghx.Home(), "decide.fails") }

func recordResult(err error) string {
	if err == nil {
		os.Remove(failFile())
		return ""
	}
	n := 0
	if b, e := os.ReadFile(failFile()); e == nil {
		n, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	n++
	if n < maxFails {
		os.WriteFile(failFile(), []byte(strconv.Itoa(n)), 0o600)
		return ""
	}
	if e := config.Set(config.UserFile(), "decide.enabled", "false"); e != nil {
		os.WriteFile(failFile(), []byte(strconv.Itoa(n)), 0o600)
		return "smithy decide: could not disable itself: " + e.Error()
	}
	os.Remove(failFile())
	return fmt.Sprintf("smithy decide: disabled after %d failed calls in a row; turn it back on with: smithy config set decide.enabled true", maxFails)
}

// ask calls the model and tracks failures, disabling it after three in a row.
func (c *Client) ask(context string, qs []Question) (map[string]float64, error) {
	p, err := c.Ask(context, qs)
	if msg := recordResult(err); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	}
	return p, err
}

// Ask returns, for each question, the probability that its definition holds for the context.
func (c *Client) Ask(context string, qs []Question) (map[string]float64, error) {
	var body any
	if c.behaviorFormat() {
		behaviors := make([]map[string]string, len(qs))
		for i, q := range qs {
			behaviors[i] = map[string]string{"id": q.ID, "definition": q.Definition}
		}
		body = map[string]any{
			"span": map[string]any{
				"input":  []map[string]string{{"role": "user", "content": "Judge the text that follows."}},
				"output": map[string]string{"role": "assistant", "content": context},
			},
			"behaviors": behaviors,
			"model":     c.Model,
		}
	} else {
		questions := map[string]any{}
		for _, q := range qs {
			questions[q.ID] = map[string]string{"type": "noul", "instructions": q.Definition}
		}
		body = map[string]any{"model": c.Model, "state": context, "questions": questions}
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", c.URL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{}
	}
	hc2 := *hc
	hc2.Timeout = c.Timeout
	resp, err := hc2.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	return parse(c.behaviorFormat(), raw, qs)
}

func parse(behaviors bool, raw []byte, qs []Question) (map[string]float64, error) {
	out := map[string]float64{}
	if behaviors {
		var r struct {
			Results []struct {
				ID       string   `json:"id"`
				PPresent *float64 `json:"p_present"`
			} `json:"results"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		for _, x := range r.Results {
			if x.PPresent != nil {
				out[x.ID] = *x.PPresent
			}
		}
	} else {
		var r struct {
			Answers map[string]struct {
				Noul *float64 `json:"noul"`
			} `json:"answers"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		for id, a := range r.Answers {
			if a.Noul != nil {
				out[id] = *a.Noul
			}
		}
	}
	for _, q := range qs {
		if _, ok := out[q.ID]; !ok {
			return nil, fmt.Errorf("no answer for %s", q.ID)
		}
	}
	return out, nil
}

// Impact definitions; the ids are neutral so the model judges the definition, not the name.
var impactQs = []Question{
	{"q1", "The decision touches money, personal data, security, production, a public API or schema, or cannot be undone."},
	{"q2", "The decision changes the contract between internal modules or what a module is responsible for."},
}

var rank = map[string]int{"low": 0, "medium": 1, "high": 2}

// Raise returns the model's level for a question when it is higher than impact. It does nothing
// (false) when decide is off or the call fails.
func Raise(question, impact string) (string, bool) {
	c, err := FromConfig()
	if err != nil {
		return impact, false
	}
	p, err := c.ask("A coding agent asks before acting: "+question, impactQs)
	if err != nil {
		return impact, false
	}
	v := Impact(p)
	logAnswer("impact", question, p, v)
	if rank[v] > rank[impact] {
		return v, true
	}
	return impact, false
}

// Impact rates a question low, medium or high from the two probabilities.
func Impact(p map[string]float64) string {
	switch {
	case p["q1"] >= 0.5:
		return "high"
	case p["q2"] >= 0.5:
		return "medium"
	}
	return "low"
}

// Rules reads the bullet lines of files as rules (at most max).
func Rules(files []string, max int) []string {
	var rules []string
	seen := map[string]bool{}
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if !strings.HasPrefix(l, "- ") && !strings.HasPrefix(l, "* ") {
				continue
			}
			l = strings.TrimSpace(l[2:])
			if len(l) < 12 || seen[l] {
				continue
			}
			seen[l] = true
			rules = append(rules, l)
			if len(rules) == max {
				fh.Close()
				return rules
			}
		}
		fh.Close()
	}
	return rules
}

const maxDiff = 12000

func logAnswer(kind, subject string, p map[string]float64, verdict string) {
	f, err := os.OpenFile(filepath.Join(ghx.Home(), "decide.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(map[string]any{"time": time.Now().UTC().Format(time.RFC3339), "kind": kind,
		"subject": subject, "p": p, "verdict": verdict})
	f.Write(append(b, '\n'))
}

// Main runs smithy decide.
func Main(args []string) int {
	if len(args) == 1 && args[0] == "status" {
		_, err := FromConfig()
		switch {
		case err == nil:
			fmt.Println("decide: on", redactURL(config.Get("decide.url", "")), config.Get("decide.model", ""))
		case errors.Is(err, ErrOff) && config.Get("decide.enabled", "true") == "false":
			fmt.Println("decide: disabled (smithy config set decide.enabled true)")
		case errors.Is(err, ErrOff):
			fmt.Println("decide: off (set decide.url and decide.model)")
		default:
			fmt.Println("decide:", err)
		}
		return 0
	}
	if len(args) == 0 || (args[0] != "rules" && args[0] != "impact") {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	if args[0] == "impact" && len(args) != 2 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	c, err := FromConfig()
	if errors.Is(err, ErrOff) {
		return 3
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy decide:", err)
		return 1
	}
	if args[0] == "impact" {
		p, err := c.ask("A coding agent asks before acting: "+args[1], impactQs)
		if err != nil {
			fmt.Fprintln(os.Stderr, "smithy decide:", err)
			return 1
		}
		v := Impact(p)
		logAnswer("impact", args[1], p, v)
		fmt.Printf("%s p_high=%.2f p_medium=%.2f\n", v, p["q1"], p["q2"])
		return 0
	}
	return rulesMain(c, args[1:])
}

func rulesMain(c *Client, args []string) int {
	base := ""
	var files []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--base" && i+1 < len(args):
			base = args[i+1]
			i++
		case args[i] == "--rules" && i+1 < len(args):
			files = append(files, args[i+1])
			i++
		default:
			fmt.Fprintln(os.Stderr, usage)
			return 2
		}
	}
	cwd, _ := os.Getwd()
	root := ghx.RepoRoot(cwd)
	if root == "" {
		fmt.Fprintln(os.Stderr, "smithy decide: not in a git repo")
		return 2
	}
	if len(files) == 0 {
		for _, f := range []string{"docs/agents/STANDARDS.md", "AGENTS.md", "CLAUDE.md"} {
			files = append(files, filepath.Join(root, f))
		}
	}
	rules := Rules(files, 30)
	if len(rules) == 0 {
		fmt.Println("decide: no rules found")
		return 0
	}
	sha, label := gitx.ResolveBase(root, base)
	if sha == gitx.EmptyTree {
		fmt.Fprintln(os.Stderr, "smithy decide: no base to diff against ("+label+"); pass --base")
		return 2
	}
	diff, err := ghx.Run("git", "-C", root, "diff", sha)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy decide:", err)
		return 1
	}
	if strings.TrimSpace(diff) == "" {
		fmt.Println("decide: empty diff")
		return 0
	}
	if len(diff) > maxDiff {
		diff = strings.ToValidUTF8(diff[:maxDiff], "")
		fmt.Println("decide: diff truncated to its first 12000 bytes")
	}
	qs := make([]Question, len(rules))
	for i, r := range rules {
		qs[i] = Question{fmt.Sprintf("r%d", i+1), "The added lines (starting with +) of this diff break this rule: " + r}
	}
	p, err := c.ask("Diff:\n"+diff, qs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy decide:", err)
		return 1
	}
	type hit struct {
		p    float64
		rule string
	}
	var hits []hit
	for i, r := range rules {
		v := p[qs[i].ID]
		verdict := ""
		if v >= 0.5 {
			hits = append(hits, hit{v, r})
			verdict = "note"
			if v >= 0.8 {
				verdict = "flag"
			}
		}
		logAnswer("rule", r, map[string]float64{"p": v}, verdict)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].p > hits[j].p })
	for _, h := range hits {
		tag := "note"
		if h.p >= 0.8 {
			tag = "flag"
		}
		fmt.Printf("%s p=%.2f %s\n", tag, h.p, h.rule)
	}
	if len(hits) == 0 {
		fmt.Printf("decide: no rule flagged (%d checked)\n", len(rules))
	}
	return 0
}
