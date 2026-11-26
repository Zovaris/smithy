package text

import (
	"testing"
	"time"
)

func TestFormats(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{Plural(1, "run", "runs"), "1 run"},
		{Plural(3, "item waits", "items wait"), "3 items wait"},
		{Ago(-time.Second), "0s ago"},
		{Ago(90 * time.Second), "1m ago"},
		{Ago(47 * time.Hour), "47h ago"},
		{Ago(72 * time.Hour), "3d ago"},
		{Bytes(512), "512B"},
		{Bytes(1536), "1.5KB"},
		{Bytes(3 << 30), "3.0GB"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
