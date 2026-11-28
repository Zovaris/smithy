package workflow

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type shellOut struct {
	out  string
	exit int
}

type fake struct {
	mu      sync.Mutex
	shell   map[string]shellOut
	fails   map[string]int
	answers []string
	cost    float64
	prompts []string
	notes   []string
	ran     []string
	envs    [][]string
	block   string
	asked   []string
	reply   string
	flows   map[string]Workflow
	enabled map[string]bool
	getenv  map[string]string
}

func (f *fake) env() Env {
	return Env{
		Shell: func(_ context.Context, _ string, cmd string, env []string) (string, int, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.ran = append(f.ran, cmd)
			f.envs = append(f.envs, env)
			if f.fails[cmd] > 0 {
				f.fails[cmd]--
				return "flaky", 1, nil
			}
			r := f.shell[cmd]
			return r.out, r.exit, nil
		},
		Agent: func(_ context.Context, a AgentCall) (string, float64, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.prompts = append(f.prompts, a.Prompt)
			if len(f.answers) == 0 {
				return "", 0, errors.New("no answer")
			}
			ans := f.answers[0]
			f.answers = f.answers[1:]
			return ans, f.cost, nil
		},
		Notify: func(_ string, title, text string) error {
			f.mu.Lock()
			f.notes = append(f.notes, title+": "+text)
			f.mu.Unlock()
			return nil
		},
		Ask: func(_ context.Context, id, q string, _ []string) (string, error) {
			f.asked = append(f.asked, id+"|"+q)
			return f.reply, nil
		},
		Load: func(name string) (Workflow, bool, error) {
			w, ok := f.flows[name]
			if !ok {
				return w, false, errors.New("no workflow " + name)
			}
			return w, f.enabled[name], nil
		},
		Guard: func(cmd string) string {
			if f.block != "" && strings.Contains(cmd, f.block) {
				return f.block
			}
			return ""
		},
		Getenv: func(k string) string { return f.getenv[k] },
	}
}

