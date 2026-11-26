//go:build harness

package tui

import (
	"strings"
	"testing"
)

func skillScene(f *fakeSessions) chatView {
	s := firstRunSetup()
	s.s.Chosen = true
	s.s.Skills = append(s.s.Skills, "pdf")
	s.s.Paths = map[string]string{"smelt": "/h/.claude/skills/smelt/SKILL.md", "hush": "/h/.claude/skills/hush/SKILL.md", "pdf": "/h/.agents/skills/pdf/SKILL.md"}
	m := setupScene(s)
	m.sess, m.current = f, ""
	return m
}

func TestSkillInvokedFromChat(t *testing.T) {
	f := &fakeSessions{}
	m := skillScene(f)
	m = cpress(typeIn(m, "/pdf merge a.pdf and b.pdf"), ckey("enter"))
	if len(f.started) != 1 || !strings.Contains(f.started[0], "/h/.agents/skills/pdf/SKILL.md") || !strings.HasSuffix(f.started[0], "merge a.pdf and b.pdf") {
		t.Fatalf("/pdf sends the skill and the task: %q", f.started)
	}
	m = cpress(typeIn(m, "$smelt"), ckey("enter"))
	if len(f.sent) != 1 || !strings.Contains(f.sent[0][1], "/h/.claude/skills/smelt/SKILL.md") {
		t.Fatalf("$smelt sends the skill: %q", f.sent)
	}
	m = cpress(typeIn(m, "$nope hi"), ckey("enter"))
	if len(f.sent) != 2 || f.sent[1][1] != "$nope hi" {
		t.Fatalf("an unknown $name is plain text: %q", f.sent)
	}
	m = cpress(typeIn(m, "/nope"), ckey("enter"))
	if len(f.sent) != 2 || !strings.Contains(screen(m), "unknown command /nope") {
		t.Fatalf("an unknown /name stays an error: %q", f.sent)
	}
}

func TestSlashPopupAndHelpListSkills(t *testing.T) {
	m := skillScene(&fakeSessions{})
	m.width, m.height = 160, 48
	if out := screen(typeIn(m, "/pd")); !strings.Contains(out, "/pdf") {
		t.Fatalf("popup lists skills:\n%s", out)
	}
	m.input.SetValue("")
	m = cpress(typeIn(m, "/help"), ckey("enter"))
	if out := screen(m); !strings.Contains(out, "$name") {
		t.Fatalf("help documents invoking a skill:\n%s", out)
	}
	if out := screen(typeIn(m, "pdf")); !strings.Contains(out, "$pdf") {
		t.Fatalf("help lists each skill:\n%s", out)
	}
	m = cpress(m, ckey("esc"))
	m = cpress(typeIn(m, "/skills"), ckey("enter"))
	if out := screen(m); !strings.Contains(out, "pdf") || !strings.Contains(out, ".agents/skills") {
		t.Fatalf("/skills shows where each skill lives:\n%s", out)
	}
}
