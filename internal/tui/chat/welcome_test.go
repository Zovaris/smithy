//go:build harness

package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/tui/kit"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

type welcomeSettings struct{ fakeSettings }

func (welcomeSettings) Settings(context.Context) (api.Settings, error) {
	return api.Settings{Serve: true,
		Settings: []api.SettingRow{
			{Key: "notify.telegram.chat", Src: "unset", Meaning: "Telegram chat id serve notifies"},
			{Key: "notify.kinds", Value: "pr_ready,blocked", Src: "default"},
			{Key: "warden.max", Value: "4"},
		},
		Keys: []api.KeyRow{{Service: "linear", Masked: "****wxyz"}}}, nil
}

func welcomeScene(f *fakeSetup) chatView {
	m := newChatScene()
	m.pending = nil
	m.setup, m.cfg = f, &welcomeSettings{}
	return run(m, m.loadSetup(true))
}

func TestFirstRunOpensTheFullScreenWelcome(t *testing.T) {
	m := welcomeScene(firstRunSetup())
	if m.overlay != overlayWelcome || !m.animating() {
		t.Fatalf("first run opens the animated welcome: overlay %v, animating %v", m.overlay, m.animating())
	}
	out := screen(m)
	for _, want := range []string{"Welcome to smithy", "agents", "model", "API keys", "notifications", "enter", "esc"} {
		if !strings.Contains(out, want) {
			t.Fatalf("welcome lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Add usage-limit detection") {
		t.Fatalf("the welcome covers the whole screen, not the chat:\n%s", out)
	}
}

func TestWelcomeWalksAgentsModelKeysNotifications(t *testing.T) {
	f := firstRunSetup()
	m := welcomeScene(f)
	m = sendKey(m, ckey("enter"))
	if m.overlay != overlaySetup || !strings.Contains(screen(m), "1/4") || !strings.Contains(screen(m), "/opt/homebrew/bin/claude") {
		t.Fatalf("step 1 lists the agents found:\n%s", screen(m))
	}
	m = sendKey(m, ckey("enter"))
	if len(f.set) != 1 || f.set[0] != "claude|" {
		t.Fatalf("enter on claude picks it: %v", f.set)
	}
	if m.setupStep != stepModel || !strings.Contains(screen(m), "2/4") || !strings.Contains(screen(m), "Model for claude") {
		t.Fatalf("step 2 is the model for the picked agent:\n%s", screen(m))
	}
	m = sendKey(m, ckey("esc"))
	if m.overlay != overlayWelcome || !strings.Contains(screen(m), "3/4") || !strings.Contains(screen(m), "smithy auth set openai") || !strings.Contains(screen(m), "****wxyz") {
		t.Fatalf("esc skips the model; step 3 shows the keys and how to add one:\n%s", screen(m))
	}
	m = sendKey(m, ckey("enter"))
	if !strings.Contains(screen(m), "4/4") || !strings.Contains(screen(m), "notify.telegram.chat") || strings.Contains(screen(m), "warden.max") {
		t.Fatalf("step 4 shows only the notification settings:\n%s", screen(m))
	}
	m = sendKey(m, ckey("enter"))
	if m.overlay != overlayNone || m.welcome != welcomeOff || !strings.Contains(screen(m), "/welcome") {
		t.Fatalf("the last step closes the welcome and says how to reopen it: %v\n%s", m.overlay, screen(m))
	}
}

func TestWelcomeEscIsLater(t *testing.T) {
	m := sendKey(welcomeScene(firstRunSetup()), ckey("esc"))
	if m.overlay != overlayNone || m.welcome != welcomeOff {
		t.Fatalf("esc on the welcome closes it: %v", m.overlay)
	}
	m = sendKey(typeIn(m, "/welcome"), ckey("enter"))
	if m.overlay != overlayWelcome {
		t.Fatalf("/welcome reopens it: %v", m.overlay)
	}
}

func TestTypedCommandOnFirstRunGoesToTheChat(t *testing.T) {
	f := firstRunSetup()
	m := welcomeScene(f)
	m = sendKey(typeIn(m, "/set linear.team ENG"), ckey("enter"))
	if len(f.set) != 0 {
		t.Fatalf("typed text never picks an agent or a model: %v", f.set)
	}
	s := m.cfg.(*welcomeSettings)
	if len(s.set) != 1 || s.set[0] != [2]string{"linear.team", "ENG"} {
		t.Fatalf("the / command runs in the chat: %v", s.set)
	}
}

func TestFirstRunWaitsWhileTheUserTypes(t *testing.T) {
	m := newChatScene()
	m.pending = nil
	m.setup, m.cfg = firstRunSetup(), &welcomeSettings{}
	m = typeIn(m, "/set")
	m = run(m, m.loadSetup(true))
	if m.overlay != overlayNone || m.input.Value() != "/set" {
		t.Fatalf("setup loading mid-typing does not take the keys: %v %q", m.overlay, m.input.Value())
	}
}

func TestWelcomeFlagOpensIt(t *testing.T) {
	a, err := parseChatArgs([]string{"--welcome"})
	if err != nil || !a.welcome {
		t.Fatalf("--welcome: %+v %v", a, err)
	}
	f := firstRunSetup()
	f.s.Chosen = true
	m := newChatScene()
	m.pending, m.setup, m.welcome = nil, f, welcomeSplash
	m = run(m, m.loadSetup(true))
	if m.overlay != overlayWelcome {
		t.Fatalf("a chat opened with --welcome shows it even when set up: %v", m.overlay)
	}
}

func litWelcome(name string) chatView {
	m := newChatView(nil, "http://127.0.0.1:7477", kit.NamedTheme(name, true, colorprofile.TrueColor, true))
	m.width, m.height = 120, 40
	m.openWelcome()
	return m
}

func rgb(c interface{ RGBA() (r, g, b, a uint32) }) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%d;%d;%d", r>>8, g>>8, b>>8)
}

func TestWelcomeShowsTheLitAnvilInEveryTheme(t *testing.T) {
	for _, name := range kit.Themes {
		m := litWelcome(name)
		lit := false
		for _, r := range m.welcomeView(m.width, m.height) {
			if strings.Trim(ansi.Strip(r), "▀ ") == "" && strings.Contains(r, rgb(m.th.Strong)) {
				lit = true
			}
		}
		if !lit {
			t.Fatalf("%s: no lit anvil on the welcome's field", name)
		}
	}
}

func TestSplashAndWelcomeShareTheAnvil(t *testing.T) {
	th := kit.NamedTheme("frost", true, colorprofile.TrueColor, true)
	a := anvilField(th, anvilScene{s: kit.BigPlate, rise: 1, fade: 1}, 60, 20)
	b := anvilField(th, anvilScene{s: kit.BigPlate, rise: 0, fade: 1}, 60, 20)
	if strings.Join(a, "\n") == strings.Join(b, "\n") || !strings.Contains(strings.Join(a, ""), rgb(th.Strong)) {
		t.Fatal("the shared renderer does not draw a lit anvil")
	}
}

func TestWelcomeAnvilHoldsStillUnderReducedMotion(t *testing.T) {
	m := litWelcome("steel")
	m.still = true
	a := strings.Join(m.welcomeView(m.width, m.height), "\n")
	m.frame = 37
	if b := strings.Join(m.welcomeView(m.width, m.height), "\n"); a != b {
		t.Fatal("reduced motion: the welcome still moves")
	}
	m.still = false
	if b := strings.Join(m.welcomeView(m.width, m.height), "\n"); a == b {
		t.Fatal("with motion the welcome field should drift")
	}
}
