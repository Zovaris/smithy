package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Zovaris/smithy/internal/guard"
)

// DefaultPass is the score a judge step passes at when the step sets none.
const DefaultPass = 0.7

var prRe = regexp.MustCompile(`^(#?[0-9]{1,7}|https://github\.com/[\w.-]+/[\w.-]+/pull/[0-9]{1,7})$`)

// more runs the steps beyond the core set.
func (r *runner) more(ctx context.Context, s Step, sc *scope, res *StepResult) error {
	switch s.Kind {
	case KindJudge:
		return r.judge(ctx, s, sc, res)
	case KindLoop:
		return r.loop(ctx, s, sc, res)
	case KindSession, KindSmelt, KindWarden:
		return r.session(ctx, s, sc, res)
	case KindHTTP:
		return r.http(ctx, s, sc, res)
	case KindPar:
		return r.parallel(ctx, s, sc, res)
	case KindWait:
		return r.wait(ctx, s, res)
	case KindGitHub:
		return r.github(ctx, s, sc, res)
	case KindFile:
		return r.file(s, sc, res)
	}
	return fmt.Errorf("unknown step kind %q", s.Kind)
}

func (r *runner) judge(ctx context.Context, s Step, sc *scope, res *StepResult) error {
	if err := r.takeAgent(s.ID); err != nil {
		return err
	}
	var crit strings.Builder
	for _, c := range s.Criteria {
		crit.WriteString("- " + sc.render(c, false, 0) + "\n")
	}
	prompt := "Grade the work below against each criterion. Be strict: a criterion that is not clearly met fails.\n\nCriteria:\n" + crit.String() +
		"\nWork:\n" + sc.render(s.Subject, true, 0) + dataNote +
		"\n\nAnswer with only one JSON object: {\"score\": a number from 0 to 1, \"why\": one short paragraph naming what fails}."
	res.Input = prompt
	ans, cost, err := r.env.Agent(ctx, AgentCall{Dir: r.w.Dir, Prompt: prompt, Model: s.Model, Effort: s.Effort, Timeout: s.timeout()})
	r.lim.mu.Lock()
	r.lim.cost += cost
	r.lim.mu.Unlock()
	res.CostUSD, res.Output = cost, clip(ans)
	if err != nil {
		return err
	}
	js, _, err := answer(Step{Answer: "json", Schema: map[string]string{"score": "number", "why": "string"}}, ans)
	if err != nil {
		return err
	}
	var v struct {
		Score float64 `json:"score"`
		Why   string  `json:"why"`
	}
	_ = json.Unmarshal([]byte(js), &v)
	pass := s.Pass
	if pass == 0 {
		pass = DefaultPass
	}
	res.Answer, res.Why, res.Output = "fail", v.Why, js
	if v.Score >= pass {
		res.Answer = "pass"
	}
	return nil
}

func (r *runner) loop(ctx context.Context, s Step, sc *scope, res *StepResult) error {
	max := s.Max
	if max == 0 {
		max = 3
	}
	for i := 0; i < max; i++ {
		ir := StepResult{ID: fmt.Sprintf("%s[%d]", s.ID, i), Kind: "iteration", Started: r.env.Now()}
		stopped, err := r.block(ctx, s.Do, sc, &ir.Steps, false)
		ir.Ended = r.env.Now()
		ir.DurationMS = ir.Ended.Sub(ir.Started).Milliseconds()
		ir.Status = StatusOK
		if err != nil {
			ir.Status, ir.Error = StatusFailed, err.Error()
		}
		res.Steps = append(res.Steps, ir)
		if errors.Is(err, errCapped) {
			return err
		}
		if err != nil {
			return fmt.Errorf("try %d: %v", i+1, err)
		}
		ok, terr := sc.test(s.Until)
		if terr != nil {
			return terr
		}
		if ok || stopped {
			res.Output = fmt.Sprintf("done after %d of %d tries", i+1, max)
			res.Answer = "met"
			return nil
		}
	}
	res.Answer = "not met"
	return fmt.Errorf("until not met after %d tries", max)
}

func (r *runner) session(ctx context.Context, s Step, sc *scope, res *StepResult) error {
	if r.env.Session == nil {
		return fmt.Errorf("%s steps need the daemon (smithy daemon)", s.Kind)
	}
	if err := r.takeAgent(s.ID); err != nil {
		return err
	}
	var prompt string
	switch s.Kind {
	case KindSession:
		prompt = sc.render(s.Prompt, true, 0)
	case KindSmelt:
		speed := s.Speed
		if speed == "" {
			speed = "fast"
		}
		prompt = "smelt " + speed + " " + sc.render(s.Task, true, 0)
	default:
		prompt = "warden " + sc.render(s.Task, true, 0)
	}
	if strings.Contains(prompt, "<data") {
		prompt += dataNote
	}
	res.Input = prompt
	id, err := r.env.Session(ctx, r.w.Dir, s.Agent, prompt)
	res.Output = id
	return err
}

