package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/apiloop"
)

func TestReadOnlyOffersAndPermitsOnlyReads(t *testing.T) {
	s, _ := newSet(t)
	s.Model = "gpt-5"
	s.ReadOnly = true
	var names []string
	for _, sp := range s.Specs() {
		names = append(names, sp.Name)
	}
	if strings.Join(names, ",") != "read,glob,grep" {
		t.Fatalf("read-only specs %v", names)
	}
	for _, c := range []apiloop.ToolCall{call("write", map[string]any{"path": "a", "content": "x"}), call("edit", map[string]any{"path": "a", "old": "x", "new": "y"}),
		call("shell", map[string]any{"command": "echo hi"}), call("fetch", map[string]any{"url": "https://example.test/"}), call("apply_patch", map[string]any{"patch": ""})} {
		if ok, _ := s.Permit(c); ok {
			t.Errorf("%s permitted in a read-only run", c.Name)
		}
	}
	if ok, why := s.Permit(call("read", map[string]any{"path": "a"})); !ok {
		t.Fatalf("read refused: %s", why)
	}
}

func TestReadRefusesDevicesFast(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("no /dev/zero")
	}
	s, _ := newSet(t)
	done := make(chan error, 1)
	go func() {
		_, err := s.Call(context.Background(), call("read", map[string]any{"path": "/dev/zero"}))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("want a non-regular-file error, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("read of /dev/zero did not return")
	}
}

func TestShellTimeoutKillsBackgroundChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	s, dir := newSet(t)
	mark := filepath.Join(dir, "mark")
	start := time.Now()
	_, err := s.Call(context.Background(), call("shell", map[string]any{"command": "(sleep 2; touch " + mark + ") & sleep 30", "timeout": 1}))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timeout, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %s: a background child held the command open", time.Since(start))
	}
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("the background child outlived the timeout")
	}
}

func TestOverflowFilesLiveInTheRunTempDir(t *testing.T) {
	s, _ := newSet(t)
	out, _ := run(t, s, call("shell", map[string]any{"command": "seq 1 40000"}))
	path := strings.TrimSpace(out[strings.LastIndex(out, "full output in ")+len("full output in "):])
	path = strings.TrimSuffix(strings.Fields(path)[0], "]")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("overflow file %s survives the run", path)
	}
}

func TestFetchRefusesPrivateAddressesWithoutProxy(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "hello") }))
	defer srv.Close()
	s, _ := newSet(t)
	s.Hosts = []string{"localhost"}
	u := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	if _, err := s.Call(context.Background(), call("fetch", map[string]any{"url": u})); err == nil || !strings.Contains(err.Error(), "not a public address") {
		t.Fatalf("want a private-address refusal, got %v", err)
	}
}
