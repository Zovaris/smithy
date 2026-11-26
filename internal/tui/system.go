package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/notify"
	"github.com/Zovaris/smithy/internal/pool"
	"github.com/Zovaris/smithy/internal/text"
	"github.com/Zovaris/smithy/internal/usage"
	"github.com/Zovaris/smithy/internal/warden"
)

// System is the read-only health report of the System view.
type System struct {
	Doctor   []string
	Settings []Setting
	Auth     []string // "<service> ****abcd"
	Binary   string
	Skills   string
	Home     string
	Events   string   // events.jsonl path and size
	Notify   []string // notify.Status lines (token masked)
	Pool     []pool.Row
	Err      []string
}

// loadSystem runs `smithy doctor` (this binary, 20 s cap) and reads the settings, auth and paths.
func loadSystem() System {
	s := System{Binary: Version, Skills: or(warden.SkillsVersion()), Home: layout.Home()}
	if exe, err := os.Executable(); err == nil && strings.HasPrefix(strings.ToLower(filepath.Base(exe)), "smithy") {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		out, err := exec.CommandContext(ctx, exe, "doctor").CombinedOutput()
		cancel()
		s.Doctor = cleanAll(strings.Split(strings.TrimRight(string(out), "\n"), "\n"))
		if err != nil {
			s.Doctor = append(s.Doctor, "doctor: "+clean(err.Error()))
		}
	}
	s.Settings = settingRows(currentHarness(), config.Effective)
	if l, err := auth.List(); err == nil {
		s.Auth = cleanAll(l)
		s.Settings = grouped(append(keyRows(s.Auth), s.Settings...))
	} else {
		s.Err = append(s.Err, "auth: "+clean(err.Error()))
	}
	s.Notify = cleanAll(notify.Status())
	if rows, err := pool.Rows(time.Now()); err == nil {
		s.Pool = rows
	} else {
		s.Err = append(s.Err, "pool: "+clean(err.Error()))
	}
	s.Events = events.File() + "  (none yet)"
	if fi, err := os.Stat(events.File()); err == nil {
		s.Events = fmt.Sprintf("%s  (%.1f KB)", events.File(), float64(fi.Size())/1024)
	}
	return s
}

func demoSystem() System {
	return System{
		Doctor: []string{"warden doctor", "ok   binary v0.15.0", "ok   git found", "ok   gh found and authenticated", "ok   claude found",
			"ok   worktree root /home/dev/worktrees writable", "ok   sandbox: native (claude, hosts github.com,api.github.com,api.anthropic.com,api.linear.app)", "warn 2 orphaned worktrees (smithy warden cleanup)"},
		Settings: grouped(append(keyRows([]string{"github ****9f2c", "linear ****a41b"}), settingRows("claude", demoEffective)...)),
		Auth:     []string{"github ****9f2c", "linear ****a41b"},
		Notify: []string{"token  ****x7Qa (/home/dev/.smithy/auth.json)", "chat 123456789", "kinds  pr_ready,blocked,stuck,decision,orchestrator_failed,serve_down,cap_reached,limited",
			"state  on (smithy auto serve sends)", "last sent 2026-11-23 09:12"},
		Pool: []pool.Row{{Name: "work", Harness: "claude", Status: "ok", Resets: "-", Runs: 6, Cost: ptr(3.42)},
			{Name: "home", Harness: "claude", Status: "limited", Resets: "11-23 14:00", Runs: 9, Cost: ptr(5.10)},
			{Name: "codex", Harness: "codex", Status: "ok", Resets: "-", Runs: 2}},
		Binary: "0.15.0", Skills: "0.15.0", Home: "/home/dev/.smithy", Events: "/home/dev/.smithy/events.jsonl  (412.7 KB)",
	}
}

func ptr(f float64) *float64 { return &f }

var demoValues = map[string]string{"auto.sources": "github,linear", "auto.max_per_day": "80", "linear.team": "TES",
	"warden.merge": "manual", "notify.telegram.chat": "123456789", "models.claude.cheap": "haiku", "models.claude.strong": "opus"}

// demoEffective is Effective over a fixed user file, so demo mode reads nothing.
func demoEffective(key string) (string, string, error) {
	if v, ok := demoValues[key]; ok {
		return v, "user", nil
	}
	for _, k := range config.Known {
		if k.Key == key && k.Default != "" && k.Default != "ask" {
			return k.Default, "default", nil
		}
	}
	return "", "", config.ErrUnset
}

