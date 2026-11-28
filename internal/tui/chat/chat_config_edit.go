//go:build harness

package chat

import (
	"cmp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/api"
)

// advGroups are the topics the Advanced settings sit under, in order, by key prefix.
var advGroups = []struct {
	name     string
	prefixes []string
}{
	{"Batches", []string{"tasks.", "git.", "warden.", "route.", "stack.", "ask."}},
	{"Watch", []string{"watch."}},
	{"Guard", []string{"guard."}},
	{"Decisions", []string{"decide."}},
	{"Sandbox", []string{"auto.sandbox"}},
	{"Serve", []string{"auto.", "pool."}},
	{"Daemon", []string{"daemon."}},
	{"Sessions", []string{"session."}},
	{"Agent", []string{"agent.", "kit.", "models."}},
	{"Chat apps", []string{"connect.", "notify."}},
	{"Board and Linear", []string{"board.", "linear."}},
	{"Look", []string{"ui."}},
}

// advGroup is the topic an Advanced setting is listed under; "Other" for an unknown prefix.
func advGroup(k string) string {
	for _, g := range advGroups {
		for _, p := range g.prefixes {
			if strings.HasPrefix(k, p) {
				return g.name
			}
		}
	}
	return "Other"
}

// advLabels name the Advanced settings so they read without the config key.
var advLabels = map[string]string{
	"tasks.location": "Task lists live in", "tasks.commit": "Commit specs and tasks", "tasks.ignore": "Keep uncommitted tasks out by",
	"git.branch": "Branch name template", "git.worktree": "Worktree path template",
	"warden.merge": "Merging green batch PRs", "warden.cleanup": "Cleanup after a batch", "warden.max": "Parallel workers at most",
	"warden.mode": "Batch check speed", "warden.stuck_minutes": "Worker stuck after (min)", "warden.worker_model": "Worker model",
	"route.domain": "Product nouns that are not risk", "stack.test": "Test command", "ask.timeout": "Wait for your answer (min)",
	"watch.events": "Events that wake a waiting agent", "watch.every": "Poll GitHub every (s)", "watch.max": "Give up waiting after (min)",
	"guard.enabled": "Dangerous-command guard", "guard.block": "Also block (regexes)", "guard.allow": "Never block (regexes)",
	"guard.fetch_block": "Ask before fetching from", "guard.fetch_allow": "Fetch without asking from",
	"decide.url": "Decision model endpoint", "decide.model": "Decision model", "decide.key_env": "Decision key variable",
	"decide.enabled": "Decision model on", "decide.timeout": "Decision call timeout (ms)",
	"auto.sources": "Serve takes issues from", "auto.mode": "Serve takes which issues", "auto.label": "Label that opts an issue in",
	"auto.model_prefix": "Label prefix for the model tier", "auto.speed_prefix": "Label prefix for the speed",
	"auto.retry_stronger": "Retry blocked issues on a stronger model", "auto.hours": "Serve launches only between",
	"auto.every": "Serve polls each source every (s)", "auto.paused": "Serve paused", "pool.order": "Pool tries harnesses in order",
	"auto.session_hours": "Orchestrator session ends after (h)", "auto.merge": "Auto batches merge",
	"auto.sandbox": "Unattended workers' sandbox", "auto.sandbox_domains": "Sandbox may also reach",
	"auto.sandbox_weaker_net": "Sandbox lets Go CLIs verify TLS",
	"daemon.port":             "Daemon port on this machine", "daemon.listen": "Daemon Tailscale address", "daemon.allow": "Tailscale logins allowed",
	"session.agent": "New sessions start with agent", "session.model": "New sessions start with model",
	"session.goal_max_turns": "Goal stops after (turns)", "session.goal_max_cost": "Goal stops after spending ($)",
	"kit.skills":              "Skills given to every agent",
	"connect.telegram.notify": "Telegram alerts channel", "connect.telegram.chat": "Telegram chat channel",
	"connect.telegram.kinds": "Telegram sends these kinds", "connect.telegram.users": "Telegram users who may answer",
	"connect.slack.notify": "Slack alerts channel", "connect.slack.chat": "Slack chat channel", "connect.slack.channel": "Slack channel to post in",
	"connect.slack.kinds": "Slack sends these kinds", "connect.slack.users": "Slack users who may answer",
	"connect.discord.notify": "Discord alerts channel", "connect.discord.chat": "Discord chat channel", "connect.discord.channel": "Discord channel to post in",
	"connect.discord.users": "Discord users who may answer", "connect.discord.kinds": "Discord sends these kinds",
	"board.linear_sync": "Board pulls from Linear", "linear.key_env": "Linear key variable", "linear.team": "Linear teams",
	"linear.project": "Linear projects", "linear.milestone": "Linear milestone", "linear.cycle": "Linear cycle",
	"linear.initiative": "Linear initiative", "linear.states": "Linear states that count as new work",
}

