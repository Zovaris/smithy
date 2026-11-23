package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/warden"
)

// System is the read-only health report of the System view.
type System struct {
	Doctor   []string
	Settings [][3]string // key, value, where it comes from
	Auth     []string    // "<service> ****abcd"
	Binary   string
	Skills   string
	Home     string
	Events   string // events.jsonl path and size
	Err      []string
}

// loadSystem runs `smithy doctor` (this binary, 20 s cap) and reads the settings, auth and paths.
func loadSystem() System {
	s := System{Binary: Version, Skills: or(warden.SkillsVersion()), Home: ghx.Home()}
	if exe, err := os.Executable(); err == nil && strings.HasPrefix(strings.ToLower(filepath.Base(exe)), "smithy") {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		out, err := exec.CommandContext(ctx, exe, "doctor").CombinedOutput()
		cancel()
		s.Doctor = cleanAll(strings.Split(strings.TrimRight(string(out), "\n"), "\n"))
		if err != nil {
			s.Doctor = append(s.Doctor, "doctor: "+clean(err.Error()))
		}
	}
	known := append([]config.Setting(nil), config.Known...)
	rank := map[string]int{"auto": 0, "linear": 1, "warden": 2}
	sort.SliceStable(known, func(i, j int) bool {
		return rank[strings.SplitN(known[i].Key, ".", 2)[0]] < rank[strings.SplitN(known[j].Key, ".", 2)[0]]
	})
	for _, k := range known {
		if !strings.HasPrefix(k.Key, "auto.") && !strings.HasPrefix(k.Key, "linear.") && !strings.HasPrefix(k.Key, "warden.") {
			continue
		}
		v, src, err := config.Effective(k.Key)
		if err != nil {
			v, src = "-", "unset"
		}
		s.Settings = append(s.Settings, [3]string{k.Key, clean(v), src})
	}
	if l, err := auth.List(); err == nil {
		s.Auth = cleanAll(l)
	} else {
		s.Err = append(s.Err, "auth: "+clean(err.Error()))
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
			"ok   worktree root /home/dev/worktrees writable", "warn 2 orphaned worktrees (smithy warden cleanup)"},
		Settings: [][3]string{{"auto.sources", "github,linear", "user"}, {"auto.mode", "labeled", "default"}, {"auto.label", "auto", "default"},
			{"auto.max_per_day", "80", "user"}, {"auto.hours", "-", "unset"}, {"auto.every", "60", "default"}, {"auto.paused", "false", "default"},
			{"linear.team", "TES", "user"}, {"linear.project", "-", "unset"}, {"warden.max", "5", "default"}, {"warden.merge", "manual", "user"},
			{"warden.cleanup", "auto", "default"}, {"warden.mode", "smelt normal", "default"}},
		Auth:   []string{"github ****9f2c", "linear ****a41b"},
		Binary: "0.15.0", Skills: "0.15.0", Home: "/home/dev/.smithy", Events: "/home/dev/.smithy/events.jsonl  (412.7 KB)",
	}
}

// systemLines renders the report as two columns of sections.
func (m Model) systemLines(w int) (left, right []string) {
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
		head.Render("Paths"), mut.Render("home    ")+val.Render(home(s.Home)), mut.Render("events  ")+val.Render(home(s.Events)), "",
		head.Render("Auth")+mut.Render("  keys are masked"))
	if len(s.Auth) == 0 {
		left = append(left, mut.Render("no keys stored (smithy auth set <service>)"))
	}
	for _, a := range s.Auth {
		left = append(left, val.Render(a))
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
	group := ""
	for _, kv := range s.Settings {
		g := strings.SplitN(kv[0], ".", 2)[0]
		if g != group {
			if group != "" {
				right = append(right, "")
			}
			group = g
			right = append(right, head.Render(g+".*"))
		}
		right = append(right, val.Render(fmt.Sprintf("%-21s", kv[0]))+" "+val.Render(fmt.Sprintf("%-16s", short(kv[1], 16)))+" "+mut.Render(kv[2]))
	}
	right = append(right, "", mut.Italic(true).Render("Read-only here: change with smithy config set <key> <value>."))
	return left, right
}
