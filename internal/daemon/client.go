//go:build harness

package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/models"
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
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
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

// Ask records a warden decision (decide:<batch>/<item>/<key>) on the daemon (POST /v1/asks).
func (c *Client) Ask(ctx context.Context, id, question string, options []string) error {
	return c.AskMulti(ctx, id, question, options, false)
}

// AskMulti is Ask for a question whose options may be picked together when multi is true.
func (c *Client) AskMulti(ctx context.Context, id, question string, options []string, multi bool) error {
	b, _ := json.Marshal(map[string]any{"id": id, "question": question, "options": options, "multi": multi})
	r, err := c.req(ctx, "POST", "/v1/asks", b)
	if err != nil {
		return err
	}
	res, err := c.http().Do(r)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("daemon asks: %s", res.Status)
	}
	return nil
}

// Decision is GET /v1/decisions/{id}; Outcome is nil until someone answers.
func (c *Client) Decision(ctx context.Context, id string) (Decision, error) {
	var d Decision
	r, err := c.req(ctx, "GET", "/v1/decisions/"+url.PathEscape(id), nil)
	if err != nil {
		return d, err
	}
	res, err := c.http().Do(r)
	if err != nil {
		return d, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return d, fmt.Errorf("daemon decision: %s", res.Status)
	}
	return d, json.NewDecoder(res.Body).Decode(&d)
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

func (c *Client) call(ctx context.Context, method, path string, body, out any) error {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r, err := c.req(ctx, method, path, b)
	if err != nil {
		return err
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http().Do(r)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		var e struct{ Error string }
		_ = json.NewDecoder(io.LimitReader(res.Body, 4<<10)).Decode(&e)
		if e.Error != "" {
			return fmt.Errorf("daemon %s: %s", path, e.Error)
		}
		return fmt.Errorf("daemon %s: %s", path, res.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// StartSession is POST /v1/sessions: agent "" is the last used one; msg may be empty.
func (c *Client) StartSession(ctx context.Context, agent, dir, msg string) (Session, error) {
	var s Session
	err := c.call(ctx, "POST", "/v1/sessions", map[string]string{"agent": agent, "dir": dir, "message": msg}, &s)
	return s, err
}

// SendSession is POST /v1/sessions/{id}/messages.
func (c *Client) SendSession(ctx context.Context, id, text string) error {
	return c.call(ctx, "POST", "/v1/sessions/"+url.PathEscape(id)+"/messages", map[string]string{"text": text}, nil)
}

// StopSession is POST /v1/sessions/{id}/stop.
func (c *Client) StopSession(ctx context.Context, id string) error {
	return c.call(ctx, "POST", "/v1/sessions/"+url.PathEscape(id)+"/stop", map[string]string{}, nil)
}

// SetSessionModel is PUT /v1/sessions/{id}/model: the model and effort its next turns run with.
func (c *Client) SetSessionModel(ctx context.Context, id, model, effort string) (Session, error) {
	var s Session
	err := c.call(ctx, "PUT", "/v1/sessions/"+url.PathEscape(id)+"/model", map[string]string{"model": model, "effort": effort}, &s)
	return s, err
}

// SetSessionGoal is PUT /v1/sessions/{id}/goal: the goal the daemon continues it toward ("" clears).
func (c *Client) SetSessionGoal(ctx context.Context, id, goal string) (Session, error) {
	var s Session
	err := c.call(ctx, "PUT", "/v1/sessions/"+url.PathEscape(id)+"/goal", map[string]string{"goal": goal}, &s)
	return s, err
}

// Sessions is GET /v1/sessions.
func (c *Client) Sessions(ctx context.Context) (SessionList, error) {
	var l SessionList
	err := c.call(ctx, "GET", "/v1/sessions", nil, &l)
	return l, err
}

// Setup is GET /v1/setup: detected agents, the default agent/model, the kit and skills/MCP.
func (c *Client) Setup(ctx context.Context) (Setup, error) {
	var s Setup
	err := c.call(ctx, "GET", "/v1/setup", nil, &s)
	return s, err
}

// Models is GET /v1/agents/{agent}/models: the models agent reports; refresh skips the day's cache.
func (c *Client) Models(ctx context.Context, agent string, refresh bool) (models.List, error) {
	var l models.List
	p := "/v1/agents/" + url.PathEscape(agent) + "/models"
	if refresh {
		p += "?refresh=1"
	}
	err := c.call(ctx, "GET", p, nil, &l)
	return l, err
}

// SetDefault is PUT /v1/setup/default: the agent and model new sessions start with.
func (c *Client) SetDefault(ctx context.Context, agent, model string) error {
	return c.call(ctx, "PUT", "/v1/setup/default", AgentChoice{Agent: agent, Model: model}, nil)
}

// SetKit is PUT /v1/setup/kit: what goes into every driven CLI.
func (c *Client) SetKit(ctx context.Context, k KitChoice) error {
	if k.Skills == nil {
		k.Skills = []string{}
	}
	return c.call(ctx, "PUT", "/v1/setup/kit", k, nil)
}

// InstallSkills is POST /v1/setup/skills/install; harness "" is every detected one.
func (c *Client) InstallSkills(ctx context.Context, harness string) (string, error) {
	var out struct{ Output string }
	err := c.call(ctx, "POST", "/v1/setup/skills/install", map[string]string{"harness": harness}, &out)
	return out.Output, err
}

// Projects is GET /v1/projects: the folders new sessions can start in, and the default.
func (c *Client) Projects(ctx context.Context) (ProjectList, error) {
	var l ProjectList
	err := c.call(ctx, "GET", "/v1/projects", nil, &l)
	return l, err
}

// AddProject is POST /v1/projects; def also makes dir the default.
func (c *Client) AddProject(ctx context.Context, dir string, def bool) (ProjectList, error) {
	var l ProjectList
	err := c.call(ctx, "POST", "/v1/projects", map[string]any{"dir": dir, "default": def}, &l)
	return l, err
}

// RemoveProject is DELETE /v1/projects.
func (c *Client) RemoveProject(ctx context.Context, dir string) (ProjectList, error) {
	var l ProjectList
	err := c.call(ctx, "DELETE", "/v1/projects", map[string]string{"dir": dir}, &l)
	return l, err
}

// Tasks is GET /v1/tasks: the task lists of the project at dir ("" is the default project).
func (c *Client) Tasks(ctx context.Context, dir string) (TaskBoard, error) {
	var b TaskBoard
	err := c.call(ctx, "GET", "/v1/tasks?project="+url.QueryEscape(dir), nil, &b)
	return b, err
}

// MoveTask is PUT /v1/tasks/status: it writes status to the task file's Status line.
func (c *Client) MoveTask(ctx context.Context, file, status string) (TaskCard, error) {
	var t TaskCard
	err := c.call(ctx, "PUT", "/v1/tasks/status", map[string]string{"file": file, "status": status}, &t)
	return t, err
}

// ListDirs is GET /v1/fs: the folders inside dir on the daemon's machine ("" is its home).
func (c *Client) ListDirs(ctx context.Context, dir string) (DirList, error) {
	var l DirList
	err := c.call(ctx, "GET", "/v1/fs?dir="+url.QueryEscape(dir), nil, &l)
	return l, err
}

// Settings is GET /v1/settings: every setting, the stored API keys (masked) and serve mode.
func (c *Client) Settings(ctx context.Context) (Settings, error) {
	var s Settings
	err := c.call(ctx, "GET", "/v1/settings", nil, &s)
	return s, err
}

// ConnectTest is POST /v1/connect/test: a test message on every chat connector's channels.
func (c *Client) ConnectTest(ctx context.Context) (ConnectTestResult, error) {
	var r ConnectTestResult
	err := c.call(ctx, "POST", "/v1/connect/test", map[string]string{}, &r)
	return r, err
}

// SetSetting is PUT /v1/settings/{key}: saves key in the user's file.
func (c *Client) SetSetting(ctx context.Context, key, value string) error {
	return c.call(ctx, "PUT", "/v1/settings/"+url.PathEscape(key), map[string]string{"value": value}, nil)
}

// UnsetSetting is DELETE /v1/settings/{key}: back to its default.
func (c *Client) UnsetSetting(ctx context.Context, key string) error {
	return c.call(ctx, "DELETE", "/v1/settings/"+url.PathEscape(key), map[string]string{}, nil)
}

// SetKey is PUT /v1/keys/{service}: stores service's API key in the auth file.
func (c *Client) SetKey(ctx context.Context, service, key string) error {
	return c.call(ctx, "PUT", "/v1/keys/"+url.PathEscape(service), map[string]string{"key": key}, nil)
}

// RemoveKey is DELETE /v1/keys/{service}.
func (c *Client) RemoveKey(ctx context.Context, service string) error {
	return c.call(ctx, "DELETE", "/v1/keys/"+url.PathEscape(service), map[string]string{}, nil)
}

// SetServe is PUT /v1/serve: off pauses serve (auto.paused true), on resumes it.
func (c *Client) SetServe(ctx context.Context, on bool) error {
	return c.call(ctx, "PUT", "/v1/serve", map[string]bool{"on": on}, nil)
}

// Doctor is GET /v1/doctor: smithy doctor's lines.
func (c *Client) Doctor(ctx context.Context) ([]string, error) {
	var out struct{ Lines []string }
	err := c.call(ctx, "GET", "/v1/doctor", nil, &out)
	return out.Lines, err
}
