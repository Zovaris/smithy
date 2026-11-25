package warden

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type fix struct {
	change string
	apply  func() error
	grant  bool // widens a permission: never applied by --yes, only on an explicit y
}

var stdinTTY = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

var stdinReader = bufio.NewReader(os.Stdin)

var askFix = func(change string) bool {
	fmt.Printf("apply? %s [y/N] ", change)
	line, _ := stdinReader.ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes"
}

func applyFixes(fixes []fix, yes bool) {
	if len(fixes) == 0 {
		return
	}
	fmt.Println("fixes")
	for _, f := range fixes {
		fmt.Printf("  fix   %s\n", f.change)
	}
	if !yes && !stdinTTY() {
		fmt.Println("no terminal to ask: nothing applied (run smithy doctor --fix --yes to apply all)")
		return
	}
	for _, f := range fixes {
		if yes && f.grant {
			fmt.Println("skipped (grants a permission, answer it without --yes): " + f.change)
			continue
		}
		if !yes && !askFix(f.change) {
			fmt.Println("skipped: " + f.change)
			continue
		}
		if err := f.apply(); err != nil {
			fmt.Println("failed: " + f.change + ": " + err.Error())
			continue
		}
		fmt.Println("changed: " + f.change)
	}
}

func addAllowRule(path, rule string) error {
	m := map[string]any{}
	mode := os.FileMode(0o600)
	if !settingsFixable(path) {
		return errors.New("not a regular JSON file; edit it by hand")
	}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &m); err != nil || m == nil {
			return errors.New("not a JSON object; edit it by hand")
		}
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	perms, ok := m["permissions"].(map[string]any)
	if m["permissions"] != nil && !ok {
		return errors.New("permissions is not an object; edit it by hand")
	}
	if perms == nil {
		perms = map[string]any{}
	}
	allow, ok := perms["allow"].([]any)
	if perms["allow"] != nil && !ok {
		return errors.New("permissions.allow is not a list; edit it by hand")
	}
	perms["allow"] = append(allow, rule)
	m["permissions"] = perms
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func settingsFixable(path string) bool {
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return false
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return true
	}
	var m map[string]any
	return err == nil && json.Unmarshal(b, &m) == nil && m != nil
}
