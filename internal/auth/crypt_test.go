package auth

import (
	"bytes"
	"os"
	"runtime"
	"testing"
)

func TestFileEncryptedAtRest(t *testing.T) {
	home(t)
	if err := Set("linear", "lin_api_secretabcd"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(File())
	if bytes.Contains(b, []byte("lin_api_secretabcd")) || bytes.Contains(b, []byte("linear")) {
		t.Fatalf("auth.json holds plaintext: %s", b)
	}
	fi, err := os.Stat(KeyFile())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("key mode %v", fi.Mode().Perm())
	}
	if k := Stored("linear"); k != "lin_api_secretabcd" {
		t.Errorf("Stored = %q", k)
	}
}

func TestMigratesPlaintext(t *testing.T) {
	h := home(t)
	os.MkdirAll(h, 0o700)
	if err := os.WriteFile(File(), []byte(`{"linear":{"key":"lin_api_oldkey1234","hosts":["api.linear.app"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if k := Stored("linear"); k != "lin_api_oldkey1234" {
		t.Fatalf("Stored = %q", k)
	}
	b, _ := os.ReadFile(File())
	if bytes.Contains(b, []byte("lin_api_oldkey1234")) {
		t.Fatalf("not migrated: %s", b)
	}
	if l, _ := List(); len(l) != 1 || !bytes.Contains([]byte(l[0]), []byte("api.linear.app")) {
		t.Errorf("List = %v", l)
	}
}

func TestWrongKeyRefused(t *testing.T) {
	home(t)
	if err := Set("linear", "lin_api_secretabcd"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(KeyFile(), bytes.Repeat([]byte{'0'}, 64), 0o600)
	if _, _, err := Get("linear"); err == nil {
		t.Error("decrypted with a wrong key")
	}
}
