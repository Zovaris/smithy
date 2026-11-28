//go:build harness

package chat

import (
	"strings"
	"testing"
)

func TestSlashPopupFloatsOverTheTranscript(t *testing.T) {
	m := newChatTest(40)
	m.width, m.height = 100, 40
	before := strings.Split(screen(m), "\n")
	m = typeIn(m, "/")
	after := strings.Split(screen(m), "\n")
	if len(after) != len(before) {
		t.Fatalf("screen height changed")
	}
	y := rowOf(t, m, "message 30")
	if before[y] != after[y] {
		t.Fatalf("the transcript moved under the popup:\n%q\n%q", before[y], after[y])
	}
	n := 0
	for _, l := range after {
		if strings.Contains(l, "  /") {
			n++
		}
	}
	if n == 0 || n > popupMax {
		t.Fatalf("popup shows %d rows, want 1..%d:\n%s", n, popupMax, strings.Join(after, "\n"))
	}
}
