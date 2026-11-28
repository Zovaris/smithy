//go:build harness

package chat

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/tui/kit"
)

// bgSpan is the first and last column of line that carries a background colour, below limit.
func bgSpan(line string, limit int) (lo, hi int) {
	lo, hi = -1, -1
	col, on := 0, false
	for i := 0; i < len(line); {
		if line[i] == 0x1b && i+1 < len(line) && line[i+1] == '[' {
			j := i + 2
			for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
				j++
			}
			if j < len(line) && line[j] == 'm' {
				ps := strings.Split(line[i+2:j], ";")
				for k := 0; k < len(ps); k++ {
					n, _ := strconv.Atoi(ps[k])
					switch {
					case ps[k] == "" || n == 0 || n == 49:
						on = false
					case n == 38 || n == 48:
						on = on || n == 48
						if k+1 < len(ps) && ps[k+1] == "2" {
							k += 4
						} else {
							k += 2
						}
					case n >= 40 && n <= 47 || n >= 100 && n <= 107:
						on = true
					}
				}
			}
			i = j + 1
			continue
		}
		r, size := []rune(line[i:])[0], len(string([]rune(line[i:])[0]))
		if on && col < limit {
			if lo < 0 {
				lo = col
			}
			hi = col
		}
		col += max(ansi.StringWidth(string(r)), 1)
		i += size
	}
	return lo, hi
}

func gridScene(w int) chatView {
	m := layoutView(w)
	m.th = kit.NamedTheme("steel", true, colorprofile.TrueColor, true)
	m.height = 44
	m.push(chatMsg{who: "you", text: "read go.mod"})
	m.push(chatMsg{who: "daemon", tool: &chatTool{Name: "Read", Arg: "go.mod", Status: "done"}})
	m.push(chatMsg{who: "agent", text: "It uses four Charm libraries."})
	m.push(chatMsg{who: "daemon", kind: "foot", text: "claude · haiku · 2.0s"})
	m.push(chatMsg{who: "daemon", kind: "shell", sh: &shellBlock{cmd: "git log -1", run: &daemon.ShellRun{Lines: []daemon.ShellLine{{Text: "abc123 first"}}}}})
	return m
}

// Every transcript block and the input share one left and one right edge, and text in each starts
// on one column, at 120 and 200 columns.
func TestBlocksShareOneGrid(t *testing.T) {
	for _, w := range []int{120, 200} {
		m := gridScene(w)
		raw := strings.Split(m.View().Content, "\n")
		plain := strings.Split(ansi.Strip(m.View().Content), "\n")
		limit := w
		if m.sideOn() {
			limit = m.sideX()
		}
		edges := map[string][2]int{}
		textCol := map[string]int{}
		for i, l := range plain {
			at := func(sub string) int {
				i := strings.Index(l, sub)
				if i < 0 {
					return -1
				}
				return ansi.StringWidth(l[:i])
			}
			name := ""
			switch {
			case strings.Contains(l, "read go.mod"):
				name, textCol["user"] = "user", at("read go.mod")
			case strings.Contains(l, "git log -1"):
				name, textCol["shell"] = "shell", at("git log -1")
			case strings.Contains(l, "Ask, or"):
				name, textCol["input"] = "input", at("Ask, or")
			case strings.Contains(l, "four Charm"):
				textCol["agent"] = at("It uses")
			case strings.Contains(l, "+ 1 read"):
				textCol["tool"] = at("+ 1 read")
			}
			if name != "" {
				lo, hi := bgSpan(raw[i], limit)
				edges[name] = [2]int{lo, hi}
			}
		}
		delete(edges, "user")
		_, want, _ := m.grid(m.tw())
		if len(edges) != 2 {
			t.Fatalf("%d cols: blocks found %v:\n%s", w, edges, strings.Join(plain, "\n"))
		}
		if edges["shell"] != edges["input"] || edges["shell"][0] != want-2 {
			t.Fatalf("%d cols: edges differ (lo, hi): %v", w, edges)
		}
		for k, c := range textCol {
			if c != want {
				t.Fatalf("%d cols: %s text at column %d, want %d (%v)", w, k, c, want, textCol)
			}
		}
		if m.sideOn() {
			if _, hi := bgSpan(raw[3], w); hi != w-m.pad()-1 {
				t.Fatalf("%d cols: sidebar ends at %d, want %d", w, hi, w-m.pad()-1)
			}
		}
		if m.sideOn() && edges["shell"][1] != m.sideX()-sideGap-1 {
			t.Fatalf("%d cols: blocks end at %d, the sidebar starts at %d", w, edges["shell"][1], m.sideX())
		}
	}
}
