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

func TestAskMultiAndOther(t *testing.T) {
	_, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	c := &Client{Base: hs.URL, Token: testToken}
	a := &Asker{C: c}
	ctx := context.Background()
	if err := a.AskMulti(ctx, "ask:multi1", "Which checks?", []string{"lint", "test", "e2e"}, true); err != nil {
		t.Fatal(err)
	}
	d, err := c.Decision(ctx, "ask:multi1")
	if err != nil || !d.Multi || !d.Other {
		t.Fatalf("decision %+v %v", d, err)
	}
	if _, err := c.Decide(ctx, "ask:multi1", "lint, e2e"); err != nil {
		t.Fatal(err)
	}
	if err := a.Ask(ctx, "ask:other1", "Which db?", []string{"sqlite"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Decide(ctx, "ask:other1", "duckdb, in memory"); err != nil {
		t.Fatalf("free text refused: %v", err)
	}
	if err := a.Ask(ctx, "guard:g1", "Allow x?", []string{"approve", "deny"}); err != nil {
		t.Fatal(err)
	}
	if d, _ := c.Decision(ctx, "guard:g1"); d.Other {
		t.Fatal("a guard decision takes free text")
	}
	if _, err := c.Decide(ctx, "guard:g1", "maybe"); err == nil {
		t.Fatal("guard accepted free text")
	}
}