func (r *runner) http(ctx context.Context, s Step, sc *scope, res *StepResult) error {
	method := strings.ToUpper(s.Method)
	if method == "" {
		method = "GET"
	}
	u := sc.render(s.URL, false, 0)
	headers := map[string]string{}
	for k, v := range s.Headers {
		headers[k] = sc.render(v, false, 0)
	}
	body := sc.render(s.Body, false, 0)
	res.Input = method + " " + u
	fetch := r.env.Fetch
	if fetch == nil {
		fetch = Fetch
	}
	status, out, err := fetch(ctx, method, u, headers, body)
	res.Exit, res.Output = status, clip(out)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("HTTP %d", status)
	}
	return nil
}

// Fetch is an http step's request: only to hosts in the user's guard.fetch_allow, no redirects,
// 30 s and 1 MiB at most.
func Fetch(ctx context.Context, method, raw string, headers map[string]string, body string) (int, string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return 0, "", fmt.Errorf("bad url")
	}
	rules, _ := guard.LoadFetch()
	allowed := false
	for _, a := range rules.Allow {
		if a != "*" && guard.HostMatch(a, u.Hostname()) {
			allowed = true
		}
	}
	if !allowed {
		return 0, "", fmt.Errorf("%s is not in guard.fetch_allow (smithy config set guard.fetch_allow %s)", u.Hostname(), u.Hostname())
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "smithy-workflow")
	d := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, _ := net.SplitHostPort(address)
		if ip := net.ParseIP(host); ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("%s is a local or private address", host)
		}
		return nil
	}}
	c := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{DialContext: d.DialContext, TLSHandshakeTimeout: 10 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(b), nil
}

func (r *runner) parallel(ctx context.Context, s Step, sc *scope, res *StepResult) error {
	out := make([]StepResult, len(s.Branches))
	errs := make([]error, len(s.Branches))
	scopes := make([]*scope, len(s.Branches))
	var wg sync.WaitGroup
	for i, b := range s.Branches {
		scopes[i] = sc.child()
		wg.Add(1)
		go func(i int, b []Step) {
			defer wg.Done()
			br := StepResult{ID: fmt.Sprintf("%s[%d]", s.ID, i), Kind: "branch", Started: r.env.Now()}
			_, err := r.block(ctx, b, scopes[i], &br.Steps, false)
			br.Ended = r.env.Now()
			br.DurationMS = br.Ended.Sub(br.Started).Milliseconds()
			br.Status = StatusOK
			if err != nil {
				br.Status, br.Error = StatusFailed, err.Error()
			}
			if n := len(br.Steps); n > 0 {
				br.Output = br.Steps[n-1].Output
			}
			out[i], errs[i] = br, err
		}(i, b)
	}
	wg.Wait()
	outs := []string{}
	for i, br := range out {
		outs = append(outs, br.Output)
		for _, st := range br.Steps {
			sc.setStep(st)
		}
		scopes[i].mu.Lock()
		for k, v := range scopes[i].vars {
			sc.setVar(k, v)
		}
		scopes[i].mu.Unlock()
	}
	res.Steps = out
	b, _ := json.Marshal(outs)
	res.Output = clip(string(b))
	for _, err := range errs {
		if errors.Is(err, errCapped) {
			return err
		}
	}
	for i, err := range errs {
		if err != nil {
			return fmt.Errorf("branch %d: %v", i, err)
		}
	}
	return nil
}

func (r *runner) wait(ctx context.Context, s Step, res *StepResult) error {
	if s.For != "" {
		d, _ := time.ParseDuration(s.For)
		res.Input = "for " + s.For
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out before %s passed", s.For)
		case <-time.After(d):
		}
		return nil
	}
	every := 30 * time.Second
	if d, err := time.ParseDuration(s.Every); err == nil && d > 0 {
		every = d
	}
	res.Input = s.Check
	if r.env.Guard != nil {
		if p := r.env.Guard(s.Check); p != "" {
			return fmt.Errorf("blocked by guard (%s)", p)
		}
	}
	for n := 1; ; n++ {
		out, exit, err := r.env.Shell(ctx, r.w.Dir, s.Check, nil)
		res.Output, res.Exit, res.Attempts = clip(out), exit, n
		if err == nil && exit == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("check still failing after %d tries", n)
		case <-time.After(every):
		}
	}
}

