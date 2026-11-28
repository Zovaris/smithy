package connect

import (
	"path/filepath"
	"testing"

	"github.com/Zovaris/smithy/internal/config"
)

func TestNotifyAndChatKeys(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("SMITHY_CONFIG", cfg)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TELEGRAM_API_KEY", "123:"+"abcDEF")
	t.Setenv("SLACK_API_KEY", "xoxb-1-abc")
	t.Setenv("SLACK_APP_API_KEY", "xapp-1-abc")
	t.Setenv("DISCORD_API_KEY", dTok)
	config.Set(cfg, "notify.telegram.chat", "-100500")
	config.Set(cfg, "connect.telegram.chat", "-100600")
	config.Set(cfg, "connect.slack.channel", "C0123456")
	config.Set(cfg, "connect.slack.chat", "C0999999")
	config.Set(cfg, "connect.discord.notify", "500500500")
	config.Set(cfg, "connect.discord.chat", "600600600")
	cs, err := FromConfig()
	if err != nil || len(cs) != 3 {
		t.Fatalf("%v %v", cs, err)
	}
	if tg := cs[0].(*Telegram); tg.Chat != "-100500" || tg.ChatID != "-100600" {
		t.Fatalf("telegram legacy notify + chat: %+v", tg)
	}
	if s := cs[1].(*Slack); s.Channel != "C0123456" || s.ChatChan != "C0999999" {
		t.Fatalf("slack %+v", s)
	}
	if d := cs[2].(*Discord); d.Channel != "500500500" || d.ChatChan != "600600600" {
		t.Fatalf("discord %+v", d)
	}
	config.Set(cfg, "connect.telegram.notify", "-100700")
	config.Set(cfg, "connect.slack.notify", "C0777777")
	cs, _ = FromConfig()
	if cs[0].(*Telegram).Chat != "-100700" || cs[1].(*Slack).Channel != "C0777777" {
		t.Fatal("the new notify key wins over the old one")
	}
	config.Set(cfg, "connect.slack.chat", "general")
	if _, err := FromConfig(); err == nil {
		t.Fatal("a chat channel name is refused")
	}
	config.Set(cfg, "connect.slack.chat", "C0999999")
	config.Set(cfg, "connect.telegram.chat", "@x")
	if _, err := FromConfig(); err == nil {
		t.Fatal("a bad telegram chat id is refused")
	}
}

func TestChatOnlyConnectorStillNeedsANotifyChannel(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("SMITHY_CONFIG", cfg)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TELEGRAM_API_KEY", "")
	t.Setenv("DISCORD_API_KEY", dTok)
	config.Set(cfg, "connect.discord.chat", "600600600")
	cs, err := FromConfig()
	if err != nil || len(cs) != 1 || cs[0].(*Discord).Channel != "600600600" {
		t.Fatalf("chat only: notifications go to the chat channel too: %v %v", cs, err)
	}
}
