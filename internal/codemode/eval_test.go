//go:build harness

package codemode

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEvalReturnsJSONFromStepsInputsAndVars(t *testing.T) {
	in := `{"steps":{"prs":{"output":"[{\"n\":1,\"age\":40},{\"n\":2,\"age\":3}]","json":[{"n":1,"age":40},{"n":2,"age":3}],"exit":0}},"inputs":{"days":"30"},"vars":{"who":"me"}}`
	out, err := Eval(context.Background(), `const d = Number(inputs.days);
return steps.prs.json.filter(p => p.age > d).map(p => ({n: p.n, who: vars.who}));`, in, Limits{})
	if err != nil || out != `[{"n":1,"who":"me"}]` {
		t.Fatalf("out = %q, err = %v", out, err)
	}
}

func TestEvalHelpersAndBareStrings(t *testing.T) {
	in := `{"steps":{"ls":{"output":"a\n\nb\n","exit":0}},"inputs":{},"vars":{}}`
	out, err := Eval(context.Background(), `return lines(steps.ls.output).join("+") + json('{"x":2}').x`, in, Limits{})
	if err != nil || out != "a+b2" {
		t.Fatalf("out = %q, err = %v", out, err)
	}
	if out, err := Eval(context.Background(), `const x = 1;`, in, Limits{}); err != nil || out != "" {
		t.Fatalf("no return: %q %v", out, err)
	}
}

func TestEvalErrorNamesMessageAndLine(t *testing.T) {
	_, err := Eval(context.Background(), "const a = 1;\nconst b = 2;\nthrow new Error(\"too old\");", `{}`, Limits{})
	if err == nil || !strings.Contains(err.Error(), "too old") || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("err = %v", err)
	}
	_, err = Eval(context.Background(), "return steps.nope.output", `{"steps":{}}`, Limits{})
	if err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("err = %v", err)
	}
}

func TestEvalHasNoWayOut(t *testing.T) {
	for _, code := range []string{
		`return typeof std + typeof os + typeof fetch + typeof require + typeof tools + typeof __smithy_input`,
	} {
		out, err := Eval(context.Background(), code, `{}`, Limits{})
		if err != nil || out != strings.Repeat("undefined", 6) {
			t.Fatalf("out = %q, err = %v", out, err)
		}
	}
	dir := t.TempDir()
	secret := dir + "/secret.txt"
	if err := os.WriteFile(secret, []byte("hunter2"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	for _, code := range []string{
		`const s = await import("qjs:std"); return s.loadFile("/secret.txt")`,
		`const s = await import("qjs:std"); return s.loadFile("` + secret + `")`,
		`const s = await import("qjs:std"); const f = s.open("/x.txt", "w"); return !!f`,
	} {
		out, _ := Eval(context.Background(), code, `{}`, Limits{})
		if strings.Contains(out, "hunter2") || out == "true" {
			t.Errorf("%s reached the host: %q", code, out)
		}
	}
}

func TestEvalLimits(t *testing.T) {
	start := time.Now()
	_, err := Eval(context.Background(), `while (true) {}`, `{}`, Limits{Time: 300 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "time limit") || time.Since(start) > 10*time.Second {
		t.Fatalf("err = %v after %s", err, time.Since(start))
	}
	_, err = Eval(context.Background(), `const a = []; while (true) a.push(new ArrayBuffer(1 << 20));`, `{}`, Limits{Memory: 16 << 20})
	if err == nil {
		t.Fatal("memory cap did not hold")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := Eval(ctx, `while (true) {}`, `{}`, Limits{}); err == nil {
		t.Fatal("caller's deadline ignored")
	}
}

func TestEvalLastExpressionAndTrueLines(t *testing.T) {
	in := `{"steps":{"a":{"output":"x\ny"}}}`
	out, err := Eval(context.Background(), "const l = lines(steps.a.output);\nawait 0;\nl.length", in, Limits{})
	if err != nil || out != "2" {
		t.Fatalf("out = %q, err = %v", out, err)
	}
	_, err = Eval(context.Background(), "const x = 1;\n\nx.y.z", in, Limits{})
	if err == nil || !strings.Contains(err.Error(), "cannot read property 'z'") || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("err = %v", err)
	}
	_, err = Eval(context.Background(), "const x = 1;\nreturn x.y.z", in, Limits{})
	if err == nil || strings.Contains(err.Error(), "line 1") {
		t.Fatalf("a function body's engine error names a wrong line: %v", err)
	}
}