func (r *runner) github(ctx context.Context, s Step, sc *scope, res *StepResult) error {
	pr := strings.TrimPrefix(strings.TrimSpace(sc.render(s.PR, false, 0)), "#")
	if (s.Action == "comment" || s.Action == "label" || s.Action == "merge") && !prRe.MatchString(pr) {
		return fmt.Errorf("pr %q is not a PR number or URL", clipText(pr, 60))
	}
	body, title, label := sc.render(s.Body, false, 0), sc.render(s.Title, false, 0), sc.render(s.Label, false, 0)
	var runs [][]string
	switch s.Action {
	case "alerts":
		runs = [][]string{{"gh", "api", "repos/{owner}/{repo}/dependabot/alerts?state=open"}}
	case "prs":
		runs = [][]string{{"gh", "pr", "list", "--state", "open", "--json", "number,title,url,headRefName,isDraft"}}
	case "issues":
		runs = [][]string{{"gh", "issue", "list", "--state", "open", "--json", "number,title,url"}}
	case "comment":
		runs = [][]string{{"gh", "pr", "comment", pr, "--body", body}}
	case "label":
		runs = [][]string{{"gh", "pr", "edit", pr, "--add-label", label}}
	case "issue":
		runs = [][]string{{"gh", "issue", "create", "--title", title, "--body", body}}
	case "pr":
		exe0 := r.env.Exec
		if exe0 == nil {
			exe0 = Exec
		}
		br, _, err := exe0(ctx, r.w.Dir, []string{"git", "rev-parse", "--abbrev-ref", "HEAD"})
		if br = strings.TrimSpace(br); err != nil || br == "" || br == "HEAD" || br == "main" || br == "master" {
			return fmt.Errorf("github pr runs on a work branch, not %q", br)
		}
		runs = [][]string{{"git", "push", "-u", "origin", "HEAD"}, {"gh", "pr", "create", "--title", title, "--body", body}}
	case "merge":
		runs = [][]string{{"gh", "pr", "merge", pr, "--squash"}}
	}
	exe := r.env.Exec
	if exe == nil {
		exe = Exec
	}
	for _, argv := range runs {
		res.Input = strings.Join(argv[:min(len(argv), 4)], " ")
		if r.env.Guard != nil {
			if p := r.env.Guard(strings.Join(argv, " ")); p != "" {
				return fmt.Errorf("blocked by guard (%s)", p)
			}
		}
		out, exit, err := exe(ctx, r.w.Dir, argv)
		res.Output, res.Exit = clip(out), exit
		if err != nil {
			return err
		}
		if exit != 0 {
			return fmt.Errorf("%s exited %d", argv[0], exit)
		}
	}
	return nil
}

// Exec runs argv in dir without a shell, from the minimal shell environment.
func Exec(ctx context.Context, dir string, argv []string) (string, int, error) {
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = dirOr(dir)
	c.Env = ShellEnv(os.Getenv)
	c.WaitDelay = 5 * time.Second
	out := &capped{}
	c.Stdout, c.Stderr = out, out
	err := c.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), ee.ExitCode(), nil
	}
	return out.String(), 0, err
}

// allowedPath finds the files folder p is in and p's path inside it ("" root: outside them all).
func (r *runner) allowedPath(p string) (string, string, error) {
	var roots []string
	for _, f := range r.w.Files {
		if abs, err := filepath.Abs(dirOr(f)); err == nil {
			roots = append(roots, filepath.Clean(abs))
			if real, err := filepath.EvalSymlinks(abs); err == nil && real != abs {
				roots = append(roots, real)
			}
		}
	}
	if len(roots) == 0 {
		return "", "", fmt.Errorf("none of the workflow's files folders exist")
	}
	if !filepath.IsAbs(dirOr(p)) {
		p = filepath.Join(roots[0], p)
	}
	p = filepath.Clean(dirOr(p))
	for _, root := range roots {
		if rel, err := filepath.Rel(root, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return root, rel, nil
		}
	}
	return "", "", fmt.Errorf("%s is outside the workflow's files folders", p)
}

// file reads or writes through os.Root, so no path or symlink inside a files folder reaches
// outside it.
func (r *runner) file(s Step, sc *scope, res *StepResult) error {
	root, rel, err := r.allowedPath(sc.render(s.Path, false, 0))
	res.Input = s.Op + " " + filepath.Join(root, rel)
	if err != nil {
		return err
	}
	rt, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer rt.Close()
	if s.Op == "read" {
		f, err := rt.Open(rel)
		if err != nil {
			return outside(err)
		}
		defer f.Close()
		b, _ := io.ReadAll(io.LimitReader(f, MaxOutput))
		res.Output = string(b)
		return nil
	}
	if fi, err := rt.Lstat(rel); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink: file steps write only regular files", rel)
	}
	content := sc.render(s.Content, false, 0)
	flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if s.Op == "append" {
		flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := rt.OpenFile(rel, flag, 0o600)
	if err != nil {
		return outside(err)
	}
	if _, err := io.Copy(f, bytes.NewBufferString(content)); err != nil {
		f.Close()
		return err
	}
	res.Output = strconv.Itoa(len(content)) + " bytes"
	return f.Close()
}

func outside(err error) error {
	if strings.Contains(err.Error(), "escapes") {
		return fmt.Errorf("path is outside the workflow's files folders: %v", err)
	}
	return err
}
