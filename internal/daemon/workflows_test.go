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

const wfHello = `{"name":"hello","steps":[{"id":"a","kind":"run","run":"echo hi"},{"id":"n","kind":"notify","title":"hi","message":"said {{steps.a.output}}"}]}`

const wfAsk = `{"name":"askme","inputs":{"msg":""},"outputs":{"echo":"got {{inputs.msg}} / {{steps.q.answer}}"},"steps":[{"id":"q","kind":"ask","question":"ok {{inputs.msg}}?","options":["yes","no"]}]}`

func workflowTest(t *testing.T) (*Server, string, *[]string) {
	t.Helper()
	sv, u, _ := sessionTest(t, okRun)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.json"), []byte(wfHello), 0o600)
	os.WriteFile(filepath.Join(dir, "askme.json"), []byte(wfAsk), 0o600)
	var ran []string
	sv.Workflows = sv.NewWorkflows(workflow.Store{Dir: dir})
	sv.Workflows.Env.Shell = func(_ context.Context, _, cmd string, _ []string) (string, int, error) {
		ran = append(ran, cmd)
		return "hi", 0, nil
	}
	return sv, u, &ran
}

func TestWorkflowsRunAndNote(t *testing.T) {
	sv, u, ran := workflowTest(t)
	res := do(t, "GET", u+"/v1/workflows", "", auth)
	var l WorkflowList
	if err := json.NewDecoder(res.Body).Decode(&l); err != nil || len(l.Workflows) != 2 {
		t.Fatalf("%d %v %+v", res.StatusCode, err, l)
	}
	if res := do(t, "POST", u+"/v1/workflows/hello/run", `{"inputs":{}}`, auth); res.StatusCode != 202 {
		t.Fatalf("run: %d", res.StatusCode)
	}
	var d WorkflowDetail
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline) && len(d.History) == 0; time.Sleep(20 * time.Millisecond) {
		json.NewDecoder(do(t, "GET", u+"/v1/workflows/hello", "", auth).Body).Decode(&d)
	}
	if len(*ran) != 1 || len(d.History) != 1 || d.History[0].Trigger != "web" || !strings.Contains(d.Source, `"hello"`) {
		t.Fatalf("ran=%v detail=%+v", *ran, d)
	}
	evs, _ := sv.st.Since(0)
	var note store.Event
	for _, e := range evs {
		if e.Kind == KindWorkflowNote {
			note = e
		}
	}
	if n, ok := NoteOf(note); !ok || n.Kind != "workflow" || n.Text != "said hi" {
		t.Fatalf("note %+v %v", n, ok)
	}
}

func TestWorkflowSaveValidateGuards(t *testing.T) {
	_, u, _ := workflowTest(t)
	if res := do(t, "PUT", u+"/v1/workflows/hello", strings.Replace(wfHello, "echo hi", "echo yo", 1), auth); res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("save: %d %s", res.StatusCode, b)
	}
	if res := do(t, "PUT", u+"/v1/workflows/hello", `{"name":"other","steps":[{"id":"a","kind":"run","run":"ls"}]}`, auth); res.StatusCode != 400 {
		t.Fatalf("rename: %d", res.StatusCode)
	}
	if res := do(t, "POST", u+"/v1/workflows/hello/run", "{}", map[string]string{"Authorization": auth["Authorization"], "Origin": "https://evil.example"}); res.StatusCode != 403 {
		t.Fatalf("cross-origin run: %d", res.StatusCode)
	}
	for body, ok := range map[string]bool{wfHello: true, `{"name":"x","steps":[]}`: false} {
		var v struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		json.NewDecoder(do(t, "POST", u+"/v1/workflows/validate", body, auth).Body).Decode(&v)
		if v.OK != ok || (!ok && v.Error == "") {
			t.Errorf("%s: %+v", body, v)
		}
	}
}

func TestSchedulesAPI(t *testing.T) {
	sv, u, _ := workflowTest(t)
	if res := do(t, "POST", u+"/v1/schedules", `{"name":"daily","workflow":"hello","cron":"0 9 * * *"}`, auth); res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("add: %d %s", res.StatusCode, b)
	}
	if res := do(t, "POST", u+"/v1/schedules", `{"name":"bad","workflow":"nope","cron":"0 9 * * *"}`, auth); res.StatusCode != 400 {
		t.Fatalf("schedule for a missing workflow: %d", res.StatusCode)
	}
	if sv.WorkflowsScheduled() {
		t.Fatal("nothing enabled yet")
	}
	if res := do(t, "POST", u+"/v1/schedules/daily/enable", "{}", auth); res.StatusCode != 200 {
		t.Fatalf("enable: %d", res.StatusCode)
	}
	var l ScheduleList
	json.NewDecoder(do(t, "GET", u+"/v1/schedules", "", auth).Body).Decode(&l)
	if len(l.Schedules) != 1 || !l.Schedules[0].Enabled || l.Schedules[0].Next == nil || !sv.WorkflowsScheduled() {
		t.Fatalf("%+v", l)
	}
	if res := do(t, "DELETE", u+"/v1/schedules/daily", "", auth); res.StatusCode != 200 {
		t.Fatalf("remove: %d", res.StatusCode)
	}
}

func TestScheduleHookAskReply(t *testing.T) {
	sv, u, _ := workflowTest(t)
	sv.Workflows.Store.SaveSchedule(workflow.Schedule{Name: "hook", Workflow: "askme", Webhook: true})
	hook := func(tok, body string) (int, map[string]any) {
		res := do(t, "POST", u+"/v1/hooks/schedules/hook", body, map[string]string{"X-Smithy-Hook": tok})
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	if code, _ := hook("whatever", `{}`); code != 404 {
		t.Fatalf("disabled hook: %d", code)
	}
	sv.Workflows.Store.EnableSchedule("hook", time.Now())
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
			if strings.HasPrefix(k, "workflow:askme:") && strings.Contains(d.Question, "ok hi?") {
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

func TestWorkflowSessionStep(t *testing.T) {
	sv, _, _ := workflowTest(t)
	made := []string{}
	sv.Runners = func(agent, _, dir, _ string) (Runner, error) {
		made = append(made, agent)
		return &fakeRunner{fn: okRun}, nil
	}
	os.WriteFile(filepath.Join(sv.Workflows.Store.Dir, "chat.json"), []byte(`{"name":"chat","dir":"`+t.TempDir()+`","allow_write":true,"steps":[{"id":"s","kind":"session","prompt":"hello from a workflow"}]}`), 0o600)
	r, err := sv.Workflows.Start(context.Background(), "chat", workflow.Invocation{Trigger: "cli"})
	if err != nil || r.Status != workflow.StatusOK || r.Steps[0].Output == "" || len(made) != 1 {
		t.Fatalf("%v %+v made=%v", err, r, made)
	}
}
