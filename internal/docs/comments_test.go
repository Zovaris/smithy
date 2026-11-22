package docs

import (
	"fmt"
	"strings"
	"testing"
)

func findings(t *testing.T, name, src string) []string {
	t.Helper()
	res, ok := CheckFile(name, src)
	if !ok {
		t.Fatalf("%s: language not checked", name)
	}
	var out []string
	for _, f := range res {
		out = append(out, fmt.Sprintf("%d: %s", f.Line, f.Why))
	}
	return out
}

func expect(t *testing.T, name, src string, want ...string) {
	t.Helper()
	got := findings(t, name, src)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got %q\nwant %q", name, got, want)
	}
}

func TestCommentsTypeScript(t *testing.T) {
	expect(t, "a.ts", `// SPDX-License-Identifier: MIT
/** Adds two numbers. */
export function add(a: number, b: number): number {
  // eslint-disable-next-line no-console -- debug output is part of the CLI contract
  console.log("http://example.com // not a comment");
  // @ts-expect-error legacy types are wrong for this call
  // prettier-ignore
  return a + b;
}
`)
	expect(t, "b.ts", `export function sub(a: number, b: number) {
  // subtract because the API wants deltas
  const d = a - b; // keep it signed
  /* old version */
  // return a + b;
  // TODO: handle overflow
  // eslint-disable-next-line no-console
  // @ts-ignore
  // @ts-expect-error
  /*
   FIXME inside a block
  */
  return d;
}
`,
		"2: explanatory comment",
		"3: trailing explanatory comment",
		"4: block comment; move it to docs or a doc comment",
		"5: commented-out code",
		"6: TODO/FIXME in a comment; open an issue instead",
		"7: directive without a reason",
		"8: @ts-ignore is not allowed; use @ts-expect-error with a reason",
		"9: directive without a reason",
		"10: block comment; move it to docs or a doc comment",
		"11: TODO/FIXME in a comment; open an issue instead",
	)
}

func TestCommentsJSDocOnlyForJSLike(t *testing.T) {
	expect(t, "a.go", "/** Not for Go. */\npackage a\n", "1: block comment; move it to docs or a doc comment")
	expect(t, "a.java", "/** Fine in Java. */\nclass A {}\n")
	expect(t, "a.js", "/* Copyright 2024 Someone */\nconst a = 1;\n")
}

func TestCommentsGo(t *testing.T) {
	expect(t, "a.go", `//go:build linux

// Package a does things.
package a

// Add returns the sum.
func Add(a, b int) int { return a + b } //nolint:revive // name kept for API compatibility

// T is a type.
type T struct {
	// Name is exported.
	Name string
}
`)
	expect(t, "b.go", `package b

func helper() int {
	// internal note
	return 1 //nolint:revive
}
`, "4: explanatory comment", "5: directive without a reason")
}

func TestCommentsRust(t *testing.T) {
	expect(t, "a.rs", `//! Crate docs.
/// Adds.
pub fn add(a: i32) -> i32 {
    // SAFETY: pointer is valid for the whole call
    unsafe { a }
    // plain note
}
`, "6: explanatory comment")
}

func TestCommentsPython(t *testing.T) {
	expect(t, "a.py", `#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Module docstring."""
import os  # noqa: F401 re-exported for plugins
x = "a # not a comment"
y = foo()  # type: ignore[attr-defined]
z = 1  # pragma: no cover
`)
	expect(t, "b.py", `def f():
    # explain the loop
    x = 1  # trailing note
    # FIXME later
    y = g()  # noqa
    z = h()  # type: ignore
    # x = compute()
    return x
`,
		"2: explanatory comment",
		"3: trailing explanatory comment",
		"4: TODO/FIXME in a comment; open an issue instead",
		"5: bare noqa; name the rule",
		"6: bare type: ignore; name the error code",
		"7: commented-out code",
	)
}

func TestCommentsShell(t *testing.T) {
	expect(t, "a.sh", `#!/usr/bin/env bash
# tool: does a thing.
#   tool.sh <arg>
set -e
echo "${PATH#/usr}"
echo ${#x}
# shellcheck disable=SC2086
echo $x
`)
	expect(t, "b.sh", `#!/usr/bin/env bash
set -e
# explain this step
echo hi # trailing
`, "3: explanatory comment", "4: trailing explanatory comment")
}

func TestCommentsSkipsOtherLanguages(t *testing.T) {
	if _, ok := CheckFile("a.md", "# heading"); ok {
		t.Error("markdown should not be checked")
	}
	if _, ext := splitExt("dir.v2/.bashrc"); ext != "" {
		t.Errorf("dotfile ext = %q, want none", ext)
	}
}

func TestAddedLines(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -3,0 +4,2 @@\n+x\n+y\n@@ -9 +11 @@\n-z\n+w\n"
	got := AddedLines(diff)["a.go"]
	for _, n := range []int{4, 5, 11} {
		if !got[n] {
			t.Errorf("line %d not added: %v", n, got)
		}
	}
	if got[3] || got[6] || got[10] {
		t.Errorf("extra lines: %v", got)
	}
}

func TestSmithyAllowMarkerIsNotAComment(t *testing.T) {
	if f := CheckSlash([]string{`const k = "AKIA0000000000000000" // smithy:allow`}, ".ts"); len(f) != 0 {
		t.Fatalf("slash marker flagged: %v", f)
	}
	if f := CheckHash([]string{`k = "AKIA0000000000000000"  # smithy:allow`}, ".py"); len(f) != 0 {
		t.Fatalf("hash marker flagged: %v", f)
	}
}
