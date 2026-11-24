package auth

import (
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"

	"github.com/Zovaris/smithy/internal/config"
)

// ProxySecret is a stored key bound to the egress proxy: the hosts and methods it may be sent
// to, whether other write methods wait for the user (Ask), and how it is sent (Header: "Prefix Value").
type ProxySecret struct {
	Value, Header, Prefix string
	Hosts, Methods        []string
	Ask                   bool
}

// DefaultMethods are the methods a bound key is sent with when bind names none: reads only.
var DefaultMethods = []string{"GET", "HEAD"}

var (
	hostname   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	headerName = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	methodSet  = []string{"GET", "HEAD", "OPTIONS", "POST", "PUT", "PATCH", "DELETE"}
)

func methodsOf(e entry) []string {
	if len(e.Methods) == 0 {
		return DefaultMethods
	}
	return e.Methods
}

// Bind lets the proxy send service's stored key to host (added to its hosts) for methods
// (replacing them when given), in header with prefix (kept when empty).
func Bind(svc, host string, methods []string, header, prefix string) error {
	if err := check(svc); err != nil {
		return err
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if !hostname.MatchString(host) || net.ParseIP(host) != nil {
		return fmt.Errorf("bad host %q: a DNS name only, no scheme, port or path", host)
	}
	var ms []string
	for _, m := range methods {
		m = strings.ToUpper(strings.TrimSpace(m))
		if m == "" {
			continue
		}
		if !slices.Contains(methodSet, m) {
			return fmt.Errorf("bad method %q (one of %s)", m, strings.Join(methodSet, ","))
		}
		if !slices.Contains(ms, m) {
			ms = append(ms, m)
		}
	}
	if header != "" && !headerName.MatchString(header) {
		return fmt.Errorf("bad header name %q", header)
	}
	if strings.ContainsFunc(prefix, func(r rune) bool { return r < 0x21 || r > 0x7e }) {
		return fmt.Errorf("bad prefix %q (one word, e.g. Bearer)", prefix)
	}
	return update(svc, func(e *entry) {
		if !slices.Contains(e.Hosts, host) {
			e.Hosts = append(e.Hosts, host)
		}
		if len(ms) > 0 {
			e.Methods = ms
		}
		if header != "" {
			e.Header = header
		}
		if prefix != "" {
			e.Prefix = prefix
		}
	})
}

// SetAsk turns on or off holding service's unlisted write methods for the user's approval.
func SetAsk(svc string, on bool) error {
	if err := check(svc); err != nil {
		return err
	}
	return update(svc, func(e *entry) { e.Ask = on })
}

func update(svc string, f func(*entry)) error {
	return config.WithLock(File(), func() error {
		m, err := load()
		if err != nil {
			return err
		}
		e, ok := m[svc]
		if !ok || strings.TrimSpace(e.Key) == "" {
			return fmt.Errorf("no key for %s: smithy auth set %s first", svc, svc)
		}
		f(&e)
		m[svc] = e
		return save(m)
	})
}

// Secret is service's key from the auth file with its proxy binding; false when it has no key or
// no bound host (an unbound key is never sent by the proxy).
func Secret(svc string) (ProxySecret, bool) {
	if check(svc) != nil {
		return ProxySecret{}, false
	}
	m, err := load()
	if err != nil {
		return ProxySecret{}, false
	}
	e := m[svc]
	k := strings.TrimSpace(e.Key)
	if k == "" || len(e.Hosts) == 0 {
		return ProxySecret{}, false
	}
	h := e.Header
	if h == "" {
		h = "Authorization"
	}
	return ProxySecret{Value: k, Header: h, Prefix: e.Prefix, Hosts: slices.Clone(e.Hosts), Methods: slices.Clone(methodsOf(e)), Ask: e.Ask}, true
}