// notifyKinds are the notification kinds a chat app can send.
var notifyKinds = []string{"pr_ready", "blocked", "stuck", "decision", "orchestrator_failed", "serve_down", "cap_reached", "limited"}

// listChoices are the values a comma-list setting picks from; nil when any text goes.
func listChoices(r api.SettingRow) []string {
	switch {
	case r.Key == "notify.kinds" || strings.HasPrefix(r.Key, "connect.") && strings.HasSuffix(r.Key, ".kinds"):
		return notifyKinds
	case r.Key == "watch.events":
		return []string{"checks", "merged", "closed", "conflict", "review", "comment", "push", "new", "updated"}
	case r.Key == "tend.watch":
		return []string{"prs", "issues", "actions", "base", "alerts", "all"}
	case r.Key == "auto.sources":
		return []string{"github", "linear", "board"}
	case r.Key == "linear.states":
		return []string{"triage", "backlog", "unstarted", "started"}
	case r.Key == "pool.order":
		return agents.CLIs
	}
	return nil
}

// cfgEditor is the inline editor open under a /config row: a picker for a fixed set, checkboxes
// for a comma list, or a text field.
type cfgEditor struct {
	key, kind string // kind: pick, multi or text
	opts      []string
	sel       int
	on        map[string]bool
	text      string
}

// editorFor is the editor enter opens on r.
func editorFor(r api.SettingRow) *cfgEditor {
	val := cmp.Or(r.Value, r.Default)
	opts := r.Options
	if r.Key == "auto.agent" || r.Key == "session.agent" {
		opts = append([]string{""}, agents.CLIs...)
	}
	if len(opts) > 0 {
		return &cfgEditor{key: r.Key, kind: "pick", opts: opts, sel: max(slices.Index(opts, val), 0)}
	}
	if c := listChoices(r); c != nil {
		e := &cfgEditor{key: r.Key, kind: "multi", opts: slices.Clone(c), on: map[string]bool{}}
		for _, v := range strings.Split(val, ",") {
			if v = strings.TrimSpace(v); v != "" {
				e.on[v] = true
				if !slices.Contains(e.opts, v) {
					e.opts = append(e.opts, v)
				}
			}
		}
		return e
	}
	if r.Secret {
		val = ""
	}
	return &cfgEditor{key: r.Key, kind: "text", text: val}
}

// value is what the editor saves.
func (e *cfgEditor) value() string {
	switch e.kind {
	case "pick":
		return e.opts[e.sel]
	case "multi":
		var out []string
		for _, o := range e.opts {
			if e.on[o] {
				out = append(out, o)
			}
		}
		return strings.Join(out, ",")
	}
	return strings.TrimSpace(e.text)
}

// editKey handles a key while the inline editor is open.
func (m chatView) editKey(msg tea.KeyPressMsg, r api.SettingRow) (tea.Model, tea.Cmd) {
	e := *m.cfgEd
	k := msg.String()
	switch {
	case k == "esc":
		m.cfgEd = nil
		return m, nil
	case k == "enter":
		m.cfgEd = nil
		return m.applySetting(r, e.value())
	case e.kind != "text" && k == "up":
		e.sel = wrapStep(e.sel, -1, len(e.opts))
	case e.kind != "text" && k == "down":
		e.sel = wrapStep(e.sel, 1, len(e.opts))
	case e.kind == "multi" && (k == "space" || k == "x"):
		on := map[string]bool{}
		for o, v := range e.on {
			on[o] = v
		}
		on[e.opts[e.sel]] = !on[e.opts[e.sel]]
		e.on = on
	case e.kind == "text" && k == "backspace":
		if r := []rune(e.text); len(r) > 0 {
			e.text = string(r[:len(r)-1])
		}
	case e.kind == "text" && k == "ctrl+u":
		e.text = ""
	case e.kind == "text" && k == "space":
		e.text += " "
	case e.kind == "text" && msg.Text != "":
		e.text += msg.Text
	}
	m.cfgEd = &e
	return m, nil
}
