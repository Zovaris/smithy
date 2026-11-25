package connect

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/config"
)

func TestFromConfig(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("SMITHY_CONFIG", cfg)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TELEGRAM_API_KEY", "")
	if cs, err := FromConfig(); len(cs) != 0 || err != nil {
		t.Fatalf("not set up is not an error: %v %v", cs, err)
	}
	config.Set(cfg, "notify.telegram.chat", "-100500")
	if cs, err := FromConfig(); len(cs) != 0 || err == nil {
		t.Fatalf("a chat without a token: %v %v", cs, err)
	}
	t.Setenv("TELEGRAM_API_KEY", "123:"+"abcDEF")
	config.Set(cfg, "notify.telegram.chat", "@chan_name")
	if cs, err := FromConfig(); len(cs) != 0 || err == nil {
		t.Fatalf("a @channel has no topics or replies: %v %v", cs, err)
	}
	config.Set(cfg, "notify.telegram.chat", "-100500")
	config.Set(cfg, "connect.telegram.users", "5, 6")
	cs, err := FromConfig()
	if err != nil || len(cs) != 1 {
		t.Fatalf("%v %v", cs, err)
	}
	tg := cs[0].(*Telegram)
	if tg.Chat != "-100500" || !tg.Users["6"] || len(tg.Users) != 2 {
		t.Fatalf("%+v", tg)
	}
	if k := Kinds("telegram"); !k["blocked"] || !k["decision"] || !k["pr_merged"] || !k["ci_red"] || k["report"] {
		t.Fatalf("default kinds %v", k)
	}
	config.Set(cfg, "notify.kinds", "blocked")
	if k := Kinds("telegram"); !k["blocked"] || k["pr_merged"] {
		t.Fatalf("a set notify.kinds wins over the defaults: %v", k)
	}
	config.Set(cfg, "connect.telegram.kinds", "decision")
	if k := Kinds("telegram"); k["blocked"] || !k["decision"] {
		t.Fatalf("per-connector kinds %v", k)
	}
}

func TestSlackFromConfig(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("SMITHY_CONFIG", cfg)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TELEGRAM_API_KEY", "")
	t.Setenv("SLACK_API_KEY", "")
	t.Setenv("SLACK_APP_API_KEY", "")
	config.Set(cfg, "connect.slack.channel", "#general")
	if cs, err := FromConfig(); len(cs) != 0 || err == nil {
		t.Fatalf("a channel name: %v %v", cs, err)
	}
	config.Set(cfg, "connect.slack.channel", "C0123456")
	if cs, err := FromConfig(); len(cs) != 0 || err == nil {
		t.Fatalf("no tokens: %v %v", cs, err)
	}
	t.Setenv("SLACK_API_KEY", "xoxb-1-abc")
	if cs, err := FromConfig(); len(cs) != 0 || err == nil || !strings.Contains(err.Error(), "slack_app") {
		t.Fatalf("no app token: %v %v", cs, err)
	}
	t.Setenv("SLACK_APP_API_KEY", "xoxb-1-abc")
	if cs, err := FromConfig(); len(cs) != 0 || err == nil {
		t.Fatalf("a bot token as app token: %v %v", cs, err)
	}
	t.Setenv("SLACK_APP_API_KEY", "xapp-1-abc")
	config.Set(cfg, "connect.slack.users", "U1, U2")
	cs, err := FromConfig()
	if err != nil || len(cs) != 1 {
		t.Fatalf("%v %v", cs, err)
	}
	if s := cs[0].(*Slack); s.Channel != "C0123456" || !s.Users["U2"] || s.AppToken != "xapp-1-abc" {
		t.Fatalf("%+v", s)
	}
}
