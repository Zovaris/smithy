package workflow

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func TestCronNext(t *testing.T) {
	cases := []struct{ spec, from, want string }{
		{"0 17 * * *", "2026-11-27 16:59", "2026-11-27 17:00"},
		{"0 17 * * *", "2026-11-27 17:00", "2026-11-28 17:00"},
		{"*/15 * * * *", "2026-11-27 10:07", "2026-11-27 10:15"},
		{"0 9 * * 1-5", "2026-11-28 10:00", "2026-11-30 09:00"},
		{"30 8 1 * *", "2026-11-27 00:00", "2026-12-01 08:30"},
		{"0 0 29 2 *", "2026-03-01 00:00", "2028-02-29 00:00"},
		{"5,10 12 * * sun", "2026-11-27 00:00", "2026-11-29 12:05"},
		{"@daily", "2026-11-27 10:00", "2026-11-28 00:00"},
		{"@hourly", "2026-11-27 10:00", "2026-11-27 11:00"},
		{"0 12 13 * 5", "2026-11-27 13:00", "2026-12-04 12:00"},
	}
	for _, c := range cases {
		s, err := ParseCron(c.spec)
		if err != nil {
			t.Fatalf("%s: %v", c.spec, err)
		}
		if got := s.Next(at(c.from)); !got.Equal(at(c.want)) {
			t.Errorf("%s from %s: got %s want %s", c.spec, c.from, got.Format("2006-01-02 15:04"), c.want)
		}
	}
}

func TestCronInvalid(t *testing.T) {
	for _, spec := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8",
		"*/0 * * * *", "5-1 * * * *", "a * * * *", "0 0 31 2 *", "@weird", "1,,2 * * * *"} {
		if _, err := ParseCron(spec); err == nil {
			t.Errorf("%q: want error", spec)
		}
	}
}
