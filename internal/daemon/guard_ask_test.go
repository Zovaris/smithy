//go:build harness

package daemon

import (
	"context"
	"path/filepath"
	"testing"
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
