package proof

import (
	"strings"
	"testing"
)

func TestSecretFindsChatBotTokens(t *testing.T) {
	for _, s := range []string{
		"use 123456789:" + strings.Repeat("A", 35) + " as the bot",
		"xapp-1-" + strings.Repeat("B", 20),
		"xoxb-12345-" + strings.Repeat("c", 12),
		strings.Repeat("M", 24) + "." + "GaBcDe" + "." + strings.Repeat("z", 27),
	} {
		if Secret(s) == "" {
			t.Errorf("not taken for a secret: %q", s)
		}
		if r := Redact(s); r == s {
			t.Errorf("not redacted: %q", s)
		}
	}
	if r := Secret("Add an export button to the board | done when CSV downloads"); r != "" {
		t.Fatalf("plain text taken for %s", r)
	}
}
