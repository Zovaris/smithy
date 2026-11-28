package stats

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/tui/kit"

	"github.com/charmbracelet/colorprofile"
)

func TestSteelStaysTheDefaultTheme(t *testing.T) {
	base := kit.NewTheme(true, colorprofile.TrueColor, true)
	for _, n := range []string{"", "steel", "nope"} {
		if got := kit.NamedTheme(n, true, colorprofile.TrueColor, true); got.Accent != base.Accent || got.Name != "steel" {
			t.Errorf("%q: want steel's ember, got %v (%s)", n, got.Accent, got.Name)
		}
	}
}

func TestEachThemeHasItsOwnAccent(t *testing.T) {
	seen := map[any]string{}
	for _, n := range kit.Themes {
		th := kit.NamedTheme(n, true, colorprofile.TrueColor, true)
		if th.Name != n {
			t.Errorf("%s: name %q", n, th.Name)
		}
		if prev, ok := seen[th.Accent]; ok {
			t.Errorf("%s shares its accent with %s", n, prev)
		}
		seen[th.Accent] = n
		if again := th.Renamed(n); again.Accent != th.Accent {
			t.Errorf("%s: renamed keeps the accent", n)
		}
	}
	if len(kit.Themes) < 2 || kit.Themes[0] != "steel" {
		t.Fatalf("themes %v: steel first, at least one other", kit.Themes)
	}
	if p := kit.NamedTheme("frost", true, colorprofile.Ascii, true); p.Accent != nil {
		t.Errorf("no color stays plain under any theme")
	}
}

func TestDashboardFollowsUITheme(t *testing.T) {
	m := newModel(options{profile: colorprofile.TrueColor, dark: true, unicode: true, theme: "plum",
		now: func() time.Time { return time.Unix(0, 0) }, load: func(time.Time) Snapshot { return Snapshot{} }})
	if m.th.Name != "plum" {
		t.Fatalf("start: %s", m.th.Name)
	}
	next, _ := m.Update(systemMsg(System{Settings: []Setting{{Key: "ui.theme", Value: "frost"}}}))
	if got := next.(Model).th.Name; got != "frost" {
		t.Fatalf("after a settings load: %s", got)
	}
	next, _ = next.Update(tea.BackgroundColorMsg{})
	if got := next.(Model).th.Name; got != "frost" {
		t.Fatalf("a background change keeps the theme: %s", got)
	}
}

func TestThemesMatchTheConfigEnum(t *testing.T) {
	if _, opts := config.KindOf("ui.theme"); !slices.Equal(opts, kit.Themes) {
		t.Fatalf("config %v, tui %v", opts, kit.Themes)
	}
}

func TestEachThemeIsAFullPalette(t *testing.T) {
	if len(kit.Themes) < 7 {
		t.Fatalf("want steel plus at least six themes, got %v", kit.Themes)
	}
	steel := kit.NewTheme(true, colorprofile.TrueColor, true)
	for _, n := range kit.Themes[1:] {
		th := kit.NamedTheme(n, true, colorprofile.TrueColor, true)
		for name, pair := range map[string][2]any{
			"surface": {th.Surface, steel.Surface}, "raised": {th.Raised, steel.Raised}, "text": {th.Text, steel.Text},
			"muted": {th.Muted, steel.Muted}, "border": {th.Border, steel.Border}, "strong": {th.Strong, steel.Strong},
		} {
			if pair[0] == pair[1] {
				t.Errorf("%s: %s is steel's", n, name)
			}
		}
		for _, c := range []any{th.Ok, th.Warn, th.Err, th.Info, th.Focus, th.Overlay, th.Dim} {
			if c == nil {
				t.Errorf("%s: a signal or tier is missing", n)
			}
		}
		if len(th.Ramp) != len(steel.Ramp) || th.Ramp[0] == steel.Ramp[0] {
			t.Errorf("%s: dither ramp is steel's", n)
		}
		if light := kit.NamedTheme(n, false, colorprofile.TrueColor, true); light.Surface == th.Surface {
			t.Errorf("%s: light and dark share a surface", n)
		}
		if lo := kit.NamedTheme(n, true, colorprofile.ANSI256, true); lo.Surface == kit.NewTheme(true, colorprofile.ANSI256, true).Surface {
			t.Errorf("%s: 256 colors fall back to steel's tiers", n)
		}
		if lo := kit.NamedTheme(n, true, colorprofile.ANSI, true); lo.Surface != kit.NewTheme(true, colorprofile.ANSI, true).Surface {
			t.Errorf("%s: 16 colors keep steel's tiers", n)
		}
	}
}

func TestWebTemperHasEveryTheme(t *testing.T) {
	b, err := os.ReadFile("../../../web/src/theme/temper.css")
	if err != nil {
		t.Fatal(err)
	}
	css := strings.ToLower(string(b))
	for _, n := range kit.Themes[1:] {
		p := kit.Palettes[n]
		for _, want := range []string{
			`[data-accent="` + n + `"][data-mode="dark"]`,
			"--t-ember: " + strings.ToLower(p.Accent[0]), "--t-ember: " + strings.ToLower(p.Accent[1]),
			"--t-side: " + strings.ToLower(p.Surface[1]),
		} {
			if !strings.Contains(css, want) {
				t.Errorf("%s: temper.css lacks %q", n, want)
			}
		}
	}
}
