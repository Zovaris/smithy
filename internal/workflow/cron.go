package workflow

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cron is a parsed five-field schedule: minute, hour, day of month, month, day of week (0 or 7
// is Sunday). When both day fields are restricted a day matching either runs, as in cron(8).
type Cron struct {
	min, hour, dom, mon, dow uint64
	domAny, dowAny           bool
}

var macros = map[string]string{"@hourly": "0 * * * *", "@daily": "0 0 * * *", "@midnight": "0 0 * * *",
	"@weekly": "0 0 * * 0", "@monthly": "0 0 1 * *", "@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *"}

var names = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}

// ParseCron parses a five-field cron spec or one of the @hourly, @daily, @weekly, @monthly,
// @yearly macros. Fields take *, numbers, names (jan, sun), ranges a-b, lists and /steps.
func ParseCron(spec string) (Cron, error) {
	spec = strings.TrimSpace(spec)
	if strings.HasPrefix(spec, "@") {
		m, ok := macros[strings.ToLower(spec)]
		if !ok {
			return Cron{}, fmt.Errorf("cron: unknown macro %q", spec)
		}
		spec = m
	}
	f := strings.Fields(spec)
	if len(f) != 5 {
		return Cron{}, fmt.Errorf("cron: want 5 fields (minute hour day month weekday), got %d", len(f))
	}
	var c Cron
	var err error
	if c.min, err = field(f[0], 0, 59); err != nil {
		return c, fmt.Errorf("cron minute: %w", err)
	}
	if c.hour, err = field(f[1], 0, 23); err != nil {
		return c, fmt.Errorf("cron hour: %w", err)
	}
	if c.dom, err = field(f[2], 1, 31); err != nil {
		return c, fmt.Errorf("cron day: %w", err)
	}
	if c.mon, err = field(f[3], 1, 12); err != nil {
		return c, fmt.Errorf("cron month: %w", err)
	}
	if c.dow, err = field(f[4], 0, 7); err != nil {
		return c, fmt.Errorf("cron weekday: %w", err)
	}
	if c.dow&(1<<7) != 0 {
		c.dow |= 1
	}
	c.domAny, c.dowAny = f[2] == "*", f[4] == "*"
	if c.dowAny && !possible(c) {
		return c, fmt.Errorf("cron: day %s never falls in month %s", f[2], f[3])
	}
	return c, nil
}

func possible(c Cron) bool {
	days := [13]int{0, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	for m := 1; m <= 12; m++ {
		for d := 1; d <= days[m]; d++ {
			if c.mon&(1<<m) != 0 && c.dom&(1<<d) != 0 {
				return true
			}
		}
	}
	return false
}

func field(s string, lo, hi int) (uint64, error) {
	var bits uint64
	for _, part := range strings.Split(s, ",") {
		if part == "" {
			return 0, fmt.Errorf("empty list item in %q", s)
		}
		rng, step := part, 1
		if i := strings.IndexByte(part, '/'); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n < 1 {
				return 0, fmt.Errorf("bad step in %q", part)
			}
			rng, step = part[:i], n
		}
		a, b := lo, hi
		if rng != "*" {
			x, y, isRange := strings.Cut(rng, "-")
			var err error
			if a, err = num(x, lo, hi); err != nil {
				return 0, err
			}
			b = a
			if isRange {
				if b, err = num(y, lo, hi); err != nil {
					return 0, err
				}
			} else if step > 1 {
				b = hi
			}
			if b < a {
				return 0, fmt.Errorf("range %q runs backwards", rng)
			}
		}
		for v := a; v <= b; v += step {
			bits |= 1 << v
		}
	}
	return bits, nil
}

func num(s string, lo, hi int) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		v, ok := names[strings.ToLower(s)]
		if !ok {
			return 0, fmt.Errorf("%q is not a number", s)
		}
		n = v
	}
	if n < lo || n > hi {
		return 0, fmt.Errorf("%d is outside %d-%d", n, lo, hi)
	}
	return n, nil
}

func (c Cron) day(t time.Time) bool {
	if c.mon&(1<<int(t.Month())) == 0 {
		return false
	}
	dom, dow := c.dom&(1<<t.Day()) != 0, c.dow&(1<<int(t.Weekday())) != 0
	switch {
	case c.domAny && c.dowAny:
		return true
	case c.domAny:
		return dow
	case c.dowAny:
		return dom
	}
	return dom || dow
}

// Next is the first minute strictly after t that the schedule matches, in t's location; the zero
// time when none falls in the next eight years.
func (c Cron) Next(t time.Time) time.Time {
	t = t.Truncate(time.Minute).Add(time.Minute)
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	for i := 0; i < 366*8; i++ {
		if c.day(d) {
			for h := 0; h < 24; h++ {
				if c.hour&(1<<h) == 0 {
					continue
				}
				for m := 0; m < 60; m++ {
					if c.min&(1<<m) == 0 {
						continue
					}
					x := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, d.Location())
					if !x.Before(t) {
						return x
					}
				}
			}
		}
		d = time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, d.Location())
	}
	return time.Time{}
}
