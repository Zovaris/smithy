package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Zovaris/smithy/internal/layout"
)

// sealed is auth.json at rest: AES-256-GCM over the JSON map, keyed by auth.key. "$enc" is no
// valid service name, so a sealed file never reads as a plaintext map.
type sealed struct {
	V     int    `json:"$enc"`
	Nonce string `json:"nonce"`
	Data  string `json:"data"`
}

// KeyFile holds the data key (hex, mode 0600). Only smithy and its proxy read it; agent sandboxes deny it.
func KeyFile() string { return filepath.Join(layout.Home(), "auth.key") }

// EnsureKey creates the data key in home when it is missing. The key is written to a temp file
// and linked into place, so no reader sees a partial key and an existing key is never replaced.
func EnsureKey(home string) error {
	f := filepath.Join(home, "auth.key")
	if _, err := os.Stat(f); err == nil {
		return nil
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return err
	}
	w, err := os.CreateTemp(home, "auth.key.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(w.Name())
	_, err = w.WriteString(hex.EncodeToString(k) + "\n")
	if err == nil {
		err = w.Sync()
	}
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(w.Name(), 0o600)
	}
	if err != nil {
		return err
	}
	if err := os.Link(w.Name(), f); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	return syncDir(home)
}

func dataKey() (cipher.AEAD, error) {
	f := KeyFile()
	fi, err := os.Stat(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f, err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is readable by others; not reading it (chmod 600 %s)", f, f)
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}
	k, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(k) != 32 {
		return nil, fmt.Errorf("%s: not a 32-byte hex key", f)
	}
	c, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(c)
}

func seal(plain []byte) ([]byte, error) {
	if err := EnsureKey(layout.Home()); err != nil {
		return nil, err
	}
	g, err := dataKey()
	if err != nil {
		return nil, err
	}
	n := make([]byte, g.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return nil, err
	}
	return json.MarshalIndent(sealed{V: 1, Nonce: hex.EncodeToString(n), Data: base64.StdEncoding.EncodeToString(g.Seal(nil, n, plain, []byte("smithy-auth-v1")))}, "", "  ")
}

// open returns the plaintext map JSON and whether b was sealed (false: a pre-v0.22 plaintext file).
func open(b []byte) ([]byte, bool, error) {
	var s sealed
	if json.Unmarshal(b, &s) != nil || s.V == 0 {
		return b, false, nil
	}
	if s.V != 1 {
		return nil, true, fmt.Errorf("%s: unknown format %d (newer smithy?)", File(), s.V)
	}
	g, err := dataKey()
	if err != nil {
		return nil, true, err
	}
	n, err1 := hex.DecodeString(s.Nonce)
	d, err2 := base64.StdEncoding.DecodeString(s.Data)
	if err1 != nil || err2 != nil || len(n) != g.NonceSize() {
		return nil, true, fmt.Errorf("%s: damaged", File())
	}
	p, err := g.Open(nil, n, d, []byte("smithy-auth-v1"))
	if err != nil {
		return nil, true, fmt.Errorf("%s does not match %s; restore the key or set the keys again", File(), KeyFile())
	}
	return p, true, nil
}
