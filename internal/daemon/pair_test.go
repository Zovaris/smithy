package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newPairing(t *testing.T) (*Pairing, *time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	p := &Pairing{File: filepath.Join(t.TempDir(), "devices.json"), Now: func() time.Time { return now }}
	return p, &now
}

func TestPairCodeIsOneTimeAndShortLived(t *testing.T) {
	p, now := newPairing(t)
	code, exp := p.NewCode()
	if len(code) != 9 || code[4] != '-' || exp.Sub(*now) != PairTTL {
		t.Fatalf("code %q exp %v", code, exp)
	}
	tok, err := p.Redeem(code, "laptop")
	if err != nil || len(tok) < 40 {
		t.Fatalf("%q %v", tok, err)
	}
	if !p.Valid(tok) || p.Valid("nope") || p.Valid("") {
		t.Fatal("the device token is valid, others are not")
	}
	if _, err := p.Redeem(code, "laptop2"); err == nil {
		t.Fatal("a code works once")
	}
	c2, _ := p.NewCode()
	*now = now.Add(PairTTL + time.Second)
	if _, err := p.Redeem(c2, "late"); err == nil {
		t.Fatal("an expired code is refused")
	}
	b, _ := os.ReadFile(p.File)
	if strings.Contains(string(b), tok) {
		t.Fatal("the devices file keeps a hash, never the token")
	}
	fi, _ := os.Stat(p.File)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestPairFailuresLockOutPerAddress(t *testing.T) {
	p, now := newPairing(t)
	code, _ := p.NewCode()
	for i := 0; i < PairMaxFails; i++ {
		if _, err := p.RedeemFrom("1.2.3.4", "ZZZZ-ZZZZ", "x"); err == nil {
			t.Fatal("wrong code accepted")
		}
	}
	if _, err := p.RedeemFrom("1.2.3.4", code, "late"); err == nil {
		t.Fatal("an address with too many wrong codes is locked out")
	}
	p.NewCode()
	if _, err := p.RedeemFrom("1.2.3.4", code, "late"); err == nil {
		t.Fatal("a new code does not lift the lockout")
	}
	for i := 0; i < 20*PairMaxFails; i++ {
		p.RedeemFrom(fmt.Sprint("10.0.", i/250, ".", i%250), "ZZZZ-ZZZZ", "x")
	}
	if _, err := p.RedeemFrom("5.6.7.8", code, "other"); err != nil {
		t.Fatalf("failures elsewhere never burn an open code: %v", err)
	}
	*now = now.Add(PairFailWindow + time.Second)
	c2, _ := p.NewCode()
	if _, err := p.RedeemFrom("1.2.3.4", c2, "later"); err != nil {
		t.Fatalf("the lockout ends after the window: %v", err)
	}
}

func TestPairDevicesAndRevoke(t *testing.T) {
	p, _ := newPairing(t)
	c, _ := p.NewCode()
	tok, _ := p.Redeem(c, "phone")
	c2, _ := p.NewCode()
	if _, err := p.Redeem(c2, "phone"); err == nil {
		t.Fatal("device names are unique")
	}
	ds, err := p.Devices()
	if err != nil || len(ds) != 1 || ds[0].Name != "phone" {
		t.Fatalf("%+v %v", ds, err)
	}
	if err := p.Revoke("phone"); err != nil {
		t.Fatal(err)
	}
	if p.Valid(tok) {
		t.Fatal("a revoked device is refused")
	}
	if err := p.Revoke("phone"); err == nil {
		t.Fatal("unknown device")
	}
}

func TestCheckRemoteListen(t *testing.T) {
	for _, ok := range []string{"100.64.0.1:7478", "100.127.255.254:7478", "[fd7a:115c:a1e0::1]:7478"} {
		if err := CheckRemoteListen(ok); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "127.0.0.1:7478", "localhost:1", "nope", "1.2.3.4", "0.0.0.0:7478", "[::]:7478", ":7478",
		"192.168.1.5:7478", "100.128.0.1:7478", "[fd7a::1]:7478", "[fd7a:115c:a1e1::1]:7478"} {
		if err := CheckRemoteListen(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
		if bad == "192.168.1.5:7478" && !strings.Contains(CheckRemoteListen(bad).Error(), "tailscale serve") {
			t.Fatalf("the error names the way out: %v", CheckRemoteListen(bad))
		}
	}
}
