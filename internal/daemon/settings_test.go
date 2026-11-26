//go:build harness

package daemon

import (
	"context"
	"slices"
	"strings"
	"testing"

	authstore "github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/config"
)

func settingsClient(t *testing.T) (*Server, *Client) {
	t.Helper()
	sv, url, _ := setupTest(t)
	return sv, &Client{Base: url, Token: testToken}
}

func TestSettingsListSetAndUnset(t *testing.T) {
	_, c := settingsClient(t)
	ctx := context.Background()
	if err := c.SetSetting(ctx, "linear.team", "eng"); err != nil {
		t.Fatal(err)
	}
	st, err := c.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(st.Settings, func(r SettingRow) bool { return r.Key == "linear.team" })
	if i < 0 || st.Settings[i].Value != "eng" || st.Settings[i].Src != "user" || st.Settings[i].Meaning == "" {
		t.Fatalf("linear.team row: %+v", st.Settings)
	}
	if err := c.SetSetting(ctx, "warden.max", "many"); err == nil {
		t.Fatal("an invalid value must be refused")
	}
	if err := c.SetSetting(ctx, "notify.telegram.token", "x"); err == nil {
		t.Fatal("a secret key never goes through settings")
	}
	if err := c.UnsetSetting(ctx, "notify.telegram.token"); err == nil {
		t.Fatal("a secret key is never removed through settings")
	}
	if err := c.UnsetSetting(ctx, "linear.team"); err != nil {
		t.Fatal(err)
	}
	if v := config.Get("linear.team", ""); v != "" {
		t.Fatalf("unset left %q", v)
	}
}

func TestSettingsAPIKeysMaskedAndRemovable(t *testing.T) {
	_, c := settingsClient(t)
	ctx := context.Background()
	key := strings.Repeat("k", 12) + "wxyz"
	if err := c.SetKey(ctx, "linear", key); err != nil {
		t.Fatal(err)
	}
	if authstore.Stored("linear") != key {
		t.Fatal("key not stored")
	}
	st, err := c.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(st.Keys, func(k KeyRow) bool { return k.Service == "linear" })
	if i < 0 || st.Keys[i].Masked != "****wxyz" || strings.Contains(st.Keys[i].Masked, "kkkk") {
		t.Fatalf("keys: %+v", st.Keys)
	}
	if err := c.SetKey(ctx, "Bad Name", key); err == nil {
		t.Fatal("bad service name must be refused")
	}
	if err := c.RemoveKey(ctx, "linear"); err != nil {
		t.Fatal(err)
	}
	if authstore.Stored("linear") != "" {
		t.Fatal("key not removed")
	}
}

func TestServeModeToggle(t *testing.T) {
	_, c := settingsClient(t)
	ctx := context.Background()
	if err := c.SetServe(ctx, false); err != nil {
		t.Fatal(err)
	}
	st, _ := c.Settings(ctx)
	if st.Serve || config.Get("auto.paused", "") != "true" {
		t.Fatalf("serve off: %+v paused=%q", st.Serve, config.Get("auto.paused", ""))
	}
	if err := c.SetServe(ctx, true); err != nil {
		t.Fatal(err)
	}
	if st, _ = c.Settings(ctx); !st.Serve {
		t.Fatal("serve on")
	}
}

func TestDoctorRunsThroughTheDaemon(t *testing.T) {
	sv, c := settingsClient(t)
	sv.Doctor = func(context.Context) []string { return []string{"ok   git found", "warn x"} }
	lines, err := c.Doctor(context.Background())
	if err != nil || len(lines) != 2 || lines[0] != "ok   git found" {
		t.Fatalf("doctor: %v %v", lines, err)
	}
}
