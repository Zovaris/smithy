//go:build harness

package chat

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/tui/kit"
)

// linksCommand is /links: the open session's links; /links add <url|ref> links one, /links rm
// <n|url> removes one (n as the modal numbers them); /links <ref> still adds.
func (m chatView) linksCommand(arg string) (tea.Model, tea.Cmd) {
	if m.cfg == nil {
		m.system("links need a daemon this chat can reach")
		return m, nil
	}
	if m.current == "" {
		m.system("open a session first: /links shows the open session's PRs and issues")
		return m, nil
	}
	verb, rest, _ := strings.Cut(strings.TrimSpace(arg), " ")
	rest = strings.TrimSpace(rest)
	switch verb {
	case "rm", "remove":
		if rest == "" {
			m.system("usage: /links rm <n|url>")
			return m, nil
		}
		if n, err := strconv.Atoi(rest); err == nil {
			cl, id := m.cfg, m.current
			return m, func() tea.Msg {
				ctx, cancel := daemonCtx()
				defer cancel()
				l, err := cl.SessionLinks(ctx, id)
				if err != nil {
					return sessionErrMsg{what: "/links", err: err}
				}
				all := linkURLs(l)
				if n < 1 || n > len(all) {
					return settingsDoneMsg{lines: []string{"no link " + rest + "; /links numbers them"}}
				}
				if l, err = cl.RemoveLink(ctx, id, all[n-1]); err != nil {
					return sessionErrMsg{what: "/links rm", err: err}
				}
				return settingsDoneMsg{panel: linksPanel(l)}
			}
		}
		return m.removeLink(rest)
	case "add":
		if rest == "" {
			m.system("usage: /links add <url | TK-123 | #12>")
			return m, nil
		}
		arg = rest
	}
	cl, id, ref := m.cfg, m.current, strings.TrimSpace(arg)
	return m, func() tea.Msg {
		ctx, cancel := daemonCtx()
		defer cancel()
		var l api.SessionLinks
		var err error
		if ref != "" {
			l, err = cl.AddLink(ctx, id, ref)
		} else {
			l, err = cl.SessionLinks(ctx, id)
		}
		if err != nil {
			return sessionErrMsg{what: "/links", err: err}
		}
		return settingsDoneMsg{panel: linksPanel(l)}
	}
}

// removeLink drops u from the open session's links and shows what is left.
func (m chatView) removeLink(u string) (tea.Model, tea.Cmd) {
	if m.cfg == nil || m.current == "" {
		return m, nil
	}
	cl, id := m.cfg, m.current
	return m, func() tea.Msg {
		ctx, cancel := daemonCtx()
		defer cancel()
		l, err := cl.RemoveLink(ctx, id, u)
		if err != nil {
			return sessionErrMsg{what: "/links rm", err: err}
		}
		return settingsDoneMsg{panel: linksPanel(l)}
	}
}

// openLink opens u in the browser.
func (m *chatView) openLink(u string) tea.Cmd {
	open := m.opener
	if open == nil {
		open = kit.OpenURL
	}
	m.status("opening " + u)
	return func() tea.Msg {
		if err := open(u); err != nil {
			return sessionErrMsg{what: "open the link", err: err}
		}
		return nil
	}
}

// linksKey is the /links modal's keys on the selected link: enter opens it, c copies its URL
// (OSC 52), x or delete removes it.
func (m chatView) linksKey(k string) (tea.Model, tea.Cmd, bool) {
	u := ""
	if i := m.pal.sel; i >= 0 && i < len(m.info.urls) {
		u = m.info.urls[i]
	}
	switch k {
	case "enter":
		if u == "" {
			return m, nil, true
		}
		return m, m.openLink(u), true
	case "c":
		if u == "" {
			return m, nil, true
		}
		m.status("copied " + u)
		return m, tea.SetClipboard(u), true
	case "x", "delete":
		if u == "" {
			return m, nil, true
		}
		n, cmd := m.removeLink(u)
		return n, cmd, true
	}
	return m, nil, false
}

// linkURLs is every link of l in the order /links numbers them.
func linkURLs(l api.SessionLinks) []string {
	var out []string
	for _, p := range l.PRs {
		out = append(out, p.URL)
	}
	for _, is := range l.Issues {
		out = append(out, is.URL)
	}
	for _, u := range l.URLs {
		out = append(out, u.URL)
	}
	return out
}

