//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"testing"
)

func TestListSessionsByDir(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	_, u, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) { return "ok", nil })
	for _, d := range []string{a, b} {
		if code, m := post(t, u+"/v1/sessions", `{"agent":"claude","dir":"`+d+`"}`); code != 201 && code != 200 {
			t.Fatalf("start: %d %v", code, m)
		}
	}
	real, _ := filepath.EvalSymlinks(a)
	res := do(t, "GET", u+"/v1/sessions?dir="+url.QueryEscape(a), "", auth)
	var l SessionList
	json.NewDecoder(res.Body).Decode(&l)
	if len(l.Sessions) != 1 || !InProject(l.Sessions[0].Dir, real) {
		t.Errorf("dir filter: %+v", l.Sessions)
	}
}
