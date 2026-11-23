// Package usage reads the token use and cost an agent CLI reports for one headless run and keeps
// it in a `<log>.usage.json` sidecar next to the run's log. Only what the CLI reports is kept:
// an unknown cost stays nil and shows as "-", never an estimate.
package usage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Usage is the token use of one run (or a sum of runs) and its cost in USD when known.
type Usage struct {
	Input      int64    `json:"input_tokens"`
	Output     int64    `json:"output_tokens"`
	CacheRead  int64    `json:"cache_read_tokens"`
	CacheWrite int64    `json:"cache_write_tokens"`
	CostUSD    *float64 `json:"cost_usd,omitempty"`
}

// Add sums two usages; the cost is the sum of the known costs, nil when neither is known.
func (u Usage) Add(o Usage) Usage {
	s := Usage{Input: u.Input + o.Input, Output: u.Output + o.Output, CacheRead: u.CacheRead + o.CacheRead, CacheWrite: u.CacheWrite + o.CacheWrite}
	if u.CostUSD != nil || o.CostUSD != nil {
		c := 0.0
		for _, p := range []*float64{u.CostUSD, o.CostUSD} {
			if p != nil {
				c += *p
			}
		}
		s.CostUSD = &c
	}
	return s
}

// String is a one-line summary for logs and detail panes.
func (u Usage) String() string {
	return fmt.Sprintf("in %d, out %d, cache read %d, cache write %d, cost %s", u.Input, u.Output, u.CacheRead, u.CacheWrite, Cost(u.CostUSD))
}

// Cost formats a cost: "-" when unknown.
func Cost(c *float64) string {
	if c == nil {
		return "-"
	}
	return fmt.Sprintf("$%.2f", *c)
}

// FromClaude parses the result object `claude -p --output-format json` prints when it ends:
// usage.{input_tokens, output_tokens, cache_read_input_tokens, cache_creation_input_tokens}
// and total_cost_usd. text is the run's final answer (the `result` field).
func FromClaude(line []byte) (u Usage, text string, ok bool) {
	var r struct {
		Type   string   `json:"type"`
		Result string   `json:"result"`
		Cost   *float64 `json:"total_cost_usd"`
		Usage  *struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &r) != nil || r.Type != "result" || r.Usage == nil {
		return u, "", false
	}
	return Usage{Input: r.Usage.Input, Output: r.Usage.Output, CacheRead: r.Usage.CacheRead, CacheWrite: r.Usage.CacheWrite, CostUSD: r.Cost}, r.Result, true
}

// Sidecar is the usage file of a log.
func Sidecar(log string) string { return log + ".usage.json" }

// Finish turns a claude json run's log back into text: the last result line becomes its
// `result` text, so the log reads as it did with text output, and the usage goes to the
// sidecar. ok is false (and nothing changes) when the log has no result line.
func Finish(log string) (Usage, bool) {
	b, err := os.ReadFile(log)
	if err != nil {
		return Usage{}, false
	}
	lines := strings.Split(string(b), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		u, text, ok := FromClaude([]byte(lines[i]))
		if !ok {
			continue
		}
		lines[i] = strings.TrimRight(text, "\n")
		if err := os.WriteFile(log, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
			return Usage{}, false
		}
		j, _ := json.Marshal(u)
		if os.WriteFile(Sidecar(log), append(j, '\n'), 0o600) != nil {
			return u, false
		}
		return u, true
	}
	return Usage{}, false
}

// Read loads a log's sidecar; ok is false when there is none.
func Read(log string) (Usage, bool) {
	var u Usage
	b, err := os.ReadFile(Sidecar(log))
	if err != nil || json.Unmarshal(b, &u) != nil {
		return Usage{}, false
	}
	return u, true
}