func prLine(p ghx.PRLink) string {
	parts := []string{p.State, "+" + strconv.Itoa(p.Additions) + " −" + strconv.Itoa(p.Deletions)}
	if p.Draft {
		parts = append(parts, "draft")
	}
	if p.Checks != "" {
		parts = append(parts, "checks "+p.Checks)
	}
	if p.Review != "" {
		parts = append(parts, strings.ToLower(strings.ReplaceAll(p.Review, "_", " ")))
	}
	if p.Mergeable != "" {
		parts = append(parts, strings.ToLower(p.Mergeable))
	}
	return strings.Join(parts, " · ")
}

// linksPanel is the /links modal: each PR with state, checks, review, mergeable and lines;
// each issue with status, assignee and milestone; every row ends in its link.
func linksPanel(l api.SessionLinks) *infoPanel {
	lines, urls := []string{"#PULL REQUESTS"}, []string{""}
	n := 0
	add := func(u string, ls ...string) {
		for _, x := range ls {
			lines, urls = append(lines, x), append(urls, u)
		}
	}
	num := func() string { n++; return strconv.Itoa(n) + "  " }
	for _, p := range l.PRs {
		mark := map[string]string{"green": "ok ", "red": "!! "}[p.Checks]
		add(p.URL, mark+num()+"PR #"+strconv.Itoa(p.Number)+" "+p.Title+"\t"+prLine(p), "-> "+p.URL)
	}
	if len(l.PRs) == 0 {
		add("", "none: no PR opened by this session or on its branch")
	}
	add("", "#ISSUES")
	for _, is := range l.Issues {
		hint := []string{is.Status}
		if is.Assignee != "" {
			hint = append(hint, is.Assignee)
		}
		if is.Milestone != "" {
			hint = append(hint, is.Milestone)
		}
		label := is.ID
		if is.Kind == "github" {
			label = "issue " + is.ID
		}
		add(is.URL, num()+label+" "+is.Title+"\t"+strings.Join(hint, " · "), "-> "+is.URL)
	}
	if len(l.Issues) == 0 {
		add("", "none: name one in the branch, a PR or a prompt, or /links add TK-123")
	}
	if len(l.URLs) > 0 {
		add("", "#LINKS")
		for _, u := range l.URLs {
			add(u.URL, num()+u.Title+"\t"+u.Host)
		}
	}
	if l.Error != "" {
		add("", "#ERROR", "!! "+l.Error)
	}
	add("", "#ADD", "/links add <url | TK-123 | #12>  ·  /links rm <n|url>")
	return &infoPanel{title: "Links", lines: lines, scroll: true, urls: urls}
}

// linksHint is the agents panel's one-line summary of a session's links.
func linksHint(l api.SessionLinks, sep string) string {
	var parts []string
	for _, p := range l.PRs {
		s := "PR #" + strconv.Itoa(p.Number)
		if p.Checks != "" {
			s += " " + p.Checks
		} else if p.State != "OPEN" {
			s += " " + strings.ToLower(p.State)
		}
		parts = append(parts, s+" +"+strconv.Itoa(p.Additions)+" −"+strconv.Itoa(p.Deletions))
	}
	for _, is := range l.Issues {
		parts = append(parts, is.ID+" "+is.Status)
	}
	return strings.Join(parts, sep)
}

// linkChanges is one line per PR whose checks turned red or green or whose state changed
// (merged, closed), and per issue whose status changed, since prev; none on the first read.
func linkChanges(prev *api.SessionLinks, l api.SessionLinks) []string {
	if prev == nil {
		return nil
	}
	var out []string
	for _, p := range l.PRs {
		for _, o := range prev.PRs {
			if o.URL != p.URL {
				continue
			}
			name := "PR #" + strconv.Itoa(p.Number)
			if p.State != o.State && p.State != "" {
				out = append(out, name+" "+strings.ToLower(p.State))
			} else if p.Checks != o.Checks && (p.Checks == "red" || p.Checks == "green") {
				out = append(out, name+" checks "+p.Checks)
			}
		}
	}
	for _, is := range l.Issues {
		for _, o := range prev.Issues {
			if o.URL == is.URL && o.Status != is.Status && is.Status != "" {
				out = append(out, is.ID+" "+is.Status)
			}
		}
	}
	return out
}
