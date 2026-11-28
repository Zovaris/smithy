//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/linear"
	"github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/text"
)

const (
	KindSessionLink  = "session_link"
	KindSessionLinks = "session_links"
	linksMax         = 20
	linkTextMax      = 8 << 10
)

// LinkIssue is one issue a session links: a Linear issue (ID TK-123) or a GitHub issue (ID #12).
type LinkIssue struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Status    string `json:"status"`
	Assignee  string `json:"assignee,omitempty"`
	Milestone string `json:"milestone,omitempty"`
}

// SessionLinks is GET /v1/sessions/{id}/links: the PRs a session opened or works on (its
// branch's PR, a linked URL) and the issues named in its branch, PRs, prompts or /link refs.
type SessionLinks struct {
	Session string       `json:"session"`
	PRs     []ghx.PRLink `json:"prs"`
	Issues  []LinkIssue  `json:"issues"`
	Error   string       `json:"error,omitempty"`
	Checked time.Time    `json:"checked"`
}

func (a SessionLinks) same(b SessionLinks) bool {
	x, _ := json.Marshal([]any{a.PRs, a.Issues, a.Error})
	y, _ := json.Marshal([]any{b.PRs, b.Issues, b.Error})
	return string(x) == string(y)
}

// Linker keeps every session's links current: each session is polled on its own backoff
// (Base, doubled after each unchanged poll up to Max, back to Base on a change), and a change
// is pushed over the event stream as a session_links event.
type Linker struct {
	PR       func(url string) (ghx.PRLink, error)
	GHIssue  func(repo string, n int) (ghx.IssueLink, error)
	Linear   func(id string) (linear.Issue, error)
	BranchPR func(repo, branch string) (string, error)
	Branch   func(dir string) string
	Repo     func(dir string) string
	Base     time.Duration
	Max      time.Duration
	Now      func() time.Time

	run     sync.Mutex
	mu      sync.Mutex
	next    map[string]time.Time
	backoff map[string]time.Duration
	missing map[string]bool
}

// NewLinker is the daemon's linker over gh (or the token API) and the stored Linear key.
func NewLinker() *Linker {
	return &Linker{PR: ghx.LinkPR, GHIssue: ghx.LinkIssue, Linear: linear.Lookup, BranchPR: ghx.BranchPR,
		Branch: gitBranch, Base: time.Minute, Max: 10 * time.Minute}
}

