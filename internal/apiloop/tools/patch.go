package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type patchOp struct {
	kind, path string
	add        []string   // Add File lines
	hunks      [][]string // Update File hunks: lines starting with ' ', '-' or '+'
}

// parsePatch reads the *** Begin Patch format OpenAI models write for apply_patch.
func parsePatch(p string) ([]patchOp, error) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(p), "\r\n", "\n"), "\n")
	if len(lines) < 2 || lines[0] != "*** Begin Patch" || lines[len(lines)-1] != "*** End Patch" {
		return nil, errors.New("patch must start with *** Begin Patch and end with *** End Patch")
	}
	var ops []patchOp
	var cur *patchOp
	for _, l := range lines[1 : len(lines)-1] {
		head := func(pre string) (string, bool) { return strings.CutPrefix(l, pre) }
		if path, ok := head("*** Add File: "); ok {
			ops = append(ops, patchOp{kind: "add", path: strings.TrimSpace(path)})
			cur = &ops[len(ops)-1]
			continue
		}
		if path, ok := head("*** Delete File: "); ok {
			ops = append(ops, patchOp{kind: "delete", path: strings.TrimSpace(path)})
			cur = nil
			continue
		}
		if path, ok := head("*** Update File: "); ok {
			ops = append(ops, patchOp{kind: "update", path: strings.TrimSpace(path)})
			cur = &ops[len(ops)-1]
			continue
		}
		if strings.HasPrefix(l, "*** Move to: ") {
			return nil, errors.New("apply_patch: *** Move to is not supported; add the new file and delete the old one")
		}
		if l == "*** End of File" {
			continue
		}
		if cur == nil {
			return nil, fmt.Errorf("apply_patch: line outside a file section: %q", l)
		}
		switch {
		case cur.kind == "add":
			t, ok := strings.CutPrefix(l, "+")
			if !ok {
				return nil, fmt.Errorf("apply_patch: Add File lines start with +: %q", l)
			}
			cur.add = append(cur.add, t)
		case strings.HasPrefix(l, "@@"):
			cur.hunks = append(cur.hunks, nil)
		case l == "" || l[0] == ' ' || l[0] == '-' || l[0] == '+':
			if len(cur.hunks) == 0 {
				cur.hunks = append(cur.hunks, nil)
			}
			if l == "" {
				l = " "
			}
			cur.hunks[len(cur.hunks)-1] = append(cur.hunks[len(cur.hunks)-1], l)
		default:
			return nil, fmt.Errorf("apply_patch: bad line %q", l)
		}
	}
	if len(ops) == 0 {
		return nil, errors.New("apply_patch: no file sections")
	}
	return ops, nil
}

// applyPatch checks every section applies before it writes any file.
func (s *Set) applyPatch(p string) (string, error) {
	ops, err := parsePatch(p)
	if err != nil {
		return "", err
	}
	writes := map[string]*string{}
	for _, op := range ops {
		full := s.abs(op.path)
		switch op.kind {
		case "add":
			if _, err := os.Stat(full); err == nil {
				return "", fmt.Errorf("apply_patch: %s already exists", op.path)
			}
			c := strings.Join(op.add, "\n") + "\n"
			writes[full] = &c
		case "delete":
			if _, err := os.Stat(full); err != nil {
				return "", fmt.Errorf("apply_patch: %v", err)
			}
			writes[full] = nil
		case "update":
			b, err := os.ReadFile(full)
			if err != nil {
				return "", fmt.Errorf("apply_patch: %v", err)
			}
			c, err := applyHunks(string(b), op.hunks)
			if err != nil {
				return "", fmt.Errorf("apply_patch: %s: %v", op.path, err)
			}
			writes[full] = &c
		}
	}
	for full, c := range writes {
		if c == nil {
			if err := os.Remove(full); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
		if err := writeKeepMode(full, *c); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("applied %d file change(s)", len(ops)), nil
}

func applyHunks(content string, hunks [][]string) (string, error) {
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	pos := 0
	for _, h := range hunks {
		var old, repl []string
		for _, l := range h {
			switch l[0] {
			case ' ':
				old, repl = append(old, l[1:]), append(repl, l[1:])
			case '-':
				old = append(old, l[1:])
			case '+':
				repl = append(repl, l[1:])
			}
		}
		at := find(lines, old, pos)
		if at < 0 {
			return "", fmt.Errorf("hunk does not match the file:\n%s", strings.Join(h, "\n"))
		}
		lines = append(lines[:at:at], append(repl, lines[at+len(old):]...)...)
		pos = at + len(repl)
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func find(lines, seq []string, from int) int {
	if len(seq) == 0 {
		return from
	}
	for i := from; i+len(seq) <= len(lines); i++ {
		ok := true
		for j := range seq {
			if lines[i+j] != seq[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}
