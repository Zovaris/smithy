package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var tmplRe = regexp.MustCompile(`\{\{\s*(?:steps\.([a-z0-9][a-z0-9_-]*)\.(output|answer|why|exit|status)|vars\.([A-Za-z_][A-Za-z0-9_]*)|inputs\.([A-Za-z_][A-Za-z0-9_]*)|env\.([A-Z_][A-Z0-9_]*)|(item|index))\s*\}\}`)

// scope is what templates and conditions read: step results, variables, inputs, the loop item
// and the environment. Each foreach iteration gets its own copy.
type scope struct {
	mu      *sync.Mutex
	steps   map[string]StepResult
	vars    map[string]string
	inputs  map[string]string
	item    string
	index   string
	env     func(string) string
	secrets *secrets
}

// secrets remembers every {{env.X}} value a run read, so the record never keeps them.
type secrets struct {
	mu   sync.Mutex
	vals map[string]bool
}

func (s *secrets) add(v string) {
	if len(v) < 4 {
		return
	}
	s.mu.Lock()
	s.vals[v] = true
	s.mu.Unlock()
}

func (s *secrets) redact(text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.vals))
	for v := range s.vals {
		keys = append(keys, v)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, v := range keys {
		text = strings.ReplaceAll(text, v, "***")
	}
	return text
}

func (sc *scope) child() *scope {
	c := *sc
	sc.mu.Lock()
	c.steps = make(map[string]StepResult, len(sc.steps))
	for k, v := range sc.steps {
		c.steps[k] = v
	}
	c.vars = make(map[string]string, len(sc.vars))
	for k, v := range sc.vars {
		c.vars[k] = v
	}
	sc.mu.Unlock()
	c.mu = &sync.Mutex{}
	return &c
}

func (sc *scope) step(id string) StepResult {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.steps[id]
}

func (sc *scope) setStep(r StepResult) {
	sc.mu.Lock()
	sc.steps[r.ID] = r
	sc.mu.Unlock()
}

func (sc *scope) setVar(k, v string) {
	sc.mu.Lock()
	sc.vars[k] = v
	sc.mu.Unlock()
}

// render fills templates. data wraps each value in <data> tags (agent prompts), so the agent
// can tell it from the instructions; clip caps each value (notify text).
func (sc *scope) render(text string, data bool, clip int) string {
	return tmplRe.ReplaceAllStringFunc(text, func(m string) string {
		g := tmplRe.FindStringSubmatch(m)
		var v, name string
		switch {
		case g[1] != "":
			r := sc.step(g[1])
			name = "steps." + g[1] + "." + g[2]
			switch g[2] {
			case "output":
				v = strings.TrimSpace(r.Output)
			case "answer":
				v = r.Answer
			case "why":
				v = r.Why
			case "exit":
				v = strconv.Itoa(r.Exit)
			case "status":
				v = r.Status
			}
		case g[3] != "":
			sc.mu.Lock()
			v, name = sc.vars[g[3]], "vars."+g[3]
			sc.mu.Unlock()
		case g[4] != "":
			v, name = sc.inputs[g[4]], "inputs."+g[4]
		case g[5] != "":
			if sc.env != nil {
				v = sc.env(g[5])
			}
			sc.secrets.add(v)
			name = "env." + g[5]
		case g[6] == "item":
			v, name = sc.item, "item"
		default:
			v, name = sc.index, "index"
		}
		if clip > 0 && len(v) > clip {
			v = v[:clip] + "…"
		}
		if data {
			return `<data name="` + name + `">` + strings.ReplaceAll(v, "</data>", "</ data>") + "</data>"
		}
		return v
	})
}

// test evaluates c.
func (sc *scope) test(c *Cond) (bool, error) {
	if c.Exit != nil {
		return sc.step(c.Step).Exit == *c.Exit, nil
	}
	subject := sc.render(c.Value, false, 0)
	if c.Value == "" {
		r := sc.step(c.Step)
		subject = r.Answer
		if subject == "" {
			subject = strings.TrimSpace(r.Output)
		}
	}
	switch {
	case c.Match != "":
		return regexp.MustCompile(c.Match).MatchString(subject), nil
	case c.Equals != "":
		return strings.EqualFold(strings.TrimSpace(subject), strings.TrimSpace(sc.render(c.Equals, false, 0))), nil
	}
	var doc any
	if err := json.Unmarshal([]byte(subject), &doc); err != nil {
		return false, fmt.Errorf("condition path %s: the value is not JSON", c.Path)
	}
	segs, _ := splitPath(c.Path)
	got, found := pick(doc, segs)
	return compare(c.Op, got, found, sc.render(c.To, false, 0)), nil
}

func compare(op string, got any, found bool, to string) bool {
	if op == "exists" {
		return found
	}
	if !found {
		return op == "ne"
	}
	gs := text(got)
	switch op {
	case "", "eq":
		return gs == to
	case "ne":
		return gs != to
	case "contains":
		if a, ok := got.([]any); ok {
			for _, x := range a {
				if text(x) == to {
					return true
				}
			}
			return false
		}
		return strings.Contains(gs, to)
	}
	a, err1 := strconv.ParseFloat(gs, 64)
	b, err2 := strconv.ParseFloat(to, 64)
	if err1 != nil || err2 != nil {
		return false
	}
	switch op {
	case "gt":
		return a > b
	case "ge":
		return a >= b
	case "lt":
		return a < b
	}
	return a <= b
}

// splitPath splits a JSON path like items.0.name; "" is the whole value.
func splitPath(p string) ([]string, error) {
	if p == "" {
		return nil, nil
	}
	segs := strings.Split(p, ".")
	for _, s := range segs {
		if s == "" {
			return nil, fmt.Errorf("path %q has an empty part", p)
		}
	}
	return segs, nil
}

func pick(v any, segs []string) (any, bool) {
	for _, s := range segs {
		switch x := v.(type) {
		case map[string]any:
			n, ok := x[s]
			if !ok {
				return nil, false
			}
			v = n
		case []any:
			i, err := strconv.Atoi(s)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// text is a JSON value as a step output: strings bare, everything else compact JSON.
func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
