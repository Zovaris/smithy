package connect

import (
	"path/filepath"
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
	if k := Kinds("telegram"); !k["blocked"] || !k["decision"] {
		t.Fatalf("default kinds %v", k)
	}
	config.Set(cfg, "connect.telegram.kinds", "decision")
	if k := Kinds("telegram"); k["blocked"] || !k["decision"] {
		t.Fatalf("per-connector kinds %v", k)
	}
}
