//go:build harness

package daemon

import (
	"context"
	"errors"
	"os"
)

// Asker is smithy mcp's ask tool backend: the running daemon records the question and holds the answer.
// Session, when set, is the daemon session the questions come from.
type Asker struct {
	C       *Client
	Session string
}

// LocalAsker connects to the running local daemon, or fails when none runs.
func LocalAsker() (*Asker, error) {
	i := Running()
	if i == nil {
		return nil, errors.New("no smithy daemon is running")
	}
	tok, err := LoadToken(TokenFile(), false)
	if err != nil {
		return nil, err
	}
	return &Asker{C: &Client{Base: i.URL(), Token: tok}, Session: os.Getenv("SMITHY_SESSION")}, nil
}

// Ask records the decision on the daemon.
func (a *Asker) Ask(ctx context.Context, id, question string, options []string) error {
	return a.C.Ask(ctx, id, question, options)
}

// AskMulti records a decision whose options may be picked together.
func (a *Asker) AskMulti(ctx context.Context, id, question string, options []string, multi bool) error {
	return a.C.AskIn(ctx, a.Session, id, question, options, multi)
}

// Outcome reads the daemon's recorded answer.
func (a *Asker) Outcome(ctx context.Context, id string) (string, bool, error) {
	d, err := a.C.Decision(ctx, id)
	if err != nil || d.Outcome == nil {
		return "", false, err
	}
	return d.Outcome.Answer, true, nil
}