func gitBranch(dir string) string {
	b, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func gitRepoName(dir string) string {
	b, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	r := repoOf(strings.TrimSpace(string(b)))
	if _, rest, ok := strings.Cut(r, "/"); ok {
		return rest
	}
	return ""
}

func (k *Linker) now() time.Time {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now()
}

func (k *Linker) every(id string) time.Duration {
	k.mu.Lock()
	defer k.mu.Unlock()
	if d := k.backoff[id]; d > 0 {
		return d
	}
	return k.Base
}

func (k *Linker) schedule(id string, changed bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.next == nil {
		k.next, k.backoff = map[string]time.Time{}, map[string]time.Duration{}
	}
	d := k.Base
	if prev, ok := k.backoff[id]; ok && !changed {
		d = min(prev*2, k.Max)
	}
	k.backoff[id], k.next[id] = d, k.now().Add(d)
}

func (k *Linker) due(id string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	n, ok := k.next[id]
	return !ok || !k.now().Before(n)
}

func (k *Linker) isMissing(key string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.missing[key]
}

func (k *Linker) setMissing(key string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.missing == nil {
		k.missing = map[string]bool{}
	}
	k.missing[key] = true
}

// Tick refreshes every session whose backoff ran out.
func (k *Linker) Tick(sv *Server) {
	for _, s := range sv.Sessions() {
		if k.due(s.ID) {
			sv.refreshLinks(s.ID)
		}
	}
}

// Run ticks every interval until ctx ends.
func (k *Linker) Run(ctx context.Context, sv *Server, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		k.Tick(sv)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

var (
	linearRefRe = regexp.MustCompile(`\b([A-Z][A-Z0-9]{0,9}-[0-9]{1,7})\b`)
	ghRefRe     = regexp.MustCompile(`(?:^|[^\w/&])#([0-9]{1,7})\b`)
)

// linkRefs is the Linear ids and GitHub issue numbers in a branch, explicit refs and texts,
// first seen first; teams, when set, keeps only those Linear team keys.
func linkRefs(branch string, texts, refs, teams []string) ([]string, []int) {
	var lin []string
	var gh []int
	addLin := func(s string) {
		for _, m := range linearRefRe.FindAllStringSubmatch(s, -1) {
			key, _, _ := strings.Cut(m[1], "-")
			if !slices.Contains(lin, m[1]) && (len(teams) == 0 || slices.Contains(teams, key)) {
				lin = append(lin, m[1])
			}
		}
	}
	addGH := func(s string) {
		for _, m := range ghRefRe.FindAllStringSubmatch(s, -1) {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && !slices.Contains(gh, n) {
				gh = append(gh, n)
			}
		}
	}
	addLin(strings.ToUpper(branch))
	for _, r := range refs {
		addLin(r)
		addGH(r)
	}
	for _, t := range texts {
		addLin(t)
		addGH(t)
	}
	return lin, gh
}

func linearTeams() []string {
	var out []string
	for _, t := range strings.Split(strings.ToUpper(config.Get("linear.team", "")), ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// refreshLinks reads one session's links and pushes them when they changed.
func (sv *Server) refreshLinks(id string) (SessionLinks, bool) {
	k := sv.Links
	k.run.Lock()
	defer k.run.Unlock()
	sv.mu.Lock()
	l := sv.sessions[id]
	if l == nil {
		sv.mu.Unlock()
		return SessionLinks{}, false
	}
	dir, prs, refs := l.s.Dir, slices.Clone(l.s.PRs), slices.Clone(l.s.Links)
	var texts []string
	for _, h := range l.history {
		if h.Role == "user" && len(texts) < 8 {
			texts = append(texts, clip(h.Text, linkTextMax))
		}
	}
	var prev SessionLinks
	if l.links != nil {
		prev = *l.links
	}
	sv.mu.Unlock()

	out := k.collect(id, dir, prs, refs, texts)
	changed := !out.same(prev)
	k.schedule(id, changed)
	sv.mu.Lock()
	if changed {
		if _, err := sv.appendLocked(KindSessionLinks, id, out); err != nil {
			sv.log("links: " + err.Error())
		}
		v := out
		l.links = &v
	} else if l.links != nil {
		l.links.Checked = out.Checked
	}
	sv.mu.Unlock()
	return out, changed
}

func (k *Linker) collect(id, dir string, prs, refs, texts []string) SessionLinks {
	repo := gitRepoName
	if k.Repo != nil {
		repo = k.Repo
	}
	return k.collectIn(id, repo(dir), k.Branch(dir), prs, refs, texts)
}

func (k *Linker) collectIn(id, repo, branch string, prs, refs, texts []string) SessionLinks {
	out := SessionLinks{Session: id, PRs: []ghx.PRLink{}, Issues: []LinkIssue{}, Checked: k.now().UTC()}
	var errs []string
	note := func(err error) {
		if err != nil && !errors.Is(err, ghx.ErrNotFound) && !errors.Is(err, linear.ErrNotFound) && !errors.Is(err, auto.ErrNoAccess) {
			if m := text.Clean(err.Error()); !slices.Contains(errs, m) {
				errs = append(errs, m)
			}
		}
	}
	urls := slices.Clone(prs)
	for _, r := range refs {
		if tendPRURL(r) {
			urls = append(urls, r)
		}
	}
	if repo != "" && branch != "" && k.BranchPR != nil {
		u, err := k.BranchPR(repo, branch)
		note(err)
		if tendPRURL(u) {
			urls = append(urls, u)
		}
	}
	seen := map[string]bool{}
	for _, u := range urls {
		if seen[u] || len(out.PRs) >= linksMax {
			continue
		}
		seen[u] = true
		p, err := k.PR(u)
		if err != nil {
			note(err)
			continue
		}
		out.PRs = append(out.PRs, p)
		texts = append(texts, p.Title, clip(p.Body, linkTextMax))
	}
	lin, gh := linkRefs(branch, texts, refs, linearTeams())
	for _, idv := range lin {
		if len(out.Issues) >= linksMax || k.isMissing("linear:"+idv) {
			continue
		}
		is, err := k.Linear(idv)
		if errors.Is(err, linear.ErrNotFound) {
			k.setMissing("linear:" + idv)
		}
		if err != nil {
			note(err)
			if errors.Is(err, auto.ErrNoAccess) {
				break
			}
			continue
		}
		out.Issues = append(out.Issues, LinkIssue{Kind: "linear", ID: is.ID, Title: is.Title, URL: is.URL, Status: is.Status, Assignee: is.Assignee, Milestone: is.Milestone})
	}
	for _, n := range gh {
		key := "gh:" + repo + "#" + strconv.Itoa(n)
		if repo == "" || len(out.Issues) >= linksMax || k.isMissing(key) {
			continue
		}
		is, err := k.GHIssue(repo, n)
		if errors.Is(err, ghx.ErrNotFound) {
			k.setMissing(key)
		}
		if err != nil {
			note(err)
			continue
		}
		out.Issues = append(out.Issues, LinkIssue{Kind: "github", ID: "#" + strconv.Itoa(is.Number), Title: is.Title, URL: is.URL, Status: is.State, Assignee: is.Assignee, Milestone: is.Milestone})
	}
	out.Error = strings.Join(errs, "; ")
	return out
}

// SessionLinksOf is the last links read for a session (replayed after a restart).
func (sv *Server) SessionLinksOf(id string) (SessionLinks, bool) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	l := sv.sessions[id]
	if l == nil || l.links == nil {
		return SessionLinks{}, false
	}
	return *l.links, true
}

func (sv *Server) getLinks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !sv.hasSession(id) {
		sessionErr(w, errNoSession)
		return
	}
	if l, ok := sv.SessionLinksOf(id); ok {
		writeJSON(w, 200, l)
		return
	}
	if sv.Links == nil {
		writeJSON(w, 200, SessionLinks{Session: id, PRs: []ghx.PRLink{}, Issues: []LinkIssue{}})
		return
	}
	l, _ := sv.refreshLinks(id)
	writeJSON(w, 200, l)
}

var ghIssueRefRe = regexp.MustCompile(`^#[0-9]{1,7}$`)

// validLinkRef is a ref /link accepts: a Linear id, #<n> or a GitHub PR URL.
func validLinkRef(s string) bool {
	return linear.IDRe.MatchString(s) || ghIssueRefRe.MatchString(s) || tendPRURL(s)
}

func (sv *Server) addLink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in struct{ Ref string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		http.Error(w, "body: {\"ref\": \"TK-123\" | \"#12\" | PR URL}", 400)
		return
	}
	ref := strings.TrimSpace(in.Ref)
	if !strings.HasPrefix(ref, "http") {
		ref = strings.ToUpper(ref)
	}
	if !validLinkRef(ref) {
		http.Error(w, "ref must be a Linear id (TK-123), a GitHub issue (#12) or a GitHub PR URL", 400)
		return
	}
	sv.mu.Lock()
	l := sv.sessions[id]
	if l == nil {
		sv.mu.Unlock()
		sessionErr(w, errNoSession)
		return
	}
	if !slices.Contains(l.s.Links, ref) {
		if len(l.s.Links) >= linksMax {
			sv.mu.Unlock()
			http.Error(w, "too many links on this session", 400)
			return
		}
		if _, err := sv.appendLocked(KindSessionLink, id, map[string]string{"ref": ref}); err != nil {
			sv.mu.Unlock()
			http.Error(w, err.Error(), 500)
			return
		}
		l.s.Links = append(l.s.Links, ref)
	}
	sv.mu.Unlock()
	if sv.Links == nil {
		writeJSON(w, 200, SessionLinks{Session: id, PRs: []ghx.PRLink{}, Issues: []LinkIssue{}})
		return
	}
	out, _ := sv.refreshLinks(id)
	writeJSON(w, 200, out)
}

func (sv *Server) hasSession(id string) bool {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.sessions[id] != nil
}

func (sv *Server) applyLinks(e store.Event) {
	l := sv.sessions[e.Session]
	if l == nil {
		return
	}
	switch e.Kind {
	case KindSessionLink:
		var v struct{ Ref string }
		if json.Unmarshal(e.Data, &v) == nil && validLinkRef(v.Ref) && !slices.Contains(l.s.Links, v.Ref) {
			l.s.Links = append(l.s.Links, v.Ref)
		}
	case KindSessionLinks:
		var v SessionLinks
		if json.Unmarshal(e.Data, &v) == nil {
			l.links = &v
		}
	}
}

// WorkPR is one open PR of a project and the sessions that link it.
type WorkPR struct {
	ghx.PRLink
	Sessions []string `json:"sessions"`
}

// WorkIssue is one linked issue of a project and the sessions that link it.
type WorkIssue struct {
	LinkIssue
	Sessions []string `json:"sessions"`
}

// ProjectWork is GET /v1/links?dir=: a project's open PRs and linked issues across its sessions.
type ProjectWork struct {
	PRs    []WorkPR    `json:"prs"`
	Issues []WorkIssue `json:"issues"`
}

func (sv *Server) projectWork(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("dir")
	if real, err := filepath.EvalSymlinks(root); root != "" && err == nil {
		root = real
	}
	out := ProjectWork{PRs: []WorkPR{}, Issues: []WorkIssue{}}
	for _, s := range sv.Sessions() {
		if root != "" && !InProject(s.Dir, root) {
			continue
		}
		l, ok := sv.SessionLinksOf(s.ID)
		if !ok {
			continue
		}
		for _, p := range l.PRs {
			if p.State != "OPEN" {
				continue
			}
			if i := slices.IndexFunc(out.PRs, func(x WorkPR) bool { return x.URL == p.URL }); i >= 0 {
				out.PRs[i].Sessions = append(out.PRs[i].Sessions, s.ID)
			} else {
				out.PRs = append(out.PRs, WorkPR{p, []string{s.ID}})
			}
		}
		for _, is := range l.Issues {
			if i := slices.IndexFunc(out.Issues, func(x WorkIssue) bool { return x.Kind == is.Kind && x.URL == is.URL }); i >= 0 {
				out.Issues[i].Sessions = append(out.Issues[i].Sessions, s.ID)
			} else {
				out.Issues = append(out.Issues, WorkIssue{is, []string{s.ID}})
			}
		}
	}
	writeJSON(w, 200, out)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
