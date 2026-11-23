package text

import "testing"

func TestCleanDropsEscapesAndControls(t *testing.T) {
	for in, want := range map[string]string{
		"a\x1b[31mred\x1b[0m b":                    "ared b",
		"x\x1b]8;;http://evil\x07link\x1b]8;;\x07": "xlink",
		"one\ntwo three\r\tfour":                   "one two three four",
		"bell\x07 \u009b31m c1":                    "bell 31m c1",
	} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripEscapesKeepsTab(t *testing.T) {
	if got := StripEscapes("a\tb\x1b[1m", true); got != "a\tb" {
		t.Fatalf("%q", got)
	}
}
