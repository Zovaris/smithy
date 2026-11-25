// Package auth keeps source API keys in $SMITHY_HOME/auth.json (mode 0600, outside any repo),
// keyed by service name: {"linear": {"key": "..."}}, sealed with AES-GCM under auth.key, which
// only smithy and its proxy read and every agent sandbox denies. A plaintext auth.json from an
// older smithy is sealed on first read. The env var named by <service>.key_env wins when it is
// set. A full key is never printed. A process running as you outside a sandbox can still read
// both files, so store a test or least-privilege key.
package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
)

// Usage is the text for `smithy auth --help`.
const Usage = `usage: smithy auth set <service> < keyfile | list | remove <service>
       smithy auth bind <service> <host> [METHODS] [--header <name>] [--prefix <scheme>] | ask <service> on|off
Source API keys in ~/.smithy/auth.json (mode 0600, encrypted with ~/.smithy/auth.key). set reads the key from stdin only, never
from arguments; in a terminal it prompts with echo off. The env var named by <service>.key_env (e.g. LINEAR_API_KEY) wins when set.
bind lets the egress proxy (auto.sandbox_network proxy) send the key to host for METHODS (comma
list, default GET,HEAD) in --header (default Authorization) as "<prefix> <key>"; bind again to add
a host. A container worker calls http://smithy-proxy:8877/s/<service>/<path> and never sees the key.
ask on holds the other write methods until you run smithy proxy approve <id> (denied after a timeout).`

type entry struct {
	Key string `json:"key"`
	// Hosts, Methods, Ask, Header and Prefix bind the key to the egress proxy (smithy auth bind):
	// the proxy sends it only to Hosts, for Methods, in Header as "<Prefix> <key>".
	Hosts   []string `json:"hosts,omitempty"`
	Methods []string `json:"methods,omitempty"`
	Ask     bool     `json:"ask,omitempty"`
	Header  string   `json:"header,omitempty"`
	Prefix  string   `json:"prefix,omitempty"`
}

// service is a name, optionally :<pool account> (claude:work, a pooled account's own token).
var service = regexp.MustCompile(`^[a-z][a-z0-9_-]*(:[A-Za-z0-9_][A-Za-z0-9._-]*)?$`)

// File is the auth file path.
func File() string { return filepath.Join(ghx.Home(), "auth.json") }

// EnvVar is the env var that holds service's key: <service>.key_env, default SERVICE_API_KEY.
func EnvVar(svc string) string {
	return config.Get(svc+".key_env", strings.ToUpper(svc)+"_API_KEY")
}

// load reads the auth file and seals a plaintext one from an older smithy, under the write lock.
// Callers already holding the lock use read.
func load() (map[string]entry, error) {
	m, sealed, err := read()
	if err != nil || sealed || len(m) == 0 {
		return m, err
	}
	// a read-only mount (the proxy sidecar) can't be sealed and keeps reading the plaintext
	_ = config.WithLock(File(), func() error {
		if m2, sealed, err := read(); err == nil && !sealed && len(m2) > 0 {
			return save(m2)
		}
		return nil
	})
	return m, nil
}

func read() (map[string]entry, bool, error) {
	m := map[string]entry{}
	f := File()
	fi, err := os.Stat(f)
	if os.IsNotExist(err) {
		return m, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return nil, false, fmt.Errorf("%s is readable by others; not reading it (chmod 600 %s)", f, f)
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, false, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		// an empty file (a write cut short before fsync) holds no secrets; a .bak next to it is left alone
		return m, true, nil
	}
	p, wasSealed, err := open(b)
	if err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal(p, &m); err != nil {
		return nil, false, fmt.Errorf("%s: not valid JSON", f)
	}
	return m, wasSealed, nil
}

func save(m map[string]entry) error {
	f := File()
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	plain, _ := json.Marshal(m)
	b, err := seal(plain)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f), "auth.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), f); err != nil {
		return err
	}
	return syncDir(filepath.Dir(f))
}

// syncDir makes a rename in dir durable; Windows can't open a folder for sync, so it is skipped there.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func check(svc string) error {
	if !service.MatchString(svc) {
		return fmt.Errorf("bad service name %q (lowercase letters, digits, - and _, optionally :<pool account>)", svc)
	}
	return nil
}

// Get returns service's key and where it came from (the env var name or the file). No key is
// "" with a nil error; a file others can read is an error and is not used.
func Get(svc string) (key, from string, err error) {
	if err := check(svc); err != nil {
		return "", "", err
	}
	env := EnvVar(svc)
	if k := strings.TrimSpace(os.Getenv(env)); k != "" {
		return k, env, nil
	}
	m, err := load()
	if err != nil {
		return "", "", err
	}
	if k := strings.TrimSpace(m[svc].Key); k != "" {
		return k, File(), nil
	}
	return "", "", nil
}

