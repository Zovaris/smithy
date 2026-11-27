package tui

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/config"

	"github.com/charmbracelet/colorprofile"
)

func TestSteelStaysTheDefaultTheme(t *testing.T) {
	base := newTheme(true, colorprofile.TrueColor, true)
	for _, n := range []string{"", "steel", "nope"} {
		if got := namedTheme(n, true, colorprofile.TrueColor, true); got.accent != base.accent || got.name != "steel" {
			t.Errorf("%q: want steel's ember, got %v (%s)", n, got.accent, got.name)
		}
	}
}

func TestEachThemeHasItsOwnAccent(t *testing.T) {
	seen := map[any]string{}
	for _, n := range Themes {
		th := namedTheme(n, true, colorprofile.TrueColor, true)
		if th.name != n {
			t.Errorf("%s: name %q", n, th.name)
		}
		if prev, ok := seen[th.accent]; ok {
			t.Errorf("%s shares its accent with %s", n, prev)
		}
		seen[th.accent] = n
		if again := th.renamed(n); again.accent != th.accent {
			t.Errorf("%s: renamed keeps the accent", n)
		}
	}
	if len(Themes) < 2 || Themes[0] != "steel" {
		t.Fatalf("themes %v: steel first, at least one other", Themes)
	}
	if p := namedTheme("frost", true, colorprofile.Ascii, true); p.accent != nil {
		t.Errorf("no color stays plain under any theme")
	}
}

func TestDashboardFollowsUITheme(t *testing.T) {
	m := newModel(options{profile: colorprofile.TrueColor, dark: true, unicode: true, theme: "plum",
		now: func() time.Time { return time.Unix(0, 0) }, load: func(time.Time) Snapshot { return Snapshot{} }})
	if m.th.name != "plum" {
		t.Fatalf("start: %s", m.th.name)
	}
	next, _ := m.Update(systemMsg(System{Settings: []Setting{{Key: "ui.theme", Value: "frost"}}}))
	if got := next.(Model).th.name; got != "frost" {
		t.Fatalf("after a settings load: %s", got)
	}
	next, _ = next.Update(tea.BackgroundColorMsg{})
	if got := next.(Model).th.name; got != "frost" {
		t.Fatalf("a background change keeps the theme: %s", got)
	}
}

func TestThemesMatchTheConfigEnum(t *testing.T) {
	if _, opts := config.KindOf("ui.theme"); !slices.Equal(opts, Themes) {
		t.Fatalf("config %v, tui %v", opts, Themes)
	}
}

func TestEachThemeIsAFullPalette(t *testing.T) {
	if len(Themes) < 7 {
		t.Fatalf("want steel plus at least six themes, got %v", Themes)
	}
	steel := newTheme(true, colorprofile.TrueColor, true)
	for _, n := range Themes[1:] {
		th := namedTheme(n, true, colorprofile.TrueColor, true)
		for name, pair := range map[string][2]any{
			"surface": {th.surface, steel.surface}, "raised": {th.raised, steel.raised}, "text": {th.text, steel.text},
			"muted": {th.muted, steel.muted}, "border": {th.border, steel.border}, "strong": {th.strong, steel.strong},
		} {
			if pair[0] == pair[1] {
				t.Errorf("%s: %s is steel's", n, name)
			}
		}
		for _, c := range []any{th.ok, th.warn, th.err, th.info, th.focus, th.overlay, th.dim} {
			if c == nil {
				t.Errorf("%s: a signal or tier is missing", n)
			}
		}
		if len(th.ramp) != len(steel.ramp) || th.ramp[0] == steel.ramp[0] {
			t.Errorf("%s: dither ramp is steel's", n)
		}
		if light := namedTheme(n, false, colorprofile.TrueColor, true); light.surface == th.surface {
			t.Errorf("%s: light and dark share a surface", n)
		}
		if lo := namedTheme(n, true, colorprofile.ANSI256, true); lo.surface == newTheme(true, colorprofile.ANSI256, true).surface {
			t.Errorf("%s: 256 colors fall back to steel's tiers", n)
		}
		if lo := namedTheme(n, true, colorprofile.ANSI, true); lo.surface != newTheme(true, colorprofile.ANSI, true).surface {
			t.Errorf("%s: 16 colors keep steel's tiers", n)
		}
	}
}

func TestWebTemperHasEveryTheme(t *testing.T) {
	b, err := os.ReadFile("../../web/src/theme/temper.css")
	if err != nil {
		t.Fatal(err)
	}
	css := strings.ToLower(string(b))
	for _, n := range Themes[1:] {
		p := palettes[n]
		for _, want := range []string{
			`[data-accent="` + n + `"][data-mode="dark"]`,
			"--t-ember: " + strings.ToLower(p.accent[0]), "--t-ember: " + strings.ToLower(p.accent[1]),
			"--t-side: " + strings.ToLower(p.surface[1]),
		} {
			if !strings.Contains(css, want) {
				t.Errorf("%s: temper.css lacks %q", n, want)
			}
		}
	}
}
