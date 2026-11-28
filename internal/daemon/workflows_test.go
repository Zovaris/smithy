//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/workflow"
)

const wfHello = `{"name":"hello","schedule":"0 17 * * *","steps":[{"id":"a","kind":"run","run":"echo hi"},{"id":"n","kind":"notify","title":"hi","message":"said {{steps.a.output}}"}]}`

const wfHook = `{"name":"hook","webhook":true,"inputs":{"msg":""},"outputs":{"echo":"got {{inputs.msg}} / {{steps.q.answer}}"},"steps":[{"id":"q","kind":"ask","question":"ok {{inputs.msg}}?","options":["yes","no"]}]}`

func workflowTest(t *testing.T) (*Server, string, *[]string) {
	t.Helper()
	sv, u, _ := sessionTest(t, okRun)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.json"), []byte(wfHello), 0o600)
	os.WriteFile(filepath.Join(dir, "hook.json"), []byte(wfHook), 0o600)
	var ran []string
	sv.Workflows = sv.NewWorkflows(workflow.Store{Dir: dir})
	sv.Workflows.Env.Shell = func(_ context.Context, _, cmd string, _ []string) (string, int, error) {
		ran = append(ran, cmd)
		return "hi", 0, nil
	}
	return sv, u, &ran
}

func TestWorkflowsListEnableRun(t *testing.T) {
	sv, u, ran := workflowTest(t)
	res := do(t, "GET", u+"/v1/workflows", "", auth)
	var l WorkflowList
	if err := json.NewDecoder(res.Body).Decode(&l); err != nil || len(l.Workflows) != 2 || l.Workflows[0].Enabled {
		t.Fatalf("%d %v %+v", res.StatusCode, err, l)
	}
	if sv.WorkflowsScheduled() {
		t.Fatal("nothing enabled yet")
	}
	if res := do(t, "POST", u+"/v1/workflows/hello/enable", "{}", auth); res.StatusCode != 200 {
		t.Fatalf("enable: %d", res.StatusCode)
	}
	if !sv.WorkflowsScheduled() {
		t.Fatal("an enabled workflow must hold off the idle exit")
	}
	if res := do(t, "POST", u+"/v1/workflows/hello/run", "{}", auth); res.StatusCode != 202 {
		t.Fatalf("run: %d", res.StatusCode)
	}
	deadline := time.Now().Add(3 * time.Second)
	var d WorkflowDetail
	for time.Now().Before(deadline) {
		res := do(t, "GET", u+"/v1/workflows/hello", "", auth)
		json.NewDecoder(res.Body).Decode(&d)
		if len(d.History) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(*ran) != 1 || len(d.History) != 1 || d.History[0].Trigger != "web" || !d.Info.Enabled || !strings.Contains(d.Source, `"hello"`) {
		t.Fatalf("ran=%v detail=%+v", *ran, d)
	}
	evs, _ := sv.st.Since(0)
	var note store.Event
	for _, e := range evs {
		if e.Kind == KindWorkflowNote {
			note = e
		}
	}
	n, ok := NoteOf(note)
	if !ok || n.Kind != "workflow" || n.Title != "hi" || n.Text != "said hi" {
		t.Fatalf("note %+v %v", n, ok)
	}
}

func TestWorkflowsSaveAndGuards(t *testing.T) {
	_, u, _ := workflowTest(t)
	edited := strings.Replace(wfHello, "0 17", "30 18", 1)
	if res := do(t, "PUT", u+"/v1/workflows/hello", edited, auth); res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("save: %d %s", res.StatusCode, b)
	}
	if res := do(t, "PUT", u+"/v1/workflows/hello", `{"name":"other","schedule":"@daily","steps":[{"id":"a","kind":"run","run":"ls"}]}`, auth); res.StatusCode != 400 {
		t.Fatalf("rename via save: %d", res.StatusCode)
	}
	if res := do(t, "PUT", u+"/v1/workflows/hello", `{"name":"hello","schedule":"bad","steps":[]}`, auth); res.StatusCode != 400 {
		t.Fatalf("invalid: %d", res.StatusCode)
	}
	if res := do(t, "POST", u+"/v1/workflows/hello/run", "{}", map[string]string{"Authorization": auth["Authorization"], "Origin": "https://evil.example"}); res.StatusCode != 403 {
		t.Fatalf("cross-origin run: %d", res.StatusCode)
	}
	if res := do(t, "POST", u+"/v1/workflows/..%2Fx/enable", "{}", auth); res.StatusCode != 404 && res.StatusCode != 400 {
		t.Fatalf("bad name: %d", res.StatusCode)
	}
}

func TestWorkflowValidate(t *testing.T) {
	_, u, _ := workflowTest(t)
	for body, ok := range map[string]bool{wfHello: true, `{"name":"x","steps":[]}`: false} {
		res := do(t, "POST", u+"/v1/workflows/validate", body, auth)
		var v struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		json.NewDecoder(res.Body).Decode(&v)
		if v.OK != ok || (!ok && v.Error == "") {
			t.Errorf("%s: %+v", body, v)
		}
	}
}

func TestWorkflowHookAskReply(t *testing.T) {
	sv, u, _ := workflowTest(t)
	hook := func(tok, body string) (int, map[string]any) {
		res := do(t, "POST", u+"/v1/hooks/workflows/hook", body, map[string]string{"X-Smithy-Hook": tok})
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	if code, _ := hook("whatever", `{}`); code != 404 {
		t.Fatalf("disabled hook: %d", code)
	}
	sv.Workflows.Store.Enable("hook", time.Now())
	tok := sv.Workflows.Store.HookToken("hook")
	if code, _ := hook(tok+"x", `{}`); code != 404 {
		t.Fatalf("wrong token: %d", code)
	}
	type reply struct {
		code int
		out  map[string]any
	}
	got := make(chan reply, 1)
	go func() {
		c, o := hook(tok, `{"msg":"hi","extra":"dropped"}`)
		got <- reply{c, o}
	}()
	var id string
	for deadline := time.Now().Add(3 * time.Second); id == "" && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		sv.mu.Lock()
		for k, d := range sv.decisions {
			if strings.HasPrefix(k, "workflow:hook:") && strings.Contains(d.Question, "ok hi?") {
				id = k
			}
		}
		sv.mu.Unlock()
	}
	if id == "" {
		t.Fatal("the ask step never asked")
	}
	if _, err := sv.Answer(id, "yes", "test"); err != nil {
		t.Fatal(err)
	}
	r := <-got
	outs, _ := r.out["outputs"].(map[string]any)
	if r.code != 200 || r.out["status"] != "ok" || outs["echo"] != "got hi / yes" {
		t.Fatalf("%d %v", r.code, r.out)
	}
}
