package events

import (
	"time"

	"github.com/Zovaris/smithy/internal/usage"
)

// Tokens is the token use of a set of "usage" events: the sum and the split per agent and model
// ("unknown" when an event did not record one).
type Tokens struct {
	Total  usage.Usage            `json:"total"`
	Agents map[string]usage.Usage `json:"agents"`
	Models map[string]usage.Usage `json:"models"`
	Runs   int                    `json:"runs"`
}

// Report is the token use of one session, today, the last 7 days and everything in the log.
type Report struct {
	Session Tokens   `json:"session"`
	Today   Tokens   `json:"today"`
	Week    Tokens   `json:"week"`
	Month   Tokens   `json:"month"`
	All     Tokens   `json:"all"`
	Days    []DayUse `json:"days"` // the last 30 local days, oldest first, today last
}

// DayUse is one day's token use: its date (2006-01-02), runs, tokens and known cost.
type DayUse struct {
	Day    string  `json:"day"`
	Runs   int     `json:"runs"`
	Tokens int64   `json:"tokens"`
	Cost   float64 `json:"cost"`
}

// DaysShown is how many days Report.Days holds.
const DaysShown = 30

func (t *Tokens) add(e Event) {
	if t.Agents == nil {
		t.Agents, t.Models = map[string]usage.Usage{}, map[string]usage.Usage{}
	}
	t.Runs++
	t.Total = t.Total.Add(*e.Usage)
	a, m := orUnknown(e.Agent), orUnknown(e.Model)
	t.Agents[a] = t.Agents[a].Add(*e.Usage)
	t.Models[m] = t.Models[m].Add(*e.Usage)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// Tally sums the "usage" events in es: session is a daemon session id ("" for none); the week is
// the last 7 local days, today included, like Spend.
func Tally(es []Event, now time.Time, session string) Report {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	week := day.AddDate(0, 0, -6)
	month := day.AddDate(0, 0, -(DaysShown - 1))
	r := Report{Days: make([]DayUse, DaysShown)}
	for i := range r.Days {
		r.Days[i].Day = month.AddDate(0, 0, i).Format(time.DateOnly)
	}
	for _, e := range es {
		if e.Kind != "usage" || e.Usage == nil {
			continue
		}
		r.All.add(e)
		if !e.T.Before(month) {
			r.Month.add(e)
			lt := e.T.In(now.Location())
			d := time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, now.Location())
			if i := int(d.Sub(month).Hours()/24 + 0.5); i >= 0 && i < DaysShown {
				u := *e.Usage
				r.Days[i].Runs++
				r.Days[i].Tokens += u.Input + u.Output + u.CacheRead + u.CacheWrite
				if u.CostUSD != nil {
					r.Days[i].Cost += *u.CostUSD
				}
			}
		}
		if !e.T.Before(week) {
			r.Week.add(e)
		}
		if !e.T.Before(day) {
			r.Today.add(e)
		}
		if session != "" && e.Session == session {
			r.Session.add(e)
		}
	}
	return r
}
