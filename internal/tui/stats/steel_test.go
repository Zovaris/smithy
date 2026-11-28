package stats

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/Zovaris/smithy/internal/tui/kit"
)

func hex(c interface{ RGBA() (r, g, b, a uint32) }) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02X%02X%02X", r>>8, g>>8, b>>8)
}

func TestSteelTokens(t *testing.T) {
	th := kit.NewTheme(true, colorprofile.TrueColor, true)
	for name, want := range map[string]string{
		"ember": "#F2762E", "ok": "#4CC38A", "wait": "#E3B341", "err": "#F47067", "info": "#6CB6FF",
		"dim": "#7A879A", "text": "#B4C0CE", "strong": "#E6EDF5", "muted": "#8593A6",
		"base": "#0E1116", "raised": "#131820", "focus": "#1A212B", "overlay": "#222B37", "border": "#2C3746",
	} {
		got := map[string]string{"ember": hex(th.Accent), "ok": hex(th.Ok), "wait": hex(th.Warn), "err": hex(th.Err), "info": hex(th.Info),
			"dim": hex(th.Dim), "text": hex(th.Text), "strong": hex(th.Strong), "muted": hex(th.Muted),
			"base": hex(th.Surface), "raised": hex(th.Raised), "focus": hex(th.Focus), "overlay": hex(th.Overlay), "border": hex(th.Border)}[name]
		if got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
	if len(th.Ramp) != 6 || hex(th.Ramp[0]) != "#0E1116" || hex(th.Ramp[5]) != "#3B4A5E" {
		t.Fatal("steel ramp is not the TK-28 one")
	}
	if kit.UniGlyphs.Wait != "?" || kit.AsciiGlyphs.Wait != "?" {
		t.Fatal("waits-on-you is not ?")
	}
	if kit.Fps != 15 {
		t.Fatalf("fps %d, want 15", kit.Fps)
	}
}

func TestDitherBar(t *testing.T) {
	th := kit.NewTheme(true, colorprofile.TrueColor, true)
	for _, c := range []struct {
		frac float64
		want string
	}{{0, "░░░░░░░░░░"}, {0.5, "█████▓▒░░░"}, {1, "██████████"}} {
		bar := th.DitherBar(10, c.frac)
		if got := ansi.Strip(bar); got != c.want {
			t.Errorf("%.1f: %q, want %q", c.frac, got, c.want)
		}
	}
	if !strings.Contains(th.DitherBar(10, 0.5), "38;2;242;118;46") {
		t.Fatal("the bar's front is not ember")
	}
	plain := kit.NewTheme(true, colorprofile.Ascii, false)
	if got := plain.DitherBar(10, 0.5); got != "#####=-..." {
		t.Fatalf("ascii bar %q", got)
	}
}

func TestShimmer(t *testing.T) {
	th := kit.NewTheme(true, colorprofile.TrueColor, true)
	s := th.Shimmer("running item title here", 3)
	if ansi.Strip(s) != "running item title here" {
		t.Fatalf("shimmer changed the text: %q", ansi.Strip(s))
	}
	if !strings.Contains(s, "38;2;230;237;245") || !strings.Contains(s, "38;2;122;135;154") {
		t.Fatal("shimmer lacks strong in the band or dim outside it")
	}
	if th.Shimmer("abc", 3) == th.Shimmer("abc", 40) {
		t.Fatal("band position does not move the shimmer")
	}
}

func TestAnvilHeader(t *testing.T) {
	th := kit.NewTheme(true, colorprofile.TrueColor, true)
	rows := th.Plate(kit.BigPlate, nil)
	if len(rows) != 7 {
		t.Fatalf("%d rows", len(rows))
	}
	for i, r := range rows {
		if ansi.StringWidth(r) != 30 {
			t.Fatalf("row %d width %d: %q", i, ansi.StringWidth(r), ansi.Strip(r))
		}
	}
	if strings.TrimSpace(ansi.Strip(rows[0])) != "" {
		t.Fatalf("headroom above the anvil is not empty: %q", ansi.Strip(rows[0]))
	}
	if !strings.Contains(ansi.Strip(rows[2]), "▀") && !strings.Contains(ansi.Strip(rows[2]), "▄") && !strings.Contains(ansi.Strip(rows[2]), "█") {
		t.Fatalf("no anvil face on row 2: %q", ansi.Strip(rows[2]))
	}
	if !strings.HasPrefix(ansi.Strip(rows[6]), "       ") {
		t.Fatalf("anvil feet missing their left margin: %q", ansi.Strip(rows[6]))
	}
	spark := th.Plate(kit.BigPlate, []kit.Spark{{X: 16, Y: 1, Life: 1}})
	if strings.Join(spark, "") == strings.Join(rows, "") || !strings.Contains(strings.Join(spark, ""), "242;118;46") {
		t.Fatal("no ember spark above the anvil")
	}
	if got := th.LoadingField(8, 0); ansi.StringWidth(got) != 8 {
		t.Fatalf("loading field %q", got)
	}
}

func TestBordersOnlyOnFocus(t *testing.T) {
	m := demo(t, 100, 30)
	r := rect{0, 0, 30, 5}
	if got := ansi.Strip(m.box("T", "", false, nil, r, nil)); strings.ContainsAny(got, "╭╮╰╯│─") {
		t.Fatalf("unfocused box has a border:\n%s", got)
	}
	if got := ansi.Strip(m.box("T", "", true, nil, r, nil)); !strings.Contains(got, "┌") {
		t.Fatalf("focused box has no border:\n%s", got)
	}
}

func TestReduceMotion(t *testing.T) {
	for _, c := range []struct {
		noAnim       bool
		env, setting string
		p            colorprofile.Profile
		want         bool
	}{{false, "", "", colorprofile.TrueColor, true}, {true, "", "", colorprofile.TrueColor, false},
		{false, "1", "", colorprofile.TrueColor, false}, {false, "", "true", colorprofile.TrueColor, false},
		{false, "", "false", colorprofile.TrueColor, true}, {false, "", "", colorprofile.Ascii, false}} {
		if got := motionOn(c.noAnim, c.env, c.setting, c.p); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

func TestMotionWhileRunning(t *testing.T) {
	m := demo(t, 160, 48)
	m.motion = true
	if !m.running() {
		t.Fatal("demo has no running item")
	}
	if !m.busy() {
		t.Fatal("running items do not keep frames going")
	}
	m.snap.Batches = nil
	if m.busy() {
		t.Fatal("frames run with nothing moving")
	}
}

func TestGoldenProfiles(t *testing.T) {
	for _, p := range []struct {
		name string
		p    colorprofile.Profile
	}{{"16", colorprofile.ANSI}, {"256", colorprofile.ANSI256}, {"truecolor", colorprofile.TrueColor}} {
		m := demo(t, 100, 30)
		m.profile = p.p
		m.th = kit.NewTheme(true, p.p, true)
		s := demoSystem()
		m.sys = &s
		got := m.View().Content
		if p.p == colorprofile.ANSI && (strings.Contains(got, "38;2;") || strings.Contains(got, "38;5;")) {
			t.Fatal("16-color output carries 256 or true colors")
		}
		file := filepath.Join("testdata", "steel-overview-"+p.name+".golden")
		if *update {
			write(t, file, got)
			continue
		}
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s differs (go test ./internal/tui/stats -update)", file)
		}
	}
}

func TestSteelRampStaysGreyIn256Colors(t *testing.T) {
	th := kit.NewTheme(true, colorprofile.ANSI256, true)
	for i, c := range th.Ramp {
		ic, ok := c.(ansi.IndexedColor)
		if !ok || ic < 232 {
			t.Fatalf("ramp[%d] = %#v, want a 256-color grey (232-255)", i, c)
		}
	}
}

func TestDashboardBordersAreSquareAndTitlesBreathe(t *testing.T) {
	for v := view(0); v < nViews; v++ {
		m := demo(t, 160, 48)
		m.view = v
		out := ansi.Strip(m.render())
		if strings.ContainsAny(out, "╭╮╰╯") {
			t.Errorf("%s has rounded corners", viewNames[v])
		}
		lines := strings.Split(out, "\n")
		_, tabY, body, _ := m.frame()
		if body.y < tabY+2 || strings.TrimSpace(lines[tabY+1]) != "" {
			t.Errorf("%s: no blank row under the tab bar", viewNames[v])
		}
	}
	m := demo(t, 160, 48)
	got := strings.Split(ansi.Strip(m.box("T", "", true, nil, rect{0, 0, 20, 6}, []string{"x"})), "\n")
	if strings.Trim(got[1], "│ ") != "" || !strings.Contains(got[2], "x") {
		t.Fatalf("no blank row under a box title: %q", got)
	}
	for _, s := range []string{m.helpBox(120, 40), m.textBox()} {
		if strings.ContainsAny(ansi.Strip(s), "╭╮╰╯") {
			t.Fatal("an overlay has rounded corners")
		}
	}
}
