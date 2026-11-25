// Package daemon is smithy daemon's local API: auth, the loopback listener and the info file
// (stdlib, in both builds, so doctor can read them) plus the store-backed server (harness build).
package daemon

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/ghx"
)

// LoginHeader is the identity header tailscale serve adds to proxied requests.
const LoginHeader = "Tailscale-User-Login"

// DefaultPort is daemon.port's default.
const DefaultPort = 7477

// TokenFile is <smithy home>/daemon.token.
func TokenFile() string { return filepath.Join(ghx.Home(), "daemon.token") }

// InfoFile is <smithy home>/daemon.json, written while the daemon runs.
func InfoFile() string { return filepath.Join(ghx.Home(), "daemon.json") }

// LoadToken reads the token at path. With create, a missing file gets a new random token (0600).
// A file readable by group or others is refused.
func LoadToken(path string, create bool) (string, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		tok := hex.EncodeToString(b)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return "", err
		}
		defer f.Close()
		_, err = f.WriteString(tok + "\n")
		return tok, err
	}
	if err != nil {
		return "", err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s is readable by others (mode %v); chmod 600 it", path, fi.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return tok, nil
}

// AllowList parses daemon.allow: a comma list of Tailscale logins, lower-cased.
func AllowList(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.ToLower(strings.TrimSpace(f)); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// Authorize accepts a request to a loopback Host with the bearer token, or one to a *.ts.net Host
// (as tailscale serve forwards it) whose Tailscale login is in allow; any other Host is refused,
// so a DNS-rebound page cannot use the API. It returns who asked: "local" for the token, else the login.
func Authorize(r *http.Request, token string, allow []string) (string, bool) {
	loopback, tailnet := hostKind(r)
	if !loopback {
		if !tailnet {
			return "", false
		}
		token = ""
	}
	if token != "" {
		if got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok &&
			subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(token)) == 1 {
			return "local", true
		}
	}
	if loopback {
		return "", false
	}
	return allowedLogin(r, allow)
}

func hostKind(r *http.Request) (loopback, tailnet bool) {
	host := strings.ToLower(r.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	loopback = host == "localhost"
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		loopback = true
	}
	return loopback, !loopback && strings.HasSuffix(strings.TrimSuffix(host, "."), ".ts.net")
}

func allowedLogin(r *http.Request, allow []string) (string, bool) {
	login := strings.ToLower(strings.TrimSpace(r.Header.Get(LoginHeader)))
	if login == "" {
		return "", false
	}
	for _, a := range allow {
		if a != "" && a == login {
			return login, true
		}
	}
	return "", false
}

// ShellAllowed gates the web shell's static files: any loopback Host (the page itself holds no
// secret; its API calls carry the token from the URL fragment), or a *.ts.net Host with an allowed
// login. Any other Host is refused, as in Authorize, so a DNS-rebound page gets nothing.
func ShellAllowed(r *http.Request, allow []string) bool {
	loopback, tailnet := hostKind(r)
	if loopback {
		return true
	}
	if !tailnet {
		return false
	}
	_, ok := allowedLogin(r, allow)
	return ok
}

// CheckLoopback refuses any listen address that is not a loopback IP or localhost.
func CheckLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("smithy daemon listens on loopback only, not %q", addr)
}

// Listen binds 127.0.0.1:port (0 picks a free port).
func Listen(port int) (net.Listener, error) {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if err := CheckLoopback(addr); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if err := CheckLoopback(ln.Addr().String()); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// Info is daemon.json: who runs and where.
type Info struct {
	PID      int       `json:"pid"`
	PIDStart string    `json:"pid_start,omitempty"`
	Addr     string    `json:"addr"`
	Started  time.Time `json:"started"`
	Log      string    `json:"log,omitempty"`
}

// URL is the daemon's base URL.
func (i Info) URL() string { return "http://" + i.Addr }

// WebURL is the web shell's URL with the token in the fragment, which browsers never send.
func (i Info) WebURL(token string) string { return i.URL() + "/#token=" + url.QueryEscape(token) }

// ReadInfo reads daemon.json; nil when there is none.
func ReadInfo() (*Info, error) {
	b, err := os.ReadFile(InfoFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var i Info
	if err := json.Unmarshal(b, &i); err != nil {
		return nil, fmt.Errorf("%s: %w", InfoFile(), err)
	}
	return &i, nil
}

// WriteInfo writes daemon.json (0600).
func WriteInfo(i Info) error {
	b, _ := json.MarshalIndent(i, "", "  ")
	p := InfoFile()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
