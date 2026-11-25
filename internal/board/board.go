//go:build harness

// Package board is the built-in tracker of the harness build: tasks in the SQLite store, a
// `board` source for smithy auto, and a Linear sync by field ownership (Linear owns title,
// body, priority and labels; the harness owns state, claims, PRs and branch).
package board

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/linear"
	"github.com/Zovaris/smithy/internal/store"
)

// IntakeLabel marks a task planned from a chat idea: smithy auto skips it until a person reviews
// it and removes the label.
const IntakeLabel = "intake"

// States are the task states, in board order.
var States = []string{"todo", "in_progress", "blocked", "review", "done", "canceled"}

// linearType is the Linear workflow state type a harness state is pushed as.
var linearType = map[string]string{"todo": "unstarted", "in_progress": "started", "blocked": "started",
	"review": "started", "done": "completed", "canceled": "canceled"}

var clock = time.Now

// Task is one row of the tasks table. LinearKey (TK-12) is set for tasks synced from Linear.
type Task struct {
	ID           int64     `json:"id"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	State        string    `json:"state"`
	Priority     int       `json:"priority"`
	Labels       []string  `json:"labels"`
	BlockedBy    []int64   `json:"blocked_by"`
	PRs          []string  `json:"pr_urls"`
	Branch       string    `json:"branch"`
	ClaimedBy    string    `json:"claimed_by"`
	ClaimExpires time.Time `json:"claim_expires_at"`
	Created      time.Time `json:"created_at"`
	Updated      time.Time `json:"updated_at"`
	LinearID     string    `json:"linear_id,omitempty"`
	LinearKey    string    `json:"linear_key,omitempty"`
}

// Board is the tracker over an open store.
type Board struct{ db *sql.DB }

// New wraps st.
func New(st *store.Store) *Board { return &Board{db: st.DB()} }

// stampLayout is fixed width so stored times compare correctly as strings.
const stampLayout = "2006-01-02T15:04:05.000000000Z07:00"

func stamp(t time.Time) string { return t.UTC().Format(stampLayout) }

func js(v any) string {
	b, _ := json.Marshal(v)
	if string(b) == "null" {
		return "[]"
	}
	return string(b)
}

// Add inserts t as todo (Title required) and returns its id.
func (b *Board) Add(t Task) (int64, error) {
	if strings.TrimSpace(t.Title) == "" {
		return 0, errors.New("board: a task needs a title")
	}
	n := stamp(clock())
	res, err := b.db.Exec(`INSERT INTO tasks (title, body, priority, labels, blocked_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.Title, t.Body, t.Priority, js(t.Labels), js(t.BlockedBy), n, n)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const cols = `t.id, t.title, t.body, t.state, t.priority, t.labels, t.blocked_by, t.pr_urls, t.branch, t.claimed_by,
	t.claim_expires_at, t.created_at, t.updated_at, COALESCE(t.linear_id, ''), COALESCE(m.ext_key, '')`

const from = ` FROM tasks t LEFT JOIN task_map m ON m.task_id = t.id AND m.source = 'linear'`

func scan(r interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var labels, blocked, prs, exp, created, updated string
	if err := r.Scan(&t.ID, &t.Title, &t.Body, &t.State, &t.Priority, &labels, &blocked, &prs, &t.Branch, &t.ClaimedBy,
		&exp, &created, &updated, &t.LinearID, &t.LinearKey); err != nil {
		return t, err
	}
	_ = json.Unmarshal([]byte(labels), &t.Labels)
	_ = json.Unmarshal([]byte(blocked), &t.BlockedBy)
	_ = json.Unmarshal([]byte(prs), &t.PRs)
	t.ClaimExpires, _ = time.Parse(time.RFC3339Nano, exp)
	t.Created, _ = time.Parse(time.RFC3339Nano, created)
	t.Updated, _ = time.Parse(time.RFC3339Nano, updated)
	return t, nil
}

// Get returns task id.
func (b *Board) Get(id int64) (Task, error) {
	t, err := scan(b.db.QueryRow(`SELECT `+cols+from+` WHERE t.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, fmt.Errorf("board: no task %d", id)
	}
	return t, err
}

// List returns the tasks in state (all when empty), by priority then id. Linear priority 0 is
// "none", so it sorts last.
func (b *Board) List(state string) ([]Task, error) {
	q := `SELECT ` + cols + from
	var args []any
	if state != "" {
		if !slices.Contains(States, state) {
			return nil, fmt.Errorf("board: unknown state %q (known: %s)", state, strings.Join(States, ", "))
		}
		q += ` WHERE t.state = ?`
		args = append(args, state)
	}
	rows, err := b.db.Query(q+` ORDER BY CASE t.priority WHEN 0 THEN 99 ELSE t.priority END, t.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// linearOwned are the fields a Linear task takes only from Linear.
var linearOwned = map[string]bool{"title": true, "body": true, "priority": true, "labels": true}

// Set changes fields of task id: state, title, body, priority, labels (comma list), blocked_by
// (comma ids), branch, and pr (adds one url). Linear-owned fields of a synced task are refused.
func (b *Board) Set(id int64, f map[string]string) error {
	t, err := b.Get(id)
	if err != nil {
		return err
	}
	for k, v := range f {
		if t.LinearID != "" && linearOwned[k] {
			return fmt.Errorf("board: %s of %s is owned by Linear; edit it there", k, t.LinearKey)
		}
		switch k {
		case "state":
			if !slices.Contains(States, v) {
				return fmt.Errorf("board: unknown state %q (known: %s)", v, strings.Join(States, ", "))
			}
			t.State = v
		case "title":
			if strings.TrimSpace(v) == "" {
				return errors.New("board: a task needs a title")
			}
			t.Title = v
		case "body":
			t.Body = v
		case "priority":
			p, err := strconv.Atoi(v)
			if err != nil || p < 0 || p > 4 {
				return fmt.Errorf("board: priority is 0 (none) to 4 (low), Linear's scale, not %q", v)
			}
			t.Priority = p
		case "labels":
			t.Labels = split(v)
		case "blocked_by":
			t.BlockedBy = nil
			for _, x := range split(v) {
				n, err := strconv.ParseInt(strings.TrimPrefix(x, "B-"), 10, 64)
				if err != nil || n == id {
					return fmt.Errorf("board: blocked_by takes other task ids, not %q", x)
				}
				t.BlockedBy = append(t.BlockedBy, n)
			}
		case "branch":
			t.Branch = v
		case "pr":
			if !slices.Contains(t.PRs, v) {
				t.PRs = append(t.PRs, v)
			}
		default:
			return fmt.Errorf("board: unknown field %q", k)
		}
	}
	_, err = b.db.Exec(`UPDATE tasks SET state=?, title=?, body=?, priority=?, labels=?, blocked_by=?, branch=?, pr_urls=?, updated_at=? WHERE id=?`,
		t.State, t.Title, t.Body, t.Priority, js(t.Labels), js(t.BlockedBy), t.Branch, js(t.PRs), stamp(clock()), id)
	return err
}

func split(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// Claim gives task id to by for ttl, in one statement: it wins only when the task is
// unclaimed, its claim expired, or by already holds it (a renewal).
func (b *Board) Claim(id int64, by string, ttl time.Duration) error {
	if strings.TrimSpace(by) == "" || ttl <= 0 {
		return errors.New("board: a claim needs a holder and a positive ttl")
	}
	n := clock()
	res, err := b.db.Exec(`UPDATE tasks SET claimed_by=?, claim_expires_at=?, updated_at=? WHERE id=? AND (claimed_by='' OR claimed_by=? OR claim_expires_at < ?)`,
		by, stamp(n.Add(ttl)), stamp(n), id, by, stamp(n))
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		t, err := b.Get(id)
		if err != nil {
			return err
		}
		return fmt.Errorf("board: task %d is claimed by %s until %s", id, t.ClaimedBy, t.ClaimExpires.Local().Format(time.Kitchen))
	}
	return nil
}

// Release drops by's claim on task id.
func (b *Board) Release(id int64, by string) error {
	res, err := b.db.Exec(`UPDATE tasks SET claimed_by='', claim_expires_at='', updated_at=? WHERE id=? AND claimed_by=?`, stamp(clock()), id, by)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return fmt.Errorf("board: task %d is not claimed by %s", id, by)
	}
	return nil
}

// ready reports whether t is new work: todo, unclaimed (or claim expired), every blocker done or canceled.
func (b *Board) ready(t Task) (bool, error) {
	if t.State != "todo" || (t.ClaimedBy != "" && t.ClaimExpires.After(clock())) {
		return false, nil
	}
	for _, id := range t.BlockedBy {
		bt, err := b.Get(id)
		if err != nil {
			continue // a deleted blocker blocks nothing
		}
		if bt.State != "done" && bt.State != "canceled" {
			return false, nil
		}
	}
	return true, nil
}

// Sync pulls Linear changes since the stored cursor, then pushes harness-owned changes. Pull
// updates only Linear-owned fields of linked tasks (and only from a newer updatedAt) and imports
// unlinked issues in linear.states as todo; push moves each linked issue whose task state changed
// to the matching Linear state type and attaches new PR urls. Progress lines go to log.
func (b *Board) Sync(lin *linear.Source, log io.Writer) error {
	var cursor string
	_ = b.db.QueryRow(`SELECT cursor FROM sync_cursors WHERE source='linear'`).Scan(&cursor)
	changes, next, err := lin.Changes(cursor)
	if err != nil {
		return err
	}
	for _, c := range changes {
		if err := b.pull(lin, c, log); err != nil {
			return err
		}
	}
	if _, err := b.db.Exec(`INSERT INTO sync_cursors (source, cursor) VALUES ('linear', ?) ON CONFLICT(source) DO UPDATE SET cursor=excluded.cursor`, next); err != nil {
		return err
	}
	return b.push(lin, log)
}

// maxBody caps an imported Linear description; it ends up in a worker's prompt.
const maxBody = 64 << 10

func (b *Board) pull(lin *linear.Source, c linear.Change, log io.Writer) error {
	if len(c.Body) > maxBody {
		c.Body = c.Body[:maxBody]
	}
	n := stamp(clock())
	var id int64
	err := b.db.QueryRow(`SELECT task_id FROM task_map WHERE source='linear' AND ext_id=?`, c.ID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if !lin.NewWork(c.StateType) {
			return nil
		}
		tx, err := b.db.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		res, err := tx.Exec(`INSERT INTO tasks (title, body, priority, labels, created_at, updated_at, linear_id, linear_updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			c.Title, c.Body, c.Priority, js(c.Labels), n, n, c.ID, c.UpdatedAt)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO task_map (task_id, source, ext_id, ext_key, pushed_state) VALUES (?, 'linear', ?, ?, 'todo')`, id, c.ID, c.Identifier); err != nil {
			return err
		}
		fmt.Fprintf(log, "board: imported %s as B-%d\n", auto.Clean(c.Identifier), id)
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	res, err := b.db.Exec(`UPDATE tasks SET title=?, body=?, priority=?, labels=?, linear_updated_at=?, updated_at=? WHERE id=? AND linear_updated_at < ?`,
		c.Title, c.Body, c.Priority, js(c.Labels), c.UpdatedAt, n, id, c.UpdatedAt)
	if err != nil {
		return err
	}
	if k, _ := res.RowsAffected(); k > 0 {
		fmt.Fprintf(log, "board: B-%d updated from %s\n", id, auto.Clean(c.Identifier))
	}
	return nil
}

func (b *Board) push(lin *linear.Source, log io.Writer) error {
	rows, err := b.db.Query(`SELECT m.task_id, m.ext_id, m.ext_key, m.pushed_state, m.pushed_prs, t.state, t.pr_urls FROM task_map m JOIN tasks t ON t.id = m.task_id WHERE m.source='linear'`)
	if err != nil {
		return err
	}
	type row struct {
		id                                 int64
		ext, key, pushed, pPRs, state, prs string
	}
	var todo []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.ext, &r.key, &r.pushed, &r.pPRs, &r.state, &r.prs); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, r)
	}
	rows.Close()
	var errs []error
	for _, r := range todo {
		if r.state != r.pushed {
			if linearType[r.state] != linearType[r.pushed] {
				if err := lin.SetState(r.ext, linearType[r.state]); err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", r.key, err))
					continue
				}
				fmt.Fprintf(log, "board: %s moved to %s\n", auto.Clean(r.key), linearType[r.state])
			}
			if _, err := b.db.Exec(`UPDATE task_map SET pushed_state=? WHERE source='linear' AND ext_id=?`, r.state, r.ext); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		var have, pushed []string
		_ = json.Unmarshal([]byte(r.prs), &have)
		_ = json.Unmarshal([]byte(r.pPRs), &pushed)
		for _, u := range have {
			if slices.Contains(pushed, u) {
				continue
			}
			if err := lin.AttachPR(r.ext, u); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", r.key, err))
				break
			}
			pushed = append(pushed, u)
			if _, err := b.db.Exec(`UPDATE task_map SET pushed_prs=? WHERE source='linear' AND ext_id=?`, js(pushed), r.ext); err != nil {
				errs = append(errs, err)
				break
			}
			fmt.Fprintf(log, "board: %s linked %s\n", auto.Clean(r.key), auto.Clean(u))
		}
	}
	return errors.Join(errs...)
}

