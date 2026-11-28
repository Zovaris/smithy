//go:build harness

package chat

import (
	"context"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/ghx"
)

// linksCommand is /links: the open session's PRs and issues, or /links <ref> to link one.
func (m chatView) linksCommand(arg string) (tea.Model, tea.Cmd) {
	if m.cfg == nil {
		m.system("links need a daemon this chat can reach")
		return m, nil
	}
	if m.current == "" {
		m.system("open a session first: /links shows the open session's PRs and issues")
		return m, nil
	}
	cl, id, ref := m.cfg, m.current, strings.TrimSpace(arg)
	return m, func() tea.Msg {
		var l api.SessionLinks
		var err error
		if ref != "" {
			l, err = cl.AddLink(context.Background(), id, ref)
		} else {
			l, err = cl.SessionLinks(context.Background(), id)
		}
		if err != nil {
			return sessionErrMsg{what: "/links", err: err}
		}
		return settingsDoneMsg{panel: linksPanel(l)}
	}
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
	lines := []string{"#PULL REQUESTS"}
	for _, p := range l.PRs {
		mark := map[string]string{"green": "ok ", "red": "!! "}[p.Checks]
		lines = append(lines, mark+"PR #"+strconv.Itoa(p.Number)+" "+p.Title+"\t"+prLine(p), "-> "+p.URL)
	}
	if len(l.PRs) == 0 {
		lines = append(lines, "none: no PR opened by this session or on its branch")
	}
	lines = append(lines, "#ISSUES")
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
		lines = append(lines, label+" "+is.Title+"\t"+strings.Join(hint, " · "), "-> "+is.URL)
	}
	if len(l.Issues) == 0 {
		lines = append(lines, "none: name one in the branch, a PR or a prompt, or /links TK-123")
	}
	if l.Error != "" {
		lines = append(lines, "#ERROR", "!! "+l.Error)
	}
	return &infoPanel{title: "Links", lines: lines, scroll: true}
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