// systemLines renders the report as two columns of sections.
func (m Model) systemLines(w, rw, rh int) (left, right []string) {
	t := m.th
	mut, val, head := t.fg(t.muted), t.fg(t.text), t.fg(t.accent).Bold(!t.plain)
	if m.sys == nil {
		return []string{"", mut.Italic(true).Render("Running smithy doctor…")}, nil
	}
	s := m.sys
	ver := t.fg(t.ok).Render(t.g.pass + " match")
	if s.Binary != s.Skills && s.Skills != "-" {
		ver = t.fg(t.err).Render(t.g.fail + " mismatch: smithy doctor says how to fix it")
	}
	left = append(left, head.Render("Versions"), mut.Render("binary  ")+val.Render(s.Binary), mut.Render("skills  ")+val.Render(s.Skills)+"  "+ver, "",
		head.Render("Serve")+mut.Render("  smithy auto serve"))
	sv := m.snap.Watcher.Serve
	switch sv.State {
	case "running", "stale":
		c := t.ok
		if sv.State == "stale" {
			c = t.warn
		}
		cycle := "-"
		if !sv.LastCycle.IsZero() {
			cycle = text.Ago(m.snap.At.Sub(sv.LastCycle))
		}
		left = append(left, t.fg(c).Render(sv.State)+mut.Render(fmt.Sprintf("  pid %d %s agent ", sv.PID, t.g.dot))+val.Render(sv.Agent)+
			mut.Render(" "+t.g.dot+" "+or(sv.Loop)+" "+t.g.dot+" last cycle ")+val.Render(cycle))
	default:
		left = append(left, mut.Render("not running (start: smithy auto serve)"))
	}
	if sv.SessionLog != "" {
		left = append(left, mut.Render("session ")+val.Render(short(layout.Tilde(sv.SessionLog), max(w-12, 20)))+mut.Render("  l opens it"))
	}
	left = append(left, "",
		head.Render("Paths"), mut.Render("home    ")+val.Render(layout.Tilde(s.Home)), mut.Render("events  ")+val.Render(layout.Tilde(s.Events)), "",
		head.Render("Auth")+mut.Render("  keys are masked"))
	if len(s.Auth) == 0 {
		left = append(left, mut.Render("no keys stored (smithy auth set <service>)"))
	}
	for _, a := range s.Auth {
		left = append(left, val.Render(a))
	}
	left = append(left, "", head.Render("Notify")+mut.Render("  Telegram, smithy notify test"))
	for _, l := range s.Notify {
		c := t.text
		switch {
		case strings.HasPrefix(l, "last error"):
			c = t.err
		case strings.HasPrefix(l, "state  off"):
			c = t.muted
		}
		left = append(left, t.fg(c).Render(short(l, w)))
	}
	left = append(left, "", head.Render("Pool")+mut.Render("  smithy pool"))
	if len(s.Pool) == 0 {
		left = append(left, mut.Render("off (smithy pool add <name> <harness>)"))
	} else {
		left = append(left, mut.Render(fmt.Sprintf("%-10s %-7s %-8s %-11s %4s %7s", "account", "harness", "status", "resets", "runs", "cost")))
	}
	for _, p := range s.Pool {
		c := t.text
		if p.Status == "limited" {
			c = t.warn
		}
		left = append(left, t.fg(c).Render(short(fmt.Sprintf("%-10s %-7s %-8s %-11s %4d %7s", clean(p.Name), p.Harness, p.Status, p.Resets, p.Runs, usage.Cost(p.Cost)), w)))
	}
	left = append(left, "", head.Render("Doctor"))
	for _, d := range s.Doctor {
		c := t.text
		switch {
		case strings.HasPrefix(d, "ok"):
			c = t.ok
		case strings.HasPrefix(d, "warn"):
			c = t.warn
		case strings.HasPrefix(d, "bad"), strings.HasPrefix(d, "fail"), strings.HasPrefix(d, "doctor:"):
			c = t.err
		}
		left = append(left, t.fg(c).Render(short(d, w)))
	}
	for _, e := range s.Err {
		left = append(left, t.fg(t.err).Render(e))
	}
	right = m.settingLines(rw, rh)
	return left, right
}
