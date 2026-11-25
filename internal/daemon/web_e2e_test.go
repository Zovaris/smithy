//go:build harness

package daemon

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/web"
)

// TestWebScreensServed walks what a browser does on the served UI: the shell, every first-load
// asset, each lazy screen chunk, the event stream the screens fold, and an answer from the
// decisions screen.
func TestWebScreensServed(t *testing.T) {
	if !web.Built() {
		if os.Getenv("SMITHY_REQUIRE_WEB_BUILD") != "" {
			t.Fatal("SMITHY_REQUIRE_WEB_BUILD is set but the web UI is the placeholder: build it (npm ci && npm run build in web/) before the harness tests")
		}
		t.Skip("only the placeholder web UI is embedded: run npm ci && npm run build in web/ to test the screens")
	}
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	body := func(url string) string {
		t.Helper()
		res := do(t, "GET", url, "", nil)
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 {
			t.Fatalf("GET %s: %d", url, res.StatusCode)
		}
		return string(b)
	}
	html := body(hs.URL + "/")
	entry := regexp.MustCompile(`src="/(assets/index-[^"]+\.js)"`).FindStringSubmatch(html)
	if entry == nil {
		t.Fatalf("no entry script in %q", html)
	}
	js := body(hs.URL + "/" + entry[1])
	// The conversation is the shell itself (first load); every other page and the palette is a
	// lazy chunk the entry imports.
	for _, screen := range []string{"decisions", "agents", "board", "bench", "setup", "palette"} {
		// Vite names a chunk "assets/x-hash.js" in its preload list and "./x-hash.js" in the import.
		m := regexp.MustCompile(`(?:assets/|\./)(` + screen + `-[A-Za-z0-9_-]+\.js)`).FindStringSubmatch(js)
		if m == nil {
			t.Errorf("entry does not import the %s chunk", screen)
			continue
		}
		body(hs.URL + "/assets/" + m[1])
	}

	if err := sv.Ask("d1", "Merge #142 into release/v0.17?", []string{"merge", "later"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sv.Append("tool", "TK-6", map[string]any{"name": "Read", "status": "ok"}); err != nil {
		t.Fatal(err)
	}
	auth := map[string]string{"Authorization": "Bearer " + testToken, "Last-Event-ID": "0"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", hs.URL+"/v1/events", nil)
	for k, v := range auth {
		r.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(res.Body)
	seen := map[string]bool{}
	for sc.Scan() && !(seen["decision_asked"] && seen["tool"]) {
		if k, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
			seen[k] = true
		}
	}
	cancel()
	if !seen["decision_asked"] || !seen["tool"] {
		t.Fatalf("stream events: %v", seen)
	}

	if res := do(t, "POST", hs.URL+"/v1/decisions", `{"id":"d1","answer":"merge"}`, auth); res.StatusCode != 200 {
		t.Fatalf("answer: %d", res.StatusCode)
	}
	if p := sv.Pending(); len(p) != 0 {
		t.Errorf("still pending: %v", p)
	}
}
