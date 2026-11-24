package pool

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	claudeLimitRe = regexp.MustCompile(`(?i)(hit your [a-z0-9 -]*limit|usage limit reached)`)
	codexLimit    = "hit your usage limit"
	resetRe       = regexp.MustCompile(`(?i)(?:resets|try again at)\s+` +
		`(?:(mon|tue|wed|thu|fri|sat|sun)[a-z]*\.?,?\s+)?` +
		`(?:(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+(\d{1,2})(?:st|nd|rd|th)?,?\s+(?:\d{4},?\s+)?(?:at\s+)?)?` +
		`(\d{1,2})(?::(\d{2}))?\s*(am|pm)?(?:\s*\(([^)]+)\))?`)
)

// Warn receives the one-line warnings Reset gives (an unknown time zone); stderr by default.
var Warn = func(s string) { fmt.Fprintln(os.Stderr, "smithy pool:", s) }

// Detect reads a finished run's log for a usage-limit result of kind's CLI. claude: only a
// result JSON object with is_error and limit text; codex: only the sentence "hit your usage
// limit" in the last lines of a failed run (rc != 0). Any other CLI or text never counts, so a
// test log that mentions rate_limit does not mark an account. until is the reset the CLI gave,
// else now + DefaultLimit.
func Detect(log, kind string, rc int, now time.Time) (until time.Time, ok bool) {
	b, err := os.ReadFile(log)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	switch kind {
	case "claude":
		for i := len(lines) - 1; i >= 0 && i >= len(lines)-40; i-- {
			if t, isErr := claudeError(lines[i]); isErr && claudeLimitRe.MatchString(t) {
				return Reset(t, now), true
			}
		}
	case "codex":
		if rc == 0 {
			return
		}
		for i := len(lines) - 1; i >= 0 && i >= len(lines)-5; i-- {
			if strings.Contains(strings.ToLower(lines[i]), codexLimit) {
				return Reset(lines[i], now), true
			}
		}
	}
	return
}

func claudeError(line string) (string, bool) {
	var r struct {
		Type    string            `json:"type"`
		IsError bool              `json:"is_error"`
		Result  string            `json:"result"`
		Errors  []json.RawMessage `json:"errors"`
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &r) != nil || r.Type != "result" || !r.IsError {
		return "", false
	}
	t := r.Result
	for _, e := range r.Errors {
		t += " " + string(e)
	}
	return t, true
}

var months = map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
var weekdays = map[string]time.Weekday{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

// Reset parses "resets 3pm (Europe/Madrid)", "resets Nov 28, 3pm", "resets Mon 9am", "resets
// 14:30" or "try again at 3:05 PM" into the next such time after now (in the named zone, else
// local). now + DefaultLimit when absent, malformed or in an unknown zone (with one warning).
func Reset(text string, now time.Time) time.Time {
	m := resetRe.FindStringSubmatch(text)
	if m == nil {
		return now.Add(DefaultLimit)
	}
	wd, mon, day, hs, ms, ap, tz := strings.ToLower(m[1]), strings.ToLower(m[2]), m[3], m[4], m[5], strings.ToLower(m[6]), strings.TrimSpace(m[7])
	loc := time.Local
	if tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			Warn(fmt.Sprintf("unknown time zone %q in a limit message; limited for %s", tz, DefaultLimit))
			return now.Add(DefaultLimit)
		}
		loc = l
	}
	var h, mi, d int
	fmt.Sscanf(hs, "%d", &h)
	if ms != "" {
		fmt.Sscanf(ms, "%d", &mi)
	}
	switch ap {
	case "pm":
		if h < 12 {
			h += 12
		}
	case "am":
		if h == 12 {
			h = 0
		}
	}
	if h > 23 || mi > 59 {
		return now.Add(DefaultLimit)
	}
	n := now.In(loc)
	switch {
	case mon != "":
		fmt.Sscanf(day, "%d", &d)
		t := time.Date(n.Year(), months[mon[:3]], d, h, mi, 0, 0, loc)
		if !t.After(now) {
			t = t.AddDate(1, 0, 0)
		}
		return t
	case wd != "":
		t := time.Date(n.Year(), n.Month(), n.Day(), h, mi, 0, 0, loc)
		t = t.AddDate(0, 0, (int(weekdays[wd[:3]])-int(n.Weekday())+7)%7)
		if !t.After(now) {
			t = t.AddDate(0, 0, 7)
		}
		return t
	}
	t := time.Date(n.Year(), n.Month(), n.Day(), h, mi, 0, 0, loc)
	if !t.After(now) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}
