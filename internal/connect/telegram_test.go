package connect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type botAPI struct {
	mu      sync.Mutex
	calls   []string
	forms   []map[string]string
	updates string
	fail    string
}

func (b *botAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r.ParseForm()
	m := strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/"):], "/")
	f := map[string]string{}
	for k := range r.PostForm {
		f[k] = r.PostForm.Get(k)
	}
	b.calls = append(b.calls, m)
	b.forms = append(b.forms, f)
	if m == b.fail {
		w.Write([]byte(`{"ok":false,"description":"Bad Request: the chat is not a forum, token 123:` + "SECRETpart" + `"}`))
		return
	}
	switch m {
	case "createForumTopic":
		w.Write([]byte(`{"ok":true,"result":{"message_thread_id":77,"name":"x"}}`))
	case "sendMessage":
		w.Write([]byte(`{"ok":true,"result":{"message_id":9}}`))
	case "getUpdates":
		u := b.updates
		b.updates = "[]"
		w.Write([]byte(`{"ok":true,"result":` + u + `}`))
	default:
		w.Write([]byte(`{"ok":true,"result":true}`))
	}
}

func tg(t *testing.T, api *botAPI) *Telegram {
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	return &Telegram{Base: srv.URL, Token: "123:" + "SECRETpart", Chat: "-100500"}
}

func TestTelegramOffsetSurvivesRestart(t *testing.T) {
	api := &botAPI{updates: `[{"update_id":41,"message":{"message_id":1,"chat":{"id":-100500},"from":{"id":5},"text":"hi"}}]`}
	c := tg(t, api)
	c.OffsetFile = filepath.Join(t.TempDir(), "telegram.offset")
	if _, err := c.Receive(context.Background()); err != nil {
		t.Fatal(err)
	}
	c2 := tg(t, api)
	c2.OffsetFile = c.OffsetFile
	if _, err := c2.Receive(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f := api.forms[len(api.forms)-1]; f["offset"] != "42" {
		t.Fatalf("restarted connector asked from offset %q, want 42", f["offset"])
	}
}

func TestTelegramWaitsOutRetryAfter(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n++; n == 1 {
			w.WriteHeader(429)
			w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1}}`))
			return
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":9}}`))
	}))
	t.Cleanup(srv.Close)
	c := &Telegram{Base: srv.URL, Token: "123:x", Chat: "-1"}
	start := time.Now()
	if _, err := c.Send(context.Background(), "", Note{Kind: "blocked", Title: "x"}, nil); err != nil || n != 2 || time.Since(start) < time.Second {
		t.Fatalf("429 not waited out: err %v calls %d after %v", err, n, time.Since(start))
	}
}

func TestSlackAndDiscordWaitOutRetryAfter(t *testing.T) {
	for _, name := range []string{"slack", "discord"} {
		n := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if n++; n == 1 {
				if name == "slack" {
					w.Header().Set("Retry-After", "1")
					w.WriteHeader(429)
					w.Write([]byte(`{"ok":false,"error":"ratelimited"}`))
				} else {
					w.WriteHeader(429)
					w.Write([]byte(`{"message":"You are being rate limited.","retry_after":1.0,"global":false}`))
				}
				return
			}
			w.Write([]byte(`{"ok":true,"ts":"1.2","id":"900"}`))
		}))
		var c Connector = &Slack{Base: srv.URL, Token: "xoxb-1-x", Channel: "C123456"}
		if name == "discord" {
			c = &Discord{API: srv.URL, Token: "x", Channel: "500"}
		}
		start := time.Now()
		_, err := c.Send(context.Background(), "", Note{Kind: "blocked", Title: "x"}, nil)
		srv.Close()
		if err != nil || n != 2 || time.Since(start) < time.Second {
			t.Errorf("%s: 429 not waited out: err %v calls %d after %v", name, err, n, time.Since(start))
		}
	}
}

func TestTelegramThreadSendUpdate(t *testing.T) {
	api := &botAPI{}
	c := tg(t, api)
	ctx := context.Background()
	th, err := c.Thread(ctx, "batch/v1", "batch/v1")
	if err != nil || th != "77" {
		t.Fatalf("thread %q %v", th, err)
	}
	ref, err := c.Send(ctx, th, Note{Kind: "decision", Title: "Pick <one>", Text: "a & b", URL: "https://github.com/o/r/pull/1"},
		[]Button{{Label: "yes", Data: "abc:0"}})
	if err != nil || ref != "9" {
		t.Fatalf("send %q %v", ref, err)
	}
	f := api.forms[1]
	if f["message_thread_id"] != "77" || f["chat_id"] != "-100500" || f["parse_mode"] != "HTML" {
		t.Fatalf("form %v", f)
	}
	if !strings.Contains(f["text"], "Pick &lt;one&gt;") || !strings.Contains(f["text"], "a &amp; b") {
		t.Fatalf("text not escaped: %q", f["text"])
	}
	var kb struct {
		IK [][]struct {
			Text         string `json:"text"`
			CallbackData string `json:"callback_data"`
		} `json:"inline_keyboard"`
	}
	if json.Unmarshal([]byte(f["reply_markup"]), &kb) != nil || len(kb.IK) != 1 || kb.IK[0][0].CallbackData != "abc:0" {
		t.Fatalf("keyboard %q", f["reply_markup"])
	}
	if err := c.Update(ctx, ref, Note{Title: "done"}); err != nil || api.calls[2] != "editMessageText" || api.forms[2]["message_id"] != "9" {
		t.Fatalf("update %v %v", api.calls, err)
	}
}

