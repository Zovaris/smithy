package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCodeStepGetsStepsInputsVarsAndFeedsLaterSteps(t *testing.T) {
	w := parse(t, Examples["stale-prs"])
	f := &fake{shell: map[string]shellOut{}}
	env := f.env()
	env.Shell = func(context.Context, string, string, []string) (string, int, error) {
		return `[{"number":7,"title":"old","updatedAt":"2020-01-01T00:00:00Z"}]`, 0, nil
	}
	var got map[string]any
	env.Code = func(_ context.Context, code, input string) (string, error) {
		if !strings.Contains(code, "steps.prs.json") {
			t.Errorf("code = %q", code)
		}
		if err := json.Unmarshal([]byte(input), &got); err != nil {
			t.Fatal(err)
		}
		return `[{"number":7,"title":"old"}]`, nil
	}
	var sent string
	env.Notify = func(_, _, text string) error { sent = text; return nil }
	r := RunWith(context.Background(), w, env, Invocation{Trigger: "cli", Inputs: map[string]string{"days": "14"}})
	if r.Status != StatusOK {
		t.Fatalf("%s %s %+v", r.Status, r.Error, r.Steps)
	}
	prs := got["steps"].(map[string]any)["prs"].(map[string]any)
	if prs["exit"].(float64) != 0 || !strings.Contains(prs["output"].(string), `"number":7`) || prs["json"].([]any)[0].(map[string]any)["number"].(float64) != 7 {
		t.Errorf("prs = %v", prs)
	}
	if got["inputs"].(map[string]any)["days"] != "14" || got["vars"] == nil {
		t.Errorf("input = %v", got)
	}
	if r.Steps[1].Kind != KindCode || r.Steps[1].Output != `[{"number":7,"title":"old"}]` || !strings.Contains(sent, `"number":7`) {
		t.Errorf("steps %+v sent %q", r.Steps, sent)
	}
}

func TestCodeStepErrorsAndValidation(t *testing.T) {
	src := `{"name":"c","steps":[{"id":"a","kind":"code","code":"throw 1","on_error":"continue"}]}`
	w := parse(t, src)
	f := &fake{shell: map[string]shellOut{}}
	env := f.env()
	env.Code = func(context.Context, string, string) (string, error) { return "", errors.New("boom (line 1)") }
	r := RunWith(context.Background(), w, env, Invocation{})
	if r.Steps[0].Error != "boom (line 1)" || r.Steps[0].Input != "throw 1" {
		t.Errorf("%+v", r.Steps[0])
	}
	env.Code = nil
	r = RunWith(context.Background(), w, env, Invocation{})
	if !strings.Contains(r.Steps[0].Error, "code steps") {
		t.Errorf("%+v", r.Steps[0])
	}
	for _, bad := range []string{
		`{"name":"c","steps":[{"id":"a","kind":"code"}]}`,
		`{"name":"c","steps":[{"id":"a","kind":"code","code":"return '{{inputs.x}}'"}]}`,
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%s parsed", bad)
		}
	}
	if s := w.Steps[0].Summary(); s != "throw 1" {
		t.Errorf("summary %q", s)
	}
}
