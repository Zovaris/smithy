package connect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/text"
)

// TelegramAPI is the Bot API origin (a var for tests only, never a setting, so a config edit
// can't send the token elsewhere).
var TelegramAPI = "https://api.telegram.org"

// Telegram is the Bot API connector: one forum topic per session in Chat (a supergroup with
// topics on), inline-keyboard buttons for decisions, long polling for what comes back. Only
// updates from Chat and from a user id in Users count: with Users empty it only sends.
type Telegram struct {
	Base, Token, Chat string
	Users             map[string]bool
	// Poll is the getUpdates long-poll timeout (default 25 s).
	Poll   time.Duration
	Client *http.Client
	offset int64
}

func (t *Telegram) Name() string { return "telegram" }

func (t *Telegram) scrub(s string) string {
	if t.Token != "" {
		s = strings.ReplaceAll(s, t.Token, "<token>")
		if _, sec, ok := strings.Cut(t.Token, ":"); ok && sec != "" {
			s = strings.ReplaceAll(s, sec, "<token>")
		}
	}
	return text.Clean(s)
}

func (t *Telegram) call(ctx context.Context, method string, form url.Values, out any) error {
	base := t.Base
	if base == "" {
		base = TelegramAPI
	}
	cl := t.Client
	if cl == nil {
		cl = &http.Client{Timeout: t.poll() + 15*time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/bot"+t.Token+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("telegram: bad request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := cl.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("telegram %s: %s", method, t.scrub(err.Error()))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var r struct {
		OK     bool            `json:"ok"`
		Desc   string          `json:"description"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(b, &r) != nil || !r.OK {
		return fmt.Errorf("telegram %s: HTTP %d %s", method, resp.StatusCode, t.scrub(r.Desc))
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

func (t *Telegram) poll() time.Duration {
	if t.Poll > 0 {
		return t.Poll
	}
	return 25 * time.Second
}

// Thread creates a forum topic named after the session.
func (t *Telegram) Thread(ctx context.Context, session, title string) (string, error) {
	var r struct {
		ID int64 `json:"message_thread_id"`
	}
	name := []rune(text.Clean(title))
	if len(name) > 120 {
		name = name[:120]
	}
	if len(name) == 0 {
		name = []rune("smithy")
	}
	if err := t.call(ctx, "createForumTopic", url.Values{"chat_id": {t.Chat}, "name": {string(name)}}, &r); err != nil {
		return "", err
	}
	return strconv.FormatInt(r.ID, 10), nil
}

var tgEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

var tgTag = map[string]string{"pr_ready": "READY", "blocked": "BLOCKED", "stuck": "STUCK", "decision": "DECIDE",
	"orchestrator_failed": "FAILED", "serve_down": "STOPPED", "cap_reached": "CAP", "limited": "LIMITED", "pr_merged": "MERGED", "ci_red": "CI RED"}

func render(n Note) string {
	head := "<b>" + tgEsc(clip(text.Clean(n.Title), 200)) + "</b>"
	if tag := tgTag[n.Kind]; tag != "" {
		head = "<code>" + tag + "</code>  " + head
	}
	lines := []string{head}
	for _, l := range strings.Split(n.Text, "\n") {
		if l = strings.TrimSpace(text.Clean(l)); l != "" {
			lines = append(lines, tgEsc(clip(l, 500)))
		}
	}
	if u, err := url.Parse(n.URL); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
		lines = append(lines, `<a href="`+tgEsc(strings.ReplaceAll(u.String(), `"`, "%22"))+`">`+tgEsc(clip(u.String(), 120))+"</a>")
	}
	return strings.Join(lines, "\n")
}

// Send posts n in thread ("" is the chat's main topic) with one button per option.
func (t *Telegram) Send(ctx context.Context, thread string, n Note, buttons []Button) (string, error) {
	form := url.Values{"chat_id": {t.Chat}, "text": {render(n)}, "parse_mode": {"HTML"}, "link_preview_options": {`{"is_disabled":true}`}}
	if thread != "" {
		form.Set("message_thread_id", thread)
	}
	if len(buttons) > 0 {
		type kb struct {
			Text string `json:"text"`
			Data string `json:"callback_data"`
		}
		var rows [][]kb
		for _, b := range buttons {
			rows = append(rows, []kb{{Text: clip(text.Clean(b.Label), 60), Data: b.Data}})
		}
		mk, _ := json.Marshal(map[string]any{"inline_keyboard": rows})
		form.Set("reply_markup", string(mk))
	}
	var r struct {
		ID int64 `json:"message_id"`
	}
	if err := t.call(ctx, "sendMessage", form, &r); err != nil {
		return "", err
	}
	return strconv.FormatInt(r.ID, 10), nil
}

// Update replaces the text of message ref and removes its buttons.
func (t *Telegram) Update(ctx context.Context, ref string, n Note) error {
	form := url.Values{"chat_id": {t.Chat}, "message_id": {ref}, "text": {render(n)}, "parse_mode": {"HTML"},
		"link_preview_options": {`{"is_disabled":true}`}}
	return t.call(ctx, "editMessageText", form, nil)
}

type tgUser struct {
	ID    int64 `json:"id"`
	IsBot bool  `json:"is_bot"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

type tgMessage struct {
	ID      int64  `json:"message_id"`
	Thread  int64  `json:"message_thread_id"`
	IsTopic bool   `json:"is_topic_message"`
	From    tgUser `json:"from"`
	Chat    tgChat `json:"chat"`
	Text    string `json:"text"`
}

type tgUpdate struct {
	ID       int64      `json:"update_id"`
	Message  *tgMessage `json:"message"`
	Callback *struct {
		ID      string     `json:"id"`
		From    tgUser     `json:"from"`
		Data    string     `json:"data"`
		Message *tgMessage `json:"message"`
	} `json:"callback_query"`
}

func (t *Telegram) allowed(chat int64, u tgUser) bool {
	if u.IsBot || strconv.FormatInt(chat, 10) != t.Chat {
		return false
	}
	return t.Users[strconv.FormatInt(u.ID, 10)]
}

// Receive long-polls getUpdates once: topic messages and button presses from Chat (and Users).
// Every button press is acknowledged so the client stops its spinner, kept or not.
func (t *Telegram) Receive(ctx context.Context) ([]Inbound, error) {
	form := url.Values{"timeout": {strconv.Itoa(int(t.poll().Seconds()))}, "allowed_updates": {`["message","callback_query"]`}}
	if t.offset > 0 {
		form.Set("offset", strconv.FormatInt(t.offset, 10))
	}
	var ups []tgUpdate
	if err := t.call(ctx, "getUpdates", form, &ups); err != nil {
		return nil, err
	}
	var out []Inbound
	for _, u := range ups {
		if u.ID >= t.offset {
			t.offset = u.ID + 1
		}
		switch {
		case u.Callback != nil:
			_ = t.call(ctx, "answerCallbackQuery", url.Values{"callback_query_id": {u.Callback.ID}}, nil)
			if m := u.Callback.Message; m != nil && t.allowed(m.Chat.ID, u.Callback.From) && len(u.Callback.Data) <= 64 {
				out = append(out, Inbound{Button: u.Callback.Data, Ref: strconv.FormatInt(m.ID, 10), By: strconv.FormatInt(u.Callback.From.ID, 10)})
			}
		case u.Message != nil:
			m := u.Message
			if m.IsTopic && m.Thread != 0 && m.Text != "" && t.allowed(m.Chat.ID, m.From) {
				out = append(out, Inbound{Thread: strconv.FormatInt(m.Thread, 10), Text: clip(m.Text, 4000), By: strconv.FormatInt(m.From.ID, 10)})
			}
		}
	}
	return out, nil
}