func TestTelegramTogglesMultiSelect(t *testing.T) {
	api := &botAPI{}
	c := tg(t, api)
	c.Users = map[string]bool{"5": true}
	h := hub(t, c)
	var answers []string
	h.Answer = func(id, ans, by string) error { answers = append(answers, ans+"@"+by); return nil }
	ctx := context.Background()
	h.Notify(ctx, Note{Key: "d", Kind: "decision", Title: "Which?", Options: []string{"a", "b"}, Decision: "ask:1", Multi: true})
	hd := handle("ask:1")
	if mk := api.forms[0]["reply_markup"]; !strings.Contains(mk, "☐ a") || !strings.Contains(mk, `"`+hd+`:done"`) {
		t.Fatalf("keyboard %q", mk)
	}
	api.updates = `[{"update_id":1,"callback_query":{"id":"c1","from":{"id":5},"data":"` + hd + `:1","message":{"message_id":9,"chat":{"id":-100500}}}},
 {"update_id":2,"callback_query":{"id":"c2","from":{"id":8},"data":"` + hd + `:0","message":{"message_id":9,"chat":{"id":-100500}}}}]`
	ins, _ := c.Receive(ctx)
	for _, in := range ins {
		h.Handle(ctx, c, in)
	}
	i := len(api.calls) - 1
	if api.calls[i] != "editMessageReplyMarkup" || api.forms[i]["message_id"] != "9" || !strings.Contains(api.forms[i]["reply_markup"], "☑ b") || !strings.Contains(api.forms[i]["reply_markup"], "☐ a") {
		t.Fatalf("toggle: %v %v", api.calls, api.forms[i])
	}
	api.updates = `[{"update_id":3,"callback_query":{"id":"c3","from":{"id":5},"data":"` + hd + `:done","message":{"message_id":9,"chat":{"id":-100500}}}}]`
	ins, _ = c.Receive(ctx)
	for _, in := range ins {
		h.Handle(ctx, c, in)
	}
	if len(answers) != 1 || answers[0] != "b@telegram:5" {
		t.Fatalf("answers %q", answers)
	}
}

func TestTelegramErrorsNeverCarryTheToken(t *testing.T) {
	api := &botAPI{fail: "createForumTopic"}
	c := tg(t, api)
	_, err := c.Thread(context.Background(), "s", "s")
	if err == nil || strings.Contains(err.Error(), "SECRETpart") {
		t.Fatalf("err %v", err)
	}
}

func TestTelegramReceiveFiltersChatAndUsers(t *testing.T) {
	api := &botAPI{updates: `[
 {"update_id":1,"message":{"message_id":1,"message_thread_id":77,"is_topic_message":true,"from":{"id":5,"is_bot":false},"chat":{"id":-100500},"text":"go on"}},
 {"update_id":2,"message":{"message_id":2,"message_thread_id":77,"is_topic_message":true,"from":{"id":5},"chat":{"id":-999},"text":"other chat"}},
 {"update_id":3,"message":{"message_id":3,"message_thread_id":77,"is_topic_message":true,"from":{"id":6,"is_bot":true},"chat":{"id":-100500},"text":"a bot"}},
 {"update_id":4,"callback_query":{"id":"cb","from":{"id":5},"data":"abc:1","message":{"message_id":9,"chat":{"id":-100500}}}},
 {"update_id":5,"callback_query":{"id":"cb2","from":{"id":8},"data":"abc:0","message":{"message_id":9,"chat":{"id":-100500}}}},
 {"update_id":6,"message":{"message_id":4,"from":{"id":5},"chat":{"id":-100500},"text":"main chat"}}
]`}
	c := tg(t, api)
	c.Users = map[string]bool{"5": true}
	ins, err := c.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) != 3 || ins[0] != (Inbound{Thread: "77", Text: "go on", By: "5"}) || ins[1] != (Inbound{Button: "abc:1", Ref: "9", By: "5"}) || ins[2] != (Inbound{Text: "main chat", By: "5"}) {
		t.Fatalf("got %+v", ins)
	}
	if c.offset != 7 {
		t.Fatalf("offset %d", c.offset)
	}
	ins, _ = c.Receive(context.Background())
	if len(ins) != 0 || api.forms[len(api.forms)-1]["offset"] != "7" {
		t.Fatalf("second poll %+v %v", ins, api.forms[len(api.forms)-1])
	}
	acked := 0
	for _, m := range api.calls {
		if m == "answerCallbackQuery" {
			acked++
		}
	}
	if acked != 2 {
		t.Fatalf("every callback is acknowledged, got %d", acked)
	}
}

func TestTelegramHearsNobodyWithoutUsers(t *testing.T) {
	api := &botAPI{updates: `[
 {"update_id":1,"message":{"message_id":1,"message_thread_id":77,"is_topic_message":true,"from":{"id":5},"chat":{"id":-100500},"text":"go on"}},
 {"update_id":2,"callback_query":{"id":"cb","from":{"id":5},"data":"abc:1","message":{"message_id":9,"chat":{"id":-100500}}}}
]`}
	c := tg(t, api)
	ins, err := c.Receive(context.Background())
	if err != nil || len(ins) != 0 {
		t.Fatalf("no connect.telegram.users must accept nothing: %+v %v", ins, err)
	}
}
