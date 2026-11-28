package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShipAFix(t *testing.T) {
	w := parse(t, Examples["ship-a-fix"])
	f := &fake{shell: map[string]shellOut{}, answers: []string{"fixed it", "fixed it again", `{"score":0.9,"why":"clean"}`}, reply: "yes"}
	tests := 0
	env := f.env()
	env.Shell = func(_ context.Context, _, cmd string, envv []string) (string, int, error) {
		if strings.Contains(cmd, "SMITHY_VAR_test") {
			tests++
			if !strings.Contains(strings.Join(envv, " "), "SMITHY_VAR_test=go test ./...") {
				t.Fatalf("env %v", envv)
			}
			if tests == 1 {
				return "FAIL TestX", 1, nil
			}
			return "ok", 0, nil
		}
		return "diff", 0, nil
	}
	var argvs []string
	env.Exec = func(_ context.Context, _ string, argv []string) (string, int, error) {
		argvs = append(argvs, strings.Join(argv, " "))
		return "https://github.com/o/r/pull/9", 0, nil
	}
	r := RunWith(context.Background(), w, env, Invocation{Trigger: "web", Inputs: map[string]string{"bug": "login loops"}})
	if r.Status != StatusOK {
		t.Fatalf("%s %s", r.Status, r.Error)
	}
	if tests != 2 || len(f.prompts) != 3 || !strings.Contains(f.prompts[1], "FAIL TestX") {
		t.Fatalf("tests=%d prompts=%q", tests, f.prompts)
	}
	got := map[string]StepResult{}
	for _, s := range r.Steps {
		got[s.ID] = s
	}
	if got["green"].Answer != "met" || len(got["green"].Steps) != 2 || got["review"].Answer != "pass" {
		t.Fatalf("green %+v review %+v", got["green"], got["review"])
	}
	if len(argvs) != 2 || !strings.HasPrefix(argvs[1], "gh pr create --title fix: login loops") {
		t.Fatalf("argv %q", argvs)
	}
	f2 := &fake{answers: []string{"a", `{"score":0.2,"why":"no test"}`}, reply: "no"}
	env2 := f2.env()
	env2.Exec = func(context.Context, string, []string) (string, int, error) { t.Fatal("no PR without a yes"); return "", 0, nil }
	if r := RunWith(context.Background(), w, env2, Invocation{Inputs: map[string]string{"bug": "x"}}); r.Status != StatusOK {
		t.Fatalf("%s %s", r.Status, r.Error)
	}
}

func TestLoopNotMet(t *testing.T) {
	w := parse(t, `{"name":"l","steps":[{"id":"lp","kind":"loop","max":2,"until":{"step":"t","exit":0},"do":[{"id":"t","kind":"run","run":"x"}]}]}`)
	f := &fake{shell: map[string]shellOut{"x": {"no", 1}}}
	if r := Run(context.Background(), w, f.env()); r.Status != StatusFailed || len(f.ran) != 2 || !strings.Contains(r.Error, "not met after 2") {
		t.Fatalf("%+v", r)
	}
}

func TestFanOutAgents(t *testing.T) {
	w := parse(t, `{"name":"fan","steps":[{"id":"each","kind":"agent","items":"[\"a.go\",\"b.go\",\"c.go\"]","parallel":2,"prompt":"review {{item}}","answer":"yesno"}]}`)
	f := &fake{answers: []string{"yes ok", "no bad", "yes fine"}}
	r := Run(context.Background(), w, f.env())
	if r.Status != StatusOK || len(f.prompts) != 3 || len(r.Steps[0].Steps) != 3 {
		t.Fatalf("%+v", r)
	}
	w = parse(t, `{"name":"fan","max_agents":2,"steps":[{"id":"each","kind":"agent","items":"[1,2,3]","prompt":"{{item}}"}]}`)
	f = &fake{answers: []string{"1", "2", "3"}}
	if r := Run(context.Background(), w, f.env()); r.Status != StatusCapped || len(f.prompts) != 2 {
		t.Fatalf("fan-out under max_agents: %s %d", r.Status, len(f.prompts))
	}
}

func TestParallelJoinAndWait(t *testing.T) {
	w := parse(t, `{"name":"p","steps":[
	 {"id":"both","kind":"parallel","branches":[[{"id":"a","kind":"set","vars":{"x":"1"}}],[{"id":"b","kind":"run","run":"b"}]]},
	 {"id":"w","kind":"wait","for":"1ms"},
	 {"id":"poll","kind":"wait","check":"ready","every":"1ms","timeout":"5s"},
	 {"id":"tell","kind":"notify","message":"{{vars.x}} {{steps.b.output}}"}]}`)
	f := &fake{shell: map[string]shellOut{"b": {"B", 0}}, fails: map[string]int{"ready": 2}}
	f.shell["ready"] = shellOut{"up", 0}
	r := Run(context.Background(), w, f.env())
	if r.Status != StatusOK || f.notes[0] != "p: 1 B" || r.Steps[2].Output != "up" || len(f.ran) != 4 {
		t.Fatalf("%s %s %q %+v", r.Status, r.Error, f.notes, r.Steps[2])
	}
}

