//go:build harness

package daemon

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestAskKeepsTheAskingSession(t *testing.T) {
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	seq := sv.lastSeqNow()
	if code, m := post(t, hs.URL+"/v1/asks", `{"id":"ask:k1","question":"Ok?","options":["yes","no"],"session":"s-1"}`); code != 200 {
		t.Fatalf("%d %v", code, m)
	}
	evs, _ := sv.st.Since(seq)
	if len(evs) != 1 || evs[0].Kind != KindAsked || evs[0].Session != "s-1" {
		t.Fatalf("the asked event carries its session: %+v", evs)
	}
	var d Decision
	json.Unmarshal(evs[0].Data, &d)
	if d.Session != "s-1" {
		t.Fatalf("decision %+v", d)
	}
	if p := sv.Pending(); len(p) != 1 || p[0].Session != "s-1" {
		t.Fatalf("pending %+v", p)
	}
}
