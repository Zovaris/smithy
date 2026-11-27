//go:build harness

package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/text"
)

// KindSessionGoal records a session's goal and where its loop stands ({goal, state, turns, base}).
const KindSessionGoal = "session_goal"

// A goal's state: "" while the daemon still continues the session toward it.
const (
	GoalMet    = "met"
	GoalAsked  = "asked"
	GoalCapped = "capped"
)

// MaxGoal bounds a goal's text.
const MaxGoal = 4 << 10

const (
	goalMetMark     = "GOAL MET:"
	goalBlockedMark = "GOAL BLOCKED:"
	goalContinue    = "Continue toward the goal."
	goalBy          = "smithy"
)

type goalEvent struct {
	Goal  string  `json:"goal"`
	State string  `json:"state,omitempty"`
	Turns int     `json:"turns,omitempty"`
	Base  float64 `json:"base,omitempty"`
}

func goalPrompt(goal, msg string) string {
	return "Session goal (set by the user; only the user changes it):\n" + goal +
		"\n\nDone when the goal is met and you can show it. End your reply with a line `" + goalMetMark +
		" <evidence: commands run and their results>` once it is met, or `" + goalBlockedMark +
		" <question>` when you need the user. Without either line smithy sends you another turn.\n\n---\n\n" + msg
}

func goalMark(reply string) string {
	for _, ln := range strings.Split(reply, "\n") {
		ln = strings.TrimSpace(strings.Trim(strings.TrimSpace(ln), "`*"))
		switch {
		case strings.HasPrefix(ln, goalMetMark):
			return GoalMet
		case strings.HasPrefix(ln, goalBlockedMark):
			return GoalAsked
		}
	}
	return ""
}

func costOf(l *live) float64 {
	if l.s.Usage == nil || l.s.Usage.CostUSD == nil {
		return 0
	}
	return *l.s.Usage.CostUSD
}

func (sv *Server) goalCaps() (int, float64) {
	turns, cost := sv.GoalMaxTurns, sv.GoalMaxCost
	if turns == 0 {
		turns, _ = strconv.Atoi(config.Get("session.goal_max_turns", "10"))
	}
	if cost == 0 {
		cost, _ = strconv.ParseFloat(config.Get("session.goal_max_cost", "5"), 64)
	}
	return turns, cost
}

// SetSessionGoal pins goal on session id ("" clears it). Only a person sets it, through the API;
// the agent has no way to. A set goal restarts the loop's turn and cost count.
func (sv *Server) SetSessionGoal(id, goal string) (Session, error) {
	goal = strings.TrimSpace(text.Clean(goal))
	if len(goal) > MaxGoal {
		return Session{}, fmt.Errorf("%w: goal must be at most %d bytes", errBadRequest, MaxGoal)
	}
	sv.mu.Lock()
	defer sv.mu.Unlock()
	l := sv.sessions[id]
	if l == nil {
		return Session{}, errNoSession
	}
	if err := sv.setGoalLocked(l, goalEvent{Goal: goal, Base: costOf(l)}); err != nil {
		return Session{}, err
	}
	return l.s, nil
}

func (sv *Server) setGoalLocked(l *live, g goalEvent) error {
	if _, err := sv.appendLocked(KindSessionGoal, l.s.ID, g); err != nil {
		return err
	}
	l.s.Goal, l.s.GoalState, l.s.GoalTurns, l.goalBase = g.Goal, g.State, g.Turns, g.Base
	return nil
}

func (sv *Server) applyGoal(l *live, data []byte) {
	var g goalEvent
	if json.Unmarshal(data, &g) == nil {
		l.s.Goal, l.s.GoalState, l.s.GoalTurns, l.goalBase = g.Goal, g.State, g.Turns, g.Base
	}
}

// goalNextLocked runs after a turn that ended cleanly with nothing queued: it reports whether it
// queued the next turn toward the goal. It stops on the agent's mark or at a cap.
func (sv *Server) goalNextLocked(l *live, reply string) bool {
	if l.s.Goal == "" || l.s.GoalState != "" {
		return false
	}
	g := goalEvent{Goal: l.s.Goal, Turns: l.s.GoalTurns, Base: l.goalBase}
	maxTurns, maxCost := sv.goalCaps()
	switch {
	case goalMark(reply) != "":
		g.State = goalMark(reply)
	case g.Turns >= maxTurns || (maxCost > 0 && costOf(l)-g.Base >= maxCost):
		g.State = GoalCapped
	default:
		g.Turns++
	}
	if sv.setGoalLocked(l, g) != nil || g.State != "" {
		return false
	}
	return sv.queueLocked(l, goalContinue, goalBy) == nil
}

// goalResumeLocked restarts a stopped goal loop when a person sends the next message.
func (sv *Server) goalResumeLocked(l *live, by string) {
	if by == goalBy || l.s.Goal == "" || l.s.GoalState == "" {
		return
	}
	_ = sv.setGoalLocked(l, goalEvent{Goal: l.s.Goal, Base: costOf(l)})
}

func (sv *Server) session(id string) (Session, bool) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	l := sv.sessions[id]
	if l == nil {
		return Session{}, false
	}
	return l.s, true
}

func (sv *Server) setSessionGoal(w http.ResponseWriter, r *http.Request) {
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var req struct {
		Goal string `json:"goal"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxGoal+1<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"goal\":…}"})
		return
	}
	s, err := sv.SetSessionGoal(r.PathValue("id"), req.Goal)
	if err != nil {
		sessionErr(w, err)
		return
	}
	writeJSON(w, 200, s)
}
