//go:build harness

package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/store"
)

// Client talks to a daemon's API. Token may be empty behind tailscale serve.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
	Retry time.Duration // first reconnect wait (default 1s), doubling to 30s
}

// ClientMsg is one thing Follow reports: an event, a (re)connect, or a lost stream.
type ClientMsg struct {
	Event     *store.Event
	Connected bool
	Err       error
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) req(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		r.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return r, nil
}

// State is GET /v1/state.
func (c *Client) State(ctx context.Context) (State, error) {
	var st State
	r, err := c.req(ctx, "GET", "/v1/state", nil)
	if err != nil {
		return st, err
	}
	res, err := c.http().Do(r)
	if err != nil {
		return st, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return st, fmt.Errorf("daemon state: %s", res.Status)
	}
	return st, json.NewDecoder(res.Body).Decode(&st)
}

// Decide posts an answer; the outcome is the recorded one (Won only for the answer that won).
func (c *Client) Decide(ctx context.Context, id, answer string) (Outcome, error) {
	var o Outcome
	b, _ := json.Marshal(map[string]string{"id": id, "answer": answer})
	r, err := c.req(ctx, "POST", "/v1/decisions", b)
	if err != nil {
		return o, err
	}
	r.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(r)
	if err != nil {
		return o, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 409 {
		return o, fmt.Errorf("daemon decisions: %s", res.Status)
	}
	return o, json.NewDecoder(res.Body).Decode(&o)
}

// Follow streams every event after since to out until ctx ends, reconnecting with backoff and
// resuming from the last seq it delivered, so a daemon restart neither drops nor repeats events.
func (c *Client) Follow(ctx context.Context, since int64, out chan<- ClientMsg) {
	wait := c.Retry
	if wait <= 0 {
		wait = time.Second
	}
	first := wait
	last := since
	for ctx.Err() == nil {
		got, err := c.stream(ctx, &last, out)
		if ctx.Err() != nil {
			return
		}
		if got {
			wait = first
		}
		if err == nil {
			err = fmt.Errorf("daemon closed the stream")
		}
		select {
		case out <- ClientMsg{Err: err}:
		case <-ctx.Done():
			return
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
		wait = min(wait*2, 30*time.Second)
	}
}

// stream reads one SSE connection; connected reports whether it got a 200.
func (c *Client) stream(ctx context.Context, last *int64, out chan<- ClientMsg) (connected bool, err error) {
	r, err := c.req(ctx, "GET", "/v1/events?since="+strconv.FormatInt(*last, 10), nil)
	if err != nil {
		return false, err
	}
	r.Header.Set("Accept", "text/event-stream")
	r.Header.Set("Last-Event-ID", strconv.FormatInt(*last, 10))
	res, err := c.http().Do(r)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return false, fmt.Errorf("daemon events: %s", res.Status)
	}
	select {
	case out <- ClientMsg{Connected: true}:
	case <-ctx.Done():
		return true, nil
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var e store.Event
		if json.Unmarshal([]byte(data), &e) != nil || e.Seq <= *last {
			continue
		}
		*last = e.Seq
		select {
		case out <- ClientMsg{Event: &e}:
		case <-ctx.Done():
			return true, nil
		}
	}
	return true, sc.Err()
}
