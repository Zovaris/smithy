// Package auth keeps source API keys in $SMITHY_HOME/auth.json (mode 0600, outside any repo),
// keyed by service name: {"linear": {"key": "..."}}. The env var named by <service>.key_env
// wins when it is set. A full key is never printed. Mode 0600 keeps other users out, not other
// processes running as you (agents included), so store a test or least-privilege key.
package auth

import (
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
Source API keys in ~/.smithy/auth.json (mode 0600). set reads the key from stdin only, never
from arguments; in a terminal it prompts with echo off. The env var named by <service>.key_env (e.g. LINEAR_API_KEY) wins when set.`

type entry struct {
	Key string `json:"key"`
}

var service = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// File is the auth file path.
func File() string { return filepath.Join(ghx.Home(), "auth.json") }

// EnvVar is the env var that holds service's key: <service>.key_env, default SERVICE_API_KEY.
func EnvVar(svc string) string {
	return config.Get(svc+".key_env", strings.ToUpper(svc)+"_API_KEY")
}

func load() (map[string]entry, error) {
	m := map[string]entry{}
	f := File()
	fi, err := os.Stat(f)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is readable by others; not reading it (chmod 600 %s)", f, f)
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: not valid JSON", f)
	}
	return m, nil
}

func save(m map[string]entry) error {
	f := File()
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
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
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f)
}

func check(svc string) error {
	if !service.MatchString(svc) {
		return fmt.Errorf("bad service name %q (lowercase letters, digits, - and _)", svc)
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
		m, err := load()
		if err != nil {
			return err
		}
		m[svc] = entry{Key: key}
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
		m, err := load()
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
		out = append(out, s+" "+Mask(e.Key))
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