// LinearSync reports whether board.linear_sync is on.
func LinearSync() bool { return config.Get("board.linear_sync", "false") == "true" }

// Source is the `board` source of smithy auto.
type Source struct {
	b   *Board
	lin *linear.Source
}

// shared is the store the board source keeps open for the process: smithy auto serve builds
// its sources every cycle, so opening one per cycle would leak handles.
var shared struct {
	sync.Mutex
	path string
	st   *store.Store
}

func sharedStore() (*store.Store, error) {
	shared.Lock()
	defer shared.Unlock()
	p := store.DefaultPath()
	if shared.st != nil && shared.path == p {
		return shared.st, nil
	}
	st, err := store.Open(p)
	if err != nil {
		return nil, err
	}
	if shared.st != nil {
		shared.st.Close()
	}
	shared.path, shared.st = p, st
	return st, nil
}

func init() {
	auto.Sources["board"] = func(s auto.Settings) (auto.Source, error) {
		st, err := sharedStore()
		if err != nil {
			return nil, err
		}
		src := &Source{b: New(st)}
		if LinearSync() {
			if src.lin, err = linear.Open(s); err != nil {
				return nil, err
			}
		}
		return src, nil
	}
}

// Name is "board".
func (*Source) Name() string { return "board" }

// Poll syncs with Linear when board.linear_sync is on (a failed sync is a warning: the local
// board still works), then returns every ready task not held by IntakeLabel; since is ignored because a task becomes
// ready when another one closes, and the watcher dedupes by key.
func (s *Source) Poll(time.Time) ([]auto.Issue, error) {
	if s.lin != nil {
		if err := s.b.Sync(s.lin, io.Discard); err != nil {
			fmt.Fprintf(os.Stderr, "board: linear sync: %s\n", auto.Clean(err.Error()))
		}
	}
	ts, err := s.b.List("todo")
	if err != nil {
		return nil, err
	}
	var out []auto.Issue
	for _, t := range ts {
		if slices.Contains(t.Labels, IntakeLabel) {
			continue
		}
		if ok, err := s.b.ready(t); err != nil || !ok {
			continue
		}
		i := auto.Issue{Source: "board", Key: strconv.FormatInt(t.ID, 10), ID: fmt.Sprintf("B-%d", t.ID), Title: t.Title, Body: t.Body, Labels: t.Labels}
		if t.LinearKey != "" {
			i.URL = "linear:" + t.LinearKey
		}
		out = append(out, i)
	}
	return out, nil
}

// ParseID accepts 12 or B-12.
func ParseID(s string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(s)), "B-"), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("board: %q is not a task id (12 or B-12)", s)
	}
	return n, nil
}

// Report maps auto's states onto the board: started is in_progress, blocked is blocked,
// completed is review with a PR (it waits on the merge) and done without one.
func (s *Source) Report(id, state, pr string) error {
	n, err := ParseID(id)
	if err != nil {
		return err
	}
	f := map[string]string{}
	switch state {
	case "started":
		f["state"] = "in_progress"
	case "blocked":
		f["state"] = "blocked"
	case "completed":
		f["state"] = "done"
		if pr != "" {
			f["state"] = "review"
		}
	default:
		return fmt.Errorf("board: unknown report state %q", state)
	}
	if pr != "" {
		f["pr"] = pr
	}
	return s.b.Set(n, f)
}
