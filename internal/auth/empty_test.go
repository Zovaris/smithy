package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmptyFileHoldsNoSecretsAndDoesNotBlockSet(t *testing.T) {
	h := home(t)
	if err := os.MkdirAll(h, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(File(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	bak := filepath.Join(h, "auth.json.bak")
	if err := os.WriteFile(bak, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if l, err := List(); err != nil || len(l) != 0 {
		t.Fatalf("List on an empty file = %q %v", l, err)
	}
	if err := Set("linear", "lin_api_secretabcd"); err != nil {
		t.Fatalf("Set over an empty file: %v", err)
	}
	if k, _, err := Get("linear"); err != nil || k != "lin_api_secretabcd" {
		t.Fatalf("Get = %q %v", k, err)
	}
	if b, err := os.ReadFile(bak); err != nil || string(b) != "old" {
		t.Fatalf(".bak not kept: %q %v", b, err)
	}
}
