//go:build harness

package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/api"
)

func (f *fakeSettings) ConnectTest(context.Context) (api.ConnectTestResult, error) {
	f.tested++
	return api.ConnectTestResult{Results: []string{"slack notifications: sent", "slack chat: HTTP 403 not_in_channel"}}, nil
}

func TestChannelsPanelAndTestSend(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 48
	f := &fakeSettings{}
	m.cfg = f
	m = cpress(typeIn(m, "/channels"), ckey("enter"))
	out := screen(m)
	for _, want := range []string{"connect.slack.notify", "connect.slack.chat", "connect.telegram.chat", "connect.discord.notify", "/channels test"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/channels lacks %q:\n%s", want, out)
		}
	}
	m = newChatTest(0)
	m.width, m.height = 160, 48
	m.cfg = f
	m = cpress(typeIn(m, "/channels test"), ckey("enter"))
	out = screen(m) + notes(m)
	if f.tested != 1 || !strings.Contains(out, "slack notifications: sent") || !strings.Contains(out, "not_in_channel") {
		t.Fatalf("tested %d:\n%s", f.tested, out)
	}
}
