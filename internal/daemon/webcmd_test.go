//go:build harness

package daemon

import (
	"errors"
	"strings"
	"testing"
)

func TestWebCommand(t *testing.T) {
	i := &Info{Addr: "127.0.0.1:7477"}
	tok := func() (string, error) { return "tk", nil }
	var opened string
	open := func(u string) error { opened = u; return nil }
	var out, errw strings.Builder

	if code := Web(nil, func() *Info { return i }, tok, open, &out, &errw); code != 0 || out.String() != "http://127.0.0.1:7477/#token=tk\n" || opened != "" {
		t.Fatalf("print: %d %q %q", code, out.String(), opened)
	}
	out.Reset()
	if code := Web([]string{"open"}, func() *Info { return i }, tok, open, &out, &errw); code != 0 || opened != "http://127.0.0.1:7477/#token=tk" {
		t.Fatalf("open: %d %q", code, opened)
	}
	errw.Reset()
	if code := Web(nil, func() *Info { return nil }, tok, open, &out, &errw); code != 1 || !strings.Contains(errw.String(), "smithy daemon --bg") {
		t.Fatalf("no daemon: %d %q", code, errw.String())
	}
	errw.Reset()
	if code := Web(nil, func() *Info { return i }, func() (string, error) { return "", errors.New("gone") }, open, &out, &errw); code != 1 {
		t.Fatalf("no token: %d", code)
	}
	if code := Web([]string{"nope"}, func() *Info { return i }, tok, open, &out, &errw); code != 2 {
		t.Fatalf("bad arg: %d", code)
	}
}