// Stored is service's key from the auth file only, never an env var (whose name a repo's
// <service>.key_env could pick). No key or an unreadable file is "".
func Stored(svc string) string {
	if check(svc) != nil {
		return ""
	}
	m, err := load()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(m[svc].Key)
}

// Set stores service's key, atomically, mode 0600.
func Set(svc, key string) error {
	if err := check(svc); err != nil {
		return err
	}
	if key = strings.TrimSpace(key); key == "" {
		return fmt.Errorf("empty key")
	}
	if strings.ContainsFunc(key, unicode.IsSpace) {
		return fmt.Errorf("the key contains whitespace; paste only the key")
	}
	dir := filepath.Dir(File())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	return config.WithLock(File(), func() error {
		m, _, err := read()
		if err != nil {
			return err
		}
		e := m[svc]
		e.Key = key
		m[svc] = e
		return save(m)
	})
}

// Remove deletes service's key; false when there was none.
func Remove(svc string) (bool, error) {
	if err := check(svc); err != nil {
		return false, err
	}
	found := false
	err := config.WithLock(File(), func() error {
		m, _, err := read()
		if err != nil {
			return err
		}
		if _, found = m[svc]; !found {
			return nil
		}
		delete(m, svc)
		return save(m)
	})
	return found, err
}

// Mask shows only the last 4 characters of a key of 16 or more, and none of a shorter one.
func Mask(key string) string {
	if len(key) < 16 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}

// List is one "<service> ****abcd" line per stored key, sorted.
func List() ([]string, error) {
	m, err := load()
	if err != nil {
		return nil, err
	}
	var out []string
	for s, e := range m {
		line := s + " " + Mask(e.Key)
		if len(e.Hosts) > 0 {
			line += " -> " + strings.Join(e.Hosts, ",") + " " + strings.Join(methodsOf(e), ",")
			if e.Ask {
				line += " ask"
			}
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out, nil
}

// Main runs `smithy auth`.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fail := func(code int, err error) int {
		fmt.Fprintln(stderr, "smithy auth:", err)
		return code
	}
	switch {
	case len(args) == 2 && args[0] == "set":
		if stdin == nil {
			return fail(2, fmt.Errorf("pipe the key on stdin"))
		}
		var k string
		if f, ok := stdin.(*os.File); ok && isTTY(f) {
			line, err := readHidden(f, args[1], stderr)
			if err != nil {
				return fail(1, err)
			}
			k = strings.TrimSpace(line)
		} else {
			b, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
			if err != nil {
				return fail(1, err)
			}
			k = strings.TrimSpace(string(b))
		}
		if k == "" {
			return fail(2, fmt.Errorf("no key on stdin (smithy auth set %s < keyfile)", args[1]))
		}
		if err := Set(args[1], k); err != nil {
			return fail(1, err)
		}
		fmt.Fprintf(stdout, "saved %s %s\n", args[1], Mask(k))
		return 0
	case len(args) > 2 && args[0] == "set":
		return fail(2, fmt.Errorf("the key is read from stdin only, never from arguments"))
	case len(args) >= 3 && args[0] == "bind":
		var methods []string
		header, prefix := "", ""
		rest := args[3:]
		for len(rest) > 0 {
			switch {
			case rest[0] == "--header" && len(rest) > 1:
				header, rest = rest[1], rest[2:]
			case rest[0] == "--prefix" && len(rest) > 1:
				prefix, rest = rest[1], rest[2:]
			case methods == nil && !strings.HasPrefix(rest[0], "-"):
				methods, rest = strings.Split(rest[0], ","), rest[1:]
			default:
				fmt.Fprintln(stderr, Usage)
				return 2
			}
		}
		if err := Bind(args[1], args[2], methods, header, prefix); err != nil {
			return fail(1, err)
		}
		s, _ := Secret(args[1])
		fmt.Fprintf(stdout, "bound %s -> %s %s\n", args[1], strings.Join(s.Hosts, ","), strings.Join(s.Methods, ","))
		return 0
	case len(args) == 3 && args[0] == "ask" && (args[2] == "on" || args[2] == "off"):
		if err := SetAsk(args[1], args[2] == "on"); err != nil {
			return fail(1, err)
		}
		fmt.Fprintf(stdout, "ask %s %s\n", args[1], args[2])
		return 0
	case len(args) == 1 && args[0] == "list":
		l, err := List()
		if err != nil {
			return fail(1, err)
		}
		for _, x := range l {
			fmt.Fprintln(stdout, x)
		}
		return 0
	case len(args) == 2 && args[0] == "remove":
		ok, err := Remove(args[1])
		if err != nil {
			return fail(1, err)
		}
		if !ok {
			return fail(1, fmt.Errorf("no key for %s", args[1]))
		}
		fmt.Fprintln(stdout, "auth removed", args[1])
		return 0
	}
	fmt.Fprintln(stderr, Usage)
	return 2
}