func parse(t *testing.T, s string) Workflow {
	t.Helper()
	w, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestExamplesParse(t *testing.T) {
	if len(ExampleNames()) != 3 {
		t.Fatalf("want 3 examples: %v", ExampleNames())
	}
	for _, n := range ExampleNames() {
		if w := parse(t, Examples[n]); w.Name != n {
			t.Errorf("%s names itself %s", n, w.Name)
		}
	}
}

func TestDependabotYes(t *testing.T) {
	w := parse(t, Examples["dependabot-daily"])
	f := &fake{shell: map[string]shellOut{w.Steps[0].Run: {"high lodash: proto pollution\n", 0}}, answers: []string{"Yes. Patch bump, CI green."}}
	r := Run(context.Background(), w, f.env())
	if r.Status != StatusOK {
		t.Fatalf("%s: %s", r.Status, r.Error)
	}
	if !strings.Contains(f.prompts[0], `<data name="steps.alerts.output">high lodash`) || !strings.Contains(f.prompts[0], "yes or no") {
		t.Fatalf("prompt: %q", f.prompts[0])
	}
	if len(f.notes) != 1 || f.notes[0] != "Dependabot: merge? yes: Patch bump, CI green.\n\nOpen alerts:\nhigh lodash: proto pollution" {
		t.Fatalf("notes: %q", f.notes)
	}
	if s := r.Steps[2]; s.Answer != "yes" || s.Status != StatusOK || s.Input == "" {
		t.Fatalf("judge: %+v", s)
	}
}

func TestDependabotNoAlertsStops(t *testing.T) {
	w := parse(t, Examples["dependabot-daily"])
	f := &fake{}
	r := Run(context.Background(), w, f.env())
	if r.Status != StatusOK || len(f.prompts)+len(f.notes) != 0 || r.Steps[1].Branch != Stop || len(r.Steps) != 2 {
		t.Fatalf("%+v", r)
	}
}

func TestWebhookReplyOutputs(t *testing.T) {
	w := parse(t, Examples["webhook-reply"])
	f := &fake{answers: []string{"Sounds good, see you then."}}
	r := RunWith(context.Background(), w, f.env(), Invocation{Trigger: TriggerWebhook, Inputs: map[string]string{"message": "lunch at 1?"}})
	if r.Status != StatusOK || r.Outputs["reply"] != "Sounds good, see you then." || r.Inputs["message"] != "lunch at 1?" {
		t.Fatalf("%+v", r)
	}
	r = RunWith(context.Background(), w, f.env(), Invocation{Trigger: TriggerWebhook})
	if r.Status != StatusOK || r.Outputs["reply"] != "" || len(f.prompts) != 1 {
		t.Fatalf("empty message must stop: %+v", r)
	}
}

func TestRepoHealth(t *testing.T) {
	w := parse(t, Examples["repo-health"])
	f := &fake{shell: map[string]shellOut{w.Steps[0].Run: {`[{"workflowName":"check"}]`, 0}, w.Steps[1].Run: {`[{"number":7}]`, 0}}, answers: []string{"- check is red"}}
	r := Run(context.Background(), w, f.env())
	if r.Status != StatusOK || len(f.notes) != 1 || f.notes[0] != "Repo health: - check is red" || !strings.Contains(f.prompts[0], `"workflowName":"check"`) {
		t.Fatalf("%s %s %q %q", r.Status, r.Error, f.notes, f.prompts)
	}
}

func TestGuardBlocksRun(t *testing.T) {
	w := parse(t, `{"name":"g","steps":[{"id":"a","kind":"run","run":"git reset --hard"},{"id":"n","kind":"notify","message":"hi"}]}`)
	f := &fake{block: "reset"}
	r := Run(context.Background(), w, f.env())
	if r.Status != StatusFailed || len(f.ran) != 0 || len(f.notes) != 0 || !strings.Contains(r.Error, "guard") {
		t.Fatalf("%+v ran=%q", r, f.ran)
	}
}

func TestRetryOnErrorWhen(t *testing.T) {
	w := parse(t, `{"name":"r","steps":[
	 {"id":"flaky","kind":"run","run":"x","retry":{"count":2,"backoff":"1ms"}},
	 {"id":"bad","kind":"run","run":"y","on_error":"continue"},
	 {"id":"worse","kind":"run","run":"z","on_error":"rescue"},
	 {"id":"skipped","kind":"notify","message":"never"},
	 {"id":"rescue","kind":"notify","message":"bad said {{steps.bad.status}}","when":{"step":"bad","exit":3}},
	 {"id":"off","kind":"notify","message":"no","when":{"value":"{{steps.flaky.output}}","equals":"nope"}}]}`)
	f := &fake{fails: map[string]int{"x": 2}, shell: map[string]shellOut{"x": {"ok", 0}, "y": {"", 3}, "z": {"", 1}}}
	r := Run(context.Background(), w, f.env())
	if r.Status != StatusOK {
		t.Fatalf("%s %s", r.Status, r.Error)
	}
	got := map[string]StepResult{}
	for _, s := range r.Steps {
		got[s.ID] = s
	}
	if got["flaky"].Attempts != 3 || got["flaky"].Status != StatusOK {
		t.Errorf("retry: %+v", got["flaky"])
	}
	if got["worse"].Status != StatusFailed {
		t.Errorf("worse: %+v", got["worse"])
	}
	if _, ran := got["skipped"]; ran {
		t.Error("on_error goto must jump over skipped")
	}
	if got["off"].Status != StatusSkipped || len(f.notes) != 1 || f.notes[0] != "r: bad said ok" {
		t.Errorf("when: %+v %q", got["off"], f.notes)
	}
}

func TestSwitchSetJSONVars(t *testing.T) {
	w := parse(t, `{"name":"s","inputs":{"who":"ana"},"vars":{"greet":"hi {{inputs.who}}"},"steps":[
	 {"id":"data","kind":"run","run":"d"},
	 {"id":"lvl","kind":"json","from":"{{steps.data.output}}","path":"alerts.0.level"},
	 {"id":"pick","kind":"switch","value":"{{steps.lvl.output}}","cases":[{"equals":"low","goto":"calm"},{"match":"^(high|critical)$","goto":"loud"}],"default":"stop"},
	 {"id":"calm","kind":"notify","message":"calm"},
	 {"id":"loud","kind":"set","vars":{"msg":"{{vars.greet}}: {{steps.lvl.output}}"}},
	 {"id":"big","kind":"if","if":{"value":"{{steps.data.output}}","path":"count","op":"gt","to":"2"},"else":"stop"},
	 {"id":"tell","kind":"notify","message":"{{vars.msg}} x{{steps.lvl.exit}}"}]}`)
	f := &fake{shell: map[string]shellOut{"d": {`{"count":5,"alerts":[{"level":"high"}]}`, 0}}}
	r := Run(context.Background(), w, f.env())
	if r.Status != StatusOK || len(f.notes) != 1 || f.notes[0] != "s: hi ana: high x0" {
		t.Fatalf("%s %s %q", r.Status, r.Error, f.notes)
	}
	if !strings.Contains(strings.Join(f.envs[0], " "), "SMITHY_VAR_greet=hi ana") || !strings.Contains(strings.Join(f.envs[0], " "), "SMITHY_INPUT_who=ana") {
		t.Fatalf("run env: %v", f.envs[0])
	}
}

func TestForeach(t *testing.T) {
	w := parse(t, `{"name":"f","steps":[
	 {"id":"list","kind":"run","run":"ls"},
	 {"id":"each","kind":"foreach","items":"{{steps.list.output}}","lines":true,"limit":3,"parallel":2,"do":[
	   {"id":"say","kind":"notify","message":"#{{index}} {{item}}"}]},
	 {"id":"arr","kind":"foreach","items":"[1,{\"a\":2}]","do":[{"id":"echo","kind":"set","vars":{"v":"{{item}}"}}]}]}`)
	f := &fake{shell: map[string]shellOut{"ls": {"a\nb\n\nc\nd\n", 0}}}
	r := Run(context.Background(), w, f.env())
	if r.Status != StatusOK || len(f.notes) != 3 {
		t.Fatalf("%s %s %q", r.Status, r.Error, f.notes)
	}
	if got := r.Steps[1]; len(got.Steps) != 3 || got.Steps[2].ID != "each[2]" || !strings.Contains(got.Output, "#2 c") {
		t.Fatalf("each: %+v", got)
	}
	if got := r.Steps[2].Output; got != `["{\"v\":\"1\"}","{\"v\":\"{\\\"a\\\":2}\"}"]` {
		t.Fatalf("arr: %s", got)
	}
}

func TestAskAndCall(t *testing.T) {
	child := parse(t, `{"name":"child","inputs":{"n":"0"},"outputs":{"twice":"{{inputs.n}}{{inputs.n}}"},"steps":[{"id":"c","kind":"notify","message":"child got {{inputs.n}}"}]}`)
	w := parse(t, `{"name":"p","steps":[
	 {"id":"q","kind":"ask","question":"merge?","options":["yes","no"]},
	 {"id":"go","kind":"if","if":{"step":"q","equals":"yes"},"else":"stop"},
	 {"id":"sub","kind":"call","workflow":"child","with":{"n":"4"}},
	 {"id":"out","kind":"json","from":"{{steps.sub.output}}","path":"twice"},
	 {"id":"tell","kind":"notify","message":"{{steps.out.output}}"}]}`)
	f := &fake{reply: "yes", flows: map[string]Workflow{"child": child}, enabled: map[string]bool{}}
	r := RunWith(context.Background(), w, f.env(), Invocation{Trigger: "cli"})
	if r.Status != StatusOK || len(f.notes) != 2 || f.notes[1] != "p: 44" || !strings.HasPrefix(f.asked[0], "workflow:p:") {
		t.Fatalf("%s %s %q %q", r.Status, r.Error, f.notes, f.asked)
	}
	r = RunWith(context.Background(), w, f.env(), Invocation{Trigger: TriggerSchedule})
	if r.Status != StatusFailed || !strings.Contains(r.Error, "not enabled") {
		t.Fatalf("an unattended run must call only enabled workflows: %+v", r)
	}
	selfCall := parse(t, `{"name":"loop","steps":[{"id":"again","kind":"call","workflow":"loop"}]}`)
	f.flows["loop"], f.enabled["loop"] = selfCall, true
	if r := Run(context.Background(), selfCall, f.env()); r.Status != StatusFailed || !strings.Contains(r.Error, "deeper") {
		t.Fatalf("depth: %s", r.Error)
	}
	noAsk := f.env()
	noAsk.Ask = nil
	if r := Run(context.Background(), w, noAsk); r.Status != StatusFailed || !strings.Contains(r.Error, "daemon") {
		t.Fatalf("ask without daemon: %s", r.Error)
	}
}

func TestSecretsRedacted(t *testing.T) {
	w := parse(t, `{"name":"sec","outputs":{"o":"{{env.API_TOKEN}}"},"steps":[{"id":"a","kind":"agent","prompt":"use {{env.API_TOKEN}}"},{"id":"n","kind":"notify","message":"t={{env.API_TOKEN}} said {{steps.a.output}}"}]}`)
	f := &fake{answers: []string{"echo sk-live-12345"}, getenv: map[string]string{"API_TOKEN": "sk-live-12345"}}
	r := Run(context.Background(), w, f.env())
	all := r.Outputs["o"] + r.Steps[0].Input + r.Steps[0].Output + r.Steps[1].Output + strings.Join(f.notes, "")
	if strings.Contains(all, "sk-live-12345") || !strings.Contains(r.Steps[0].Output, "***") {
		t.Fatalf("secret leaked: %q", all)
	}
}

func TestAgentAnswers(t *testing.T) {
	cases := []struct {
		step, ans, want, why string
		ok                   bool
	}{
		{`"answer":"yesno"`, "**Yes** fine", "yes", "fine", true},
		{`"answer":"yesno"`, "no\nrisky", "no", "risky", true},
		{`"answer":"yesno"`, "maybe", "", "", false},
		{`"answer":"enum","options":["merge","wait","close"]`, "Wait: CI red", "wait", "CI red", true},
		{`"answer":"enum","options":["merge","wait"]`, "close it", "", "", false},
		{`"answer":"json","schema":{"ok":"bool","n":"number"}`, "Here: {\"ok\":true,\"n\":2}", `{"ok":true,"n":2}`, "", true},
		{`"answer":"json","schema":{"ok":"bool"}`, `{"ok":"yes"}`, "", "", false},
	}
	for _, c := range cases {
		w := parse(t, `{"name":"a","steps":[{"id":"q","kind":"agent","prompt":"p",`+c.step+`}]}`)
		f := &fake{answers: []string{c.ans}}
		r := Run(context.Background(), w, f.env())
		if (r.Status == StatusOK) != c.ok || r.Steps[0].Answer != c.want || r.Steps[0].Why != c.why {
			t.Errorf("%s / %q: %s %+v", c.step, c.ans, r.Status, r.Steps[0])
		}
	}
}

func TestCaps(t *testing.T) {
	w := parse(t, `{"name":"c","max_agents":1,"steps":[{"id":"a","kind":"agent","prompt":"one"},{"id":"b","kind":"agent","prompt":"two"}]}`)
	f := &fake{answers: []string{"x", "y"}}
	if r := Run(context.Background(), w, f.env()); r.Status != StatusCapped || len(f.prompts) != 1 {
		t.Fatalf("%s prompts=%d", r.Status, len(f.prompts))
	}
	w = parse(t, `{"name":"c","max_cost_usd":0.5,"steps":[{"id":"e","kind":"foreach","items":"[1,2,3]","do":[{"id":"a","kind":"agent","prompt":"{{item}}","on_error":"continue"}]}]}`)
	f = &fake{answers: []string{"x", "y", "z"}, cost: 0.6}
	if r := Run(context.Background(), w, f.env()); r.Status != StatusCapped || len(f.prompts) != 1 || r.CostUSD != 0.6 {
		t.Fatalf("a cap holds inside loops and over on_error: %s prompts=%d cost=%v", r.Status, len(f.prompts), r.CostUSD)
	}
}

func TestProgressAndDurations(t *testing.T) {
	w := parse(t, `{"name":"p","steps":[{"id":"a","kind":"run","run":"x"},{"id":"b","kind":"run","run":"x"}]}`)
	f := &fake{}
	env := f.env()
	var seen []string
	env.Progress = func(r Record) { seen = append(seen, r.Steps[len(r.Steps)-1].ID+":"+r.Steps[len(r.Steps)-1].Status) }
	r := Run(context.Background(), w, env)
	if strings.Join(seen, ",") != "a:running,b:running" || r.Steps[0].Ended.Before(r.Steps[0].Started) {
		t.Fatalf("progress %v", seen)
	}
}

func TestOutputCapped(t *testing.T) {
	w := parse(t, `{"name":"o","steps":[{"id":"a","kind":"run","run":"big"}]}`)
	f := &fake{shell: map[string]shellOut{"big": {strings.Repeat("x", MaxOutput+10), 0}}}
	if r := Run(context.Background(), w, f.env()); len(r.Steps[0].Output) > MaxOutput+40 {
		t.Fatalf("output %d bytes", len(r.Steps[0].Output))
	}
}

func TestStepTimeout(t *testing.T) {
	w := parse(t, `{"name":"t","steps":[{"id":"a","kind":"run","run":"slow","timeout":"20ms"}]}`)
	env := (&fake{}).env()
	env.Shell = func(ctx context.Context, _, _ string, _ []string) (string, int, error) {
		<-ctx.Done()
		return "", -1, errors.New("timed out")
	}
	start := time.Now()
	if r := Run(context.Background(), w, env); r.Status != StatusFailed || time.Since(start) > 2*time.Second {
		t.Fatalf("%+v", r)
	}
}

func TestValidate(t *testing.T) {
	bad := []string{
		`{"name":"Bad Name","steps":[{"id":"a","kind":"notify","message":"x"}]}`,
		`{"name":"x","schedule":"nope","steps":[{"id":"a","kind":"notify","message":"x"}]}`,
		`{"name":"x","steps":[]}`,
		`{"name":"x","steps":[{"id":"a","kind":"run","run":"echo {{steps.a.output}}"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"notify","message":"{{steps.zz.output}}"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"notify","message":"{{bogus}}"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"run","run":"ls"},{"id":"i","kind":"if","if":{"step":"a","exit":0},"then":"a"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"run","run":"ls"},{"id":"i","kind":"if","if":{"step":"a"}}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"shell","run":"ls"}]}`,
		`{"name":"x","extra":1,"steps":[{"id":"a","kind":"run","run":"ls"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"run","run":"ls"},{"id":"a","kind":"run","run":"ls"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"run","run":"ls","timeout":"soon"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"run","run":"ls","retry":{"count":9}}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"run","run":"ls","on_error":"nowhere"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"agent","prompt":"p","answer":"enum","options":["one"]}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"agent","prompt":"p","schema":{"k":"string"}}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"foreach","items":"[]","parallel":50,"do":[{"id":"b","kind":"set","vars":{"v":"1"}}]}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"call","workflow":"../etc"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"json","from":"{}","path":"a..b"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"switch","value":"v","cases":[{"equals":"a","match":"b","goto":"stop"}]}]}`,
	}
	for _, b := range bad {
		if _, err := Parse([]byte(b)); err == nil {
			t.Errorf("want error: %s", b)
		}
	}
}
