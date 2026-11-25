package daemon

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
)

// PairTTL is how long a pairing code works.
const PairTTL = 5 * time.Minute

// PairMaxFails is how many wrong codes burn every open code.
const PairMaxFails = 5

// codeAlphabet has no 0/O or 1/I/L, so a code read aloud survives.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// DevicesFile is <smithy home>/daemon.devices.json: the paired devices, token hashes only.
func DevicesFile() string { return filepath.Join(ghx.Home(), "daemon.devices.json") }

// Device is one paired client.
type Device struct {
	Name    string    `json:"name"`
	Hash    string    `json:"hash"`
	Created time.Time `json:"created"`
}

// Pairing hands out one-time codes and checks the device tokens they were traded for.
type Pairing struct {
	File string
	Now  func() time.Time

	mu    sync.Mutex
	codes map[string]time.Time
	fails map[string]int // wrong codes per remote address since the last new code
	total int
}

func (p *Pairing) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func randomCode() string {
	limit := byte(256 - 256%len(codeAlphabet))
	out := make([]byte, 0, 9)
	b := make([]byte, 1)
	for len(out) < 9 {
		if len(out) == 4 {
			out = append(out, '-')
			continue
		}
		rand.Read(b)
		if b[0] < limit {
			out = append(out, codeAlphabet[int(b[0])%len(codeAlphabet)])
		}
	}
	return string(out)
}

// NewCode makes a code that works once until it expires; it also resets the failure count.
func (p *Pairing) NewCode() (string, time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.codes == nil {
		p.codes = map[string]time.Time{}
	}
	now := p.now()
	for c, exp := range p.codes {
		if !now.Before(exp) {
			delete(p.codes, c)
		}
	}
	c, exp := randomCode(), now.Add(PairTTL)
	p.codes[c], p.fails, p.total = exp, nil, 0
	return c, exp
}

var errBadCode = errors.New("unknown or expired code; make a new one with smithy pair on the server")

// Redeem trades code for a new device token saved under name.
func (p *Pairing) Redeem(code, name string) (string, error) { return p.RedeemFrom("", code, name) }

// RedeemFrom is Redeem for a caller at addr: PairMaxFails wrong codes lock that address out until
// the next new code, and ten times as many from anywhere burn every open code.
func (p *Pairing) RedeemFrom(addr, code, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", errors.New("bad device name")
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	p.mu.Lock()
	now := p.now()
	if p.fails[addr] >= PairMaxFails {
		p.mu.Unlock()
		return "", errBadCode
	}
	var hit string
	for c, exp := range p.codes {
		if subtle.ConstantTimeCompare([]byte(c), []byte(code)) == 1 && now.Before(exp) {
			hit = c
		}
	}
	if hit == "" {
		if p.fails == nil {
			p.fails = map[string]int{}
		}
		p.fails[addr]++
		if p.total++; p.total >= 10*PairMaxFails {
			p.codes = nil
		}
		p.mu.Unlock()
		return "", errBadCode
	}
	delete(p.codes, hit)
	p.mu.Unlock()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := "smd_" + hex.EncodeToString(b)
	err := p.edit(func(ds []Device) ([]Device, error) {
		if slices.ContainsFunc(ds, func(d Device) bool { return d.Name == name }) {
			return nil, fmt.Errorf("a device named %q is already paired; smithy pair revoke it first or pick another name", name)
		}
		return append(ds, Device{Name: name, Hash: hashToken(tok), Created: now.UTC()}), nil
	})
	if err != nil {
		return "", err
	}
	return tok, nil
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func (p *Pairing) load() ([]Device, error) {
	b, err := os.ReadFile(p.File)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ds []Device
	return ds, json.Unmarshal(b, &ds)
}

func (p *Pairing) edit(f func([]Device) ([]Device, error)) error {
	return config.WithLock(p.File, func() error {
		ds, err := p.load()
		if err != nil {
			return err
		}
		if ds, err = f(ds); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(p.File), 0o700); err != nil {
			return err
		}
		b, _ := json.MarshalIndent(ds, "", "  ")
		tmp := p.File + ".tmp"
		if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
			return err
		}
		os.Chmod(tmp, 0o600)
		return os.Rename(tmp, p.File)
	})
}

// Valid reports whether tok belongs to a paired device.
func (p *Pairing) Valid(tok string) bool { _, ok := p.Who(tok); return ok }

// Who is the paired device holding tok.
func (p *Pairing) Who(tok string) (string, bool) {
	if p == nil || len(tok) != 68 || !strings.HasPrefix(tok, "smd_") {
		return "", false
	}
	ds, err := p.load()
	if err != nil {
		return "", false
	}
	h := hashToken(tok)
	for _, d := range ds {
		if subtle.ConstantTimeCompare([]byte(d.Hash), []byte(h)) == 1 {
			return d.Name, true
		}
	}
	return "", false
}

// Devices lists the paired devices.
func (p *Pairing) Devices() ([]Device, error) { return p.load() }

// Revoke forgets every device named name.
func (p *Pairing) Revoke(name string) error {
	return p.edit(func(ds []Device) ([]Device, error) {
		n := len(ds)
		ds = slices.DeleteFunc(ds, func(d Device) bool { return d.Name == name })
		if len(ds) == n {
			return nil, fmt.Errorf("no paired device %q", name)
		}
		return ds, nil
	})
}

// CheckRemoteListen accepts daemon.listen: a host:port that is not loopback. That listener takes
// only bearer tokens (the daemon token or a paired device's), never the Tailscale login header.
func CheckRemoteListen(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return fmt.Errorf("daemon.listen takes host:port, not %q", addr)
	}
	if CheckLoopback(addr) == nil {
		return fmt.Errorf("daemon.listen %q is loopback; the daemon already listens there", addr)
	}
	if host != "" && net.ParseIP(host) == nil {
		return fmt.Errorf("daemon.listen takes an IP address, not %q", host)
	}
	return nil
}
