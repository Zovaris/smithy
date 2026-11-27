//go:build harness

package daemon

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

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

// loadSettings finds the repo settings file once per call, not once per key (it runs git).
func TestLoadSettingsIsFast(t *testing.T) {
	settingsClient(t)
	start := time.Now()
	if _, err := loadSettings(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("loadSettings took %v, want under 500ms", d)
	}
}

// A browser sends a body-less DELETE with no Content-Type; the Origin checks still guard it.
func TestBodylessDeleteNeedsNoContentType(t *testing.T) {
	_, c := settingsClient(t)
	ctx := context.Background()
	hdr := func(kv ...string) map[string]string {
		h := map[string]string{"Authorization": "Bearer " + testToken}
		for i := 0; i < len(kv); i += 2 {
			h[kv[i]] = kv[i+1]
		}
		return h
	}
	for _, tc := range []struct {
		name string
		h    map[string]string
		want int
	}{
		{"no content type", hdr(), 200},
		{"json content type", hdr("Content-Type", "application/json"), 200},
		{"same origin", hdr("Origin", c.Base, "Sec-Fetch-Site", "same-origin"), 200},
		{"cross-site", hdr("Sec-Fetch-Site", "cross-site"), 403},
		{"foreign origin", hdr("Origin", "https://evil.example"), 403},
	} {
		if err := c.SetKey(ctx, "linear", strings.Repeat("k", 12)+"wxyz"); err != nil {
			t.Fatal(err)
		}
		if err := c.SetSetting(ctx, "linear.team", "eng"); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{"/v1/keys/linear", "/v1/settings/linear.team"} {
			if res := do(t, "DELETE", c.Base+p, "", tc.h); res.StatusCode != tc.want {
				t.Errorf("%s %s: %d, want %d", tc.name, p, res.StatusCode, tc.want)
			}
		}
	}
	if res := do(t, "DELETE", c.Base+"/v1/keys/linear", "x", hdr("Content-Type", "text/plain")); res.StatusCode != 415 {
		t.Errorf("DELETE with a text body: %d, want 415", res.StatusCode)
	}
}

func BenchmarkLoadSettings(b *testing.B) {
	for b.Loop() {
		if _, err := loadSettings(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestSettingsReportThePromptFile(t *testing.T) {
	_, c := settingsClient(t)
	st, err := c.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Prompt.Path != PromptStatus().Path || !strings.HasSuffix(st.Prompt.Path, "prompt.md") || st.Prompt.Active {
		t.Fatalf("prompt state: %+v", st.Prompt)
	}
}
