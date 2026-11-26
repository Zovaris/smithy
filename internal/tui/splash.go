//go:build harness

package tui

import (
	"image/color"
	"math"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// splashLen is the entry animation's length in frames: 12 at 15 fps, 800 ms.
const splashLen = 12

// splashMinW and splashMinH are the smallest screen the splash plays on.
const (
	splashMinW = 80
	splashMinH = 24
)

// splashMsg advances the entry animation one frame.
type splashMsg struct{}

func splashTick() tea.Cmd {
	return tea.Tick(time.Second/fps, func(time.Time) tea.Msg { return splashMsg{} })
}

// splashWanted reports whether the entry animation may play: on a terminal with color and
// motion, and ui.splash not false.
func splashWanted(tty bool, noColor, reduceEnv, setting, reduceSetting string) bool {
	return tty && noColor == "" && reduceEnv == "" && setting != "false" && reduceSetting != "true"
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// startSplash precomputes the frames for the first screen size, once per chat; too small a
// screen skips it.
func (m *chatView) startSplash() tea.Cmd {
	if !m.splashOn {
		return nil
	}
	m.splashOn = false
	if m.width < splashMinW || m.height < splashMinH || m.th.plain || m.th.ramp == nil || !m.th.g.unicode {
		return nil
	}
	m.splash = splashFrames(m.th, m.width, m.height)
	return splashTick()
}

// anvilScene is the lit anvil s with its top left at pixel ax, ay (two pixels per text row) over the
// dither field at phase ph: fade scales the field, rise the anvil's light, sparks are sprite-local.
type anvilScene struct {
	s              sprite
	ax, ay         int
	ph, fade, rise float64
	sparks         []spark
}

// anvilField draws the scene as w×h half-block rows: steel shaded from the dither ramp up to the
// theme's text tones so the anvil reads lit in every theme, the field darkened around it, and
// ember sparks cooling into the ramp. The splash and the welcome both draw it.
func (t theme) anvilField(sc anvilScene, w, h int) []string {
	return t.anvilRows(sc, w, h, nil)
}

// anvilRows is anvilField with cell, when set, drawing over a cell given the pixel colors below it.
func (t theme) anvilRows(sc anvilScene, w, h int, cell func(x, row int, bot color.Color) (string, bool)) []string {
	s := sc.s
	steel := []color.Color{t.ramp[len(t.ramp)/2], t.ramp[len(t.ramp)-1], t.muted, t.text, t.strong}
	sp := map[[2]int]float64{}
	for _, p := range sc.sparks {
		sp[[2]int{int(p.x), int(p.y)}] = p.age / p.life
	}
	cx, cy := sc.ax+s.w()/2, sc.ay+s.h()/2
	px := func(x, y int) color.Color {
		lx, ly := x-sc.ax, y-sc.ay
		if age, ok := sp[[2]int{lx, ly}]; ok && !s.on(lx, ly) {
			return dpick([]color.Color{t.accent, t.ramp[len(t.ramp)-1]}, age, x, y)
		}
		if s.on(lx, ly) && sc.rise > 0 {
			return dpick(steel, s.shade(lx, ly)*sc.rise, x, y)
		}
		dx, dy := float64(x-cx)/float64(w), float64(y-cy)/float64(2*h)
		near := clamp01(math.Hypot(dx, dy) * 4)
		v := fieldAt(x, y/2, sc.ph) * 0.45 * sc.fade * (0.25 + 0.75*near)
		return dpick(t.ramp, clamp01(v), x, y)
	}
	rows := make([]string, h)
	for row := range rows {
		var b strings.Builder
		for x := 0; x < w; x++ {
			top, bot := px(x, row*2), px(x, row*2+1)
			if cell != nil {
				if c, ok := cell(x, row, bot); ok {
					b.WriteString(c)
					continue
				}
			}
			b.WriteString(lipgloss.NewStyle().Foreground(top).Background(bot).Render("▀"))
		}
		rows[row] = b.String()
	}
	return rows
}

// splashFrames is the entry animation: the welcome's steel field (fieldAt) fades in, the anvil rises out of it,
// one strike throws sparks and the name settles below. Each frame is a full screen.
func splashFrames(t theme, w, h int) []string {
	s := bigPlate
	ax, ay := (w-s.w())/2, max(0, h-s.h()/2-len(wordB)-2)/2*2-s.faceY
	if ay < 0 {
		ay = 0
	}
	nameY := (ay+s.h())/2 + 1
	nameW := len([]rune(wordB[0]))
	nx := (w - nameW) / 2
	f := newFX(7)
	out := make([]string, splashLen)
	for k := range out {
		if k == 5 {
			f.strike(s, 14, nameW)
		}
		sc := anvilScene{s: s, ax: ax, ay: ay, ph: float64(k) / 12, sparks: f.sparks,
			fade: min(1, float64(k+1)/5), rise: min(1, math.Max(0, float64(k-1)/4))}
		name := func(x, row int, bot color.Color) (string, bool) {
			if k < 7 || row < nameY || row >= nameY+len(wordB) || x < nx || x >= nx+nameW {
				return "", false
			}
			r := []rune(wordB[row-nameY])[x-nx]
			return lipgloss.NewStyle().Foreground(t.strong).Background(bot).Bold(true).Render(string(r)), true
		}
		out[k] = strings.Join(t.anvilRows(sc, w, h, name), "\n")
		f.step()
	}
	return out
}