func TestSessionSmeltWarden(t *testing.T) {
	w := parse(t, `{"name":"s","allow_write":true,"steps":[
	 {"id":"q","kind":"ask","question":"go?","options":["yes","no"]},
	 {"id":"chat","kind":"session","prompt":"look at {{steps.q.answer}}"},
	 {"id":"build","kind":"smelt","task":"add a health endpoint","speed":"normal"},
	 {"id":"batch","kind":"warden","task":"docs/specs/v2","when":{"step":"q","equals":"yes"}}]}`)
	f := &fake{reply: "yes"}
	env := f.env()
	var got []string
	env.Session = func(_ context.Context, _, _, prompt string) (string, error) {
		got = append(got, prompt)
		return "s-1", nil
	}
	if r := Run(context.Background(), w, env); r.Status != StatusOK || strings.Join(got, "|") != "look at yes|smelt normal add a health endpoint|warden docs/specs/v2" {
		t.Fatalf("%s %s %q", r.Status, r.Error, got)
	}
}

func TestHTTPSecretsRedacted(t *testing.T) {
	w := parse(t, `{"name":"h","steps":[{"id":"get","kind":"http","url":"https://api.github.com/x","headers":{"Authorization":"Bearer {{secret.github}}"}}]}`)
	f := &fake{}
	env := f.env()
	env.Secret = func(n string) string { return map[string]string{"github": "ghp_topsecret99"}[n] }
	var sent string
	env.Fetch = func(_ context.Context, m, u string, h map[string]string, _ string) (int, string, error) {
		sent = h["Authorization"]
		return 200, "echo ghp_topsecret99", nil
	}
	r := Run(context.Background(), w, env)
	if r.Status != StatusOK || sent != "Bearer ghp_topsecret99" || strings.Contains(r.Steps[0].Output, "ghp_topsecret99") {
		t.Fatalf("%+v sent=%q", r, sent)
	}
	if code, _, err := Fetch(context.Background(), "GET", "https://not-allowed.example/", nil, ""); err == nil || code != 0 {
		t.Fatal("a host outside guard.fetch_allow must be refused")
	}
}

func TestFileStep(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()
	os.WriteFile(filepath.Join(root, "in.txt"), []byte("hello"), 0o600)
	os.Symlink(out, filepath.Join(root, "escape"))
	w := parse(t, `{"name":"f","allow_write":true,"files":["`+root+`"],"steps":[
	 {"id":"r","kind":"file","op":"read","path":"in.txt"},
	 {"id":"w","kind":"file","op":"write","path":"out.txt","content":"{{steps.r.output}} world"}]}`)
	if r := Run(context.Background(), w, (&fake{}).env()); r.Status != StatusOK {
		t.Fatalf("%s", r.Error)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "out.txt")); string(b) != "hello world" {
		t.Fatalf("%q", b)
	}
	for _, p := range []string{"../x.txt", "/etc/passwd", "escape/x.txt"} {
		bad := parse(t, `{"name":"f","allow_write":true,"files":["`+root+`"],"steps":[{"id":"w","kind":"file","op":"write","path":"`+p+`","content":"x"}]}`)
		if r := Run(context.Background(), bad, (&fake{}).env()); r.Status != StatusFailed || !strings.Contains(r.Error, "outside") {
			t.Errorf("%s: %s %s", p, r.Status, r.Error)
		}
	}
}

func TestGitHubSteps(t *testing.T) {
	w := parse(t, `{"name":"g","allow_write":true,"steps":[
	 {"id":"list","kind":"github","action":"prs"},
	 {"id":"say","kind":"github","action":"comment","pr":"12","body":"hi"},
	 {"id":"bad","kind":"github","action":"label","pr":"12; rm -rf /","label":"x"}]}`)
	f := &fake{}
	env := f.env()
	var argvs []string
	env.Exec = func(_ context.Context, _ string, argv []string) (string, int, error) {
		argvs = append(argvs, strings.Join(argv, "|"))
		return "[]", 0, nil
	}
	r := Run(context.Background(), w, env)
	if r.Status != StatusFailed || !strings.Contains(r.Error, "not a PR") || len(argvs) != 2 || argvs[1] != "gh|pr|comment|12|--body|hi" {
		t.Fatalf("%s %s %q", r.Status, r.Error, argvs)
	}
}

func TestValidateAISteps(t *testing.T) {
	bad := []string{
		`{"name":"x","steps":[{"id":"a","kind":"smelt","task":"t"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"agent","prompt":"p","write":true}]}`,
		`{"name":"x","allow_write":true,"steps":[{"id":"a","kind":"warden","task":"t"}]}`,
		`{"name":"x","allow_write":true,"steps":[{"id":"a","kind":"github","action":"merge","pr":"1"}]}`,
		`{"name":"x","allow_write":true,"steps":[{"id":"q","kind":"ask","question":"?"},{"id":"a","kind":"github","action":"merge","pr":"1","when":{"step":"q","equals":"no"}}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"github","action":"comment","pr":"1"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"http","method":"POST","url":"https://x.y"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"http","url":"ftp://x.y"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"notify","message":"{{secret.k}}"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"http","url":"https://x.y/{{secret.k}}"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"file","op":"read","path":"p"}]}`,
		`{"name":"x","files":["/tmp"],"steps":[{"id":"a","kind":"file","op":"write","path":"p"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"loop","until":{"step":"a","exit":0},"max":50,"do":[{"id":"b","kind":"run","run":"x"}]}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"parallel","branches":[[{"id":"b","kind":"run","run":"x"}]]}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"wait","for":"1s","check":"x"}]}`,
		`{"name":"x","steps":[{"id":"a","kind":"judge","subject":"s"}]}`,
	}
	for _, b := range bad {
		if _, err := Parse([]byte(b)); err == nil {
			t.Errorf("want error: %s", b)
		}
	}
}
