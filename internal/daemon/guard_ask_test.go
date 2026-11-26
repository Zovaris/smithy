//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Zovaris/smithy/internal/guard"
)

func TestGuardAskRoundTrip(t *testing.T) {
	_, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	c := &Client{Base: hs.URL, Token: testToken}
	a := &Asker{C: c}
	ctx := context.Background()
	if err := a.Ask(ctx, "guard:ab12cd34", "Run this command? rm -rf /", []string{"run once", "always allow this pattern", "deny"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"guard:", "guard:a/b", "guard:../x", "other:x"} {
		if err := a.Ask(ctx, bad, "q", nil); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, ok, _ := a.Outcome(ctx, "guard:ab12cd34"); ok {
		t.Fatal("answered before anyone answered")
	}
	if _, err := c.Decide(ctx, "guard:ab12cd34", "deny"); err != nil {
		t.Fatal(err)
	}
	if ans, ok, err := a.Outcome(ctx, "guard:ab12cd34"); !ok || ans != "deny" || err != nil {
		t.Fatalf("%q %v %v", ans, ok, err)
	}
}

func TestGuardSettingsAPI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	_, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	res := do(t, "GET", hs.URL+"/v1/guard", "", auth)
	var v struct {
		Enabled   bool `json:"enabled"`
		Suggested []struct {
			Host  string `json:"host"`
			Added bool   `json:"added"`
		} `json:"suggested"`
	}
	json.NewDecoder(res.Body).Decode(&v)
	res.Body.Close()
	if res.StatusCode != 200 || !v.Enabled || len(v.Suggested) == 0 || v.Suggested[0].Added {
		t.Fatalf("%d %+v", res.StatusCode, v)
	}
	res = do(t, "POST", hs.URL+"/v1/guard/suggested", "{}", auth)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("add: %d", res.StatusCode)
	}
	if f, _ := guard.LoadFetch(); len(f.Allow) != len(guard.Suggested) {
		t.Fatalf("allow %v", f.Allow)
	}
}

func TestChatAskRoundTrip(t *testing.T) {
	_, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	c := &Client{Base: hs.URL, Token: testToken}
	a := &Asker{C: c}
	ctx := context.Background()
	if err := a.Ask(ctx, "ask:ab12cd34", "Which db?", []string{"sqlite", "postgres"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"ask:", "ask:a/b", "ask:../x"} {
		if err := a.Ask(ctx, bad, "q", nil); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := c.Decide(ctx, "ask:ab12cd34", "sqlite"); err != nil {
		t.Fatal(err)
	}
	if ans, ok, err := a.Outcome(ctx, "ask:ab12cd34"); !ok || ans != "sqlite" || err != nil {
		t.Fatalf("%q %v %v", ans, ok, err)
	}
}
