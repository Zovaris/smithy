package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func hex(c interface{ RGBA() (r, g, b, a uint32) }) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02X%02X%02X", r>>8, g>>8, b>>8)
}

func TestSteelTokens(t *testing.T) {
	th := newTheme(true, colorprofile.TrueColor, true)
	for name, want := range map[string]string{
		"ember": "#F2762E", "ok": "#4CC38A", "wait": "#E3B341", "err": "#F47067", "info": "#6CB6FF",
		"dim": "#7A879A", "text": "#B4C0CE", "strong": "#E6EDF5", "muted": "#8593A6",
		"base": "#0E1116", "raised": "#131820", "focus": "#1A212B", "overlay": "#222B37", "border": "#2C3746",
	} {
		got := map[string]string{"ember": hex(th.accent), "ok": hex(th.ok), "wait": hex(th.warn), "err": hex(th.err), "info": hex(th.info),
			"dim": hex(th.dim), "text": hex(th.text), "strong": hex(th.strong), "muted": hex(th.muted),
			"base": hex(th.surface), "raised": hex(th.raised), "focus": hex(th.focus), "overlay": hex(th.overlay), "border": hex(th.border)}[name]
		if got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
	if len(th.ramp) != 6 || hex(th.ramp[0]) != "#0E1116" || hex(th.ramp[5]) != "#3B4A5E" {
		t.Fatal("steel ramp is not the TK-28 one")
	}
	if uniGlyphs.wait != "?" || asciiGlyphs.wait != "?" {
		t.Fatal("waits-on-you is not ?")
	}
	if fps != 15 {
		t.Fatalf("fps %d, want 15", fps)
	}
}

func TestDitherBar(t *testing.T) {
	th := newTheme(true, colorprofile.TrueColor, true)
	for _, c := range []struct {
		frac float64
		want string
	}{{0, "░░░░░░░░░░"}, {0.5, "█████▓▒░░░"}, {1, "██████████"}} {
		bar := th.ditherBar(10, c.frac)
		if got := ansi.Strip(bar); got != c.want {
			t.Errorf("%.1f: %q, want %q", c.frac, got, c.want)
		}
	}
	if !strings.Contains(th.ditherBar(10, 0.5), "38;2;242;118;46") {
		t.Fatal("the bar's front is not ember")
	}
	plain := newTheme(true, colorprofile.Ascii, false)
	if got := plain.ditherBar(10, 0.5); got != "#####=-..." {
		t.Fatalf("ascii bar %q", got)
	}
}

func TestShimmer(t *testing.T) {
	th := newTheme(true, colorprofile.TrueColor, true)
	s := th.shimmer("running item title here", 3)
	if ansi.Strip(s) != "running item title here" {
		t.Fatalf("shimmer changed the text: %q", ansi.Strip(s))
	}
	if !strings.Contains(s, "38;2;230;237;245") || !strings.Contains(s, "38;2;122;135;154") {
		t.Fatal("shimmer lacks strong in the band or dim outside it")
	}
	if th.shimmer("abc", 3) == th.shimmer("abc", 40) {
		t.Fatal("band position does not move the shimmer")
	}
}

func TestGradientHeader(t *testing.T) {
	th := newTheme(true, colorprofile.TrueColor, true)
	rows := th.plate(30, 7, nil)
	if len(rows) != 7 {
		t.Fatalf("%d rows", len(rows))
	}
	for i, r := range rows {
		if ansi.StringWidth(r) != 30 || !strings.Contains(r, "▀") {
			t.Fatalf("row %d: %q", i, ansi.Strip(r))
		}
	}
	spark := th.plate(30, 7, []spark{{x: 10, y: 5, life: 1}})
	if strings.Join(spark, "") == strings.Join(rows, "") || !strings.Contains(strings.Join(spark, ""), "242;118;46") {
		t.Fatal("no ember spark on the plate")
	}
	if got := th.loadingField(8, 0); ansi.StringWidth(got) != 8 {
		t.Fatalf("loading field %q", got)
	}
}

func TestBordersOnlyOnFocus(t *testing.T) {
	m := demo(t, 100, 30)
	r := rect{0, 0, 30, 5}
	if got := ansi.Strip(m.box("T", "", false, nil, r, nil)); strings.ContainsAny(got, "╭╮╰╯│─") {
		t.Fatalf("unfocused box has a border:\n%s", got)
	}
	if got := ansi.Strip(m.box("T", "", true, nil, r, nil)); !strings.Contains(got, "╭") {
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
	m.fx = newFX(1)
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
		m.th = newTheme(true, p.p, true)
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
			t.Errorf("%s differs (go test ./internal/tui -update)", file)
		}
	}
}
