//go:build harness

package chat

import (
	"strings"
	"testing"
)

func TestModelSpaceCompletesTheAgentsModels(t *testing.T) {
	_, m := noSessionScene()
	m = typeIn(m, "/model")
	n, cmd := m.Update(ckey(" "))
	m = run(n.(chatView), cmd)
	var got []string
	for _, it := range m.popup() {
		got = append(got, it.fill)
	}
	if !strings.Contains(strings.Join(got, ","), "/model opencode/beam-free") {
		t.Fatalf("models %v", got)
	}
	m.input.SetValue("/model beam")
	if p := m.popup(); len(p) != 1 || p[0].fill != "/model opencode/beam-free" {
		t.Fatalf("filtered %v", p)
	}
}
