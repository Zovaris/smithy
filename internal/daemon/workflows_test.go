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

const wfHello = `{"name":"hello","schedule":"0 17 * * *","steps":[{"id":"a","kind":"run","run":"echo hi"},{"id":"n","kind":"notify","title":"hi","message":"said {{a.output}}"}]}`

func workflowTest(t *testing.T) (*Server, string, *[]string) {
	t.Helper()
	sv, u, _ := sessionTest(t, okRun)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.json"), []byte(wfHello), 0o600)
	var ran []string
	sv.Workflows = sv.NewWorkflows(workflow.Store{Dir: dir})
	sv.Workflows.Env.Shell = func(_ context.Context, _, cmd string) (string, int, error) {
		ran = append(ran, cmd)
		return "hi", 0, nil
	}
	return sv, u, &ran
}

func TestWorkflowsListEnableRun(t *testing.T) {
	sv, u, ran := workflowTest(t)
	res := do(t, "GET", u+"/v1/workflows", "", auth)
	var l WorkflowList
	if err := json.NewDecoder(res.Body).Decode(&l); err != nil || len(l.Workflows) != 1 || l.Workflows[0].Enabled {
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
