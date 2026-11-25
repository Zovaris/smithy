package main

import "testing"

func TestBareNeedsTerminal(t *testing.T) {
	called := false
	open := func() int { called = true; return 0 }
	if code := bare(open, false); code != 2 || called {
		t.Fatalf("no terminal: code %d, opened %v; want usage and 2", code, called)
	}
	if code := bare(open, true); code != 0 || !called {
		t.Fatalf("terminal: code %d, opened %v", code, called)
	}
	if code := bare(nil, true); code != 2 {
		t.Fatalf("skills build: code %d, want 2", code)
	}
}
