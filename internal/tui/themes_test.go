package tui

import (
	"slices"
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
