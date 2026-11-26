//go:build harness

package daemon

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"

	"github.com/Zovaris/smithy/internal/tasks"
)

// TaskBoard is GET /v1/tasks: a project's task lists, the files `smithy tasks` reads.
type TaskBoard struct {
	Project  string     `json:"project"`
	Statuses []string   `json:"statuses"`
	Lists    []TaskList `json:"lists"`
}

// TaskList is one task dir and its tasks in NN order.
type TaskList struct {
	Dir   string     `json:"dir"`
	Name  string     `json:"name"`
	Tasks []TaskCard `json:"tasks"`
}

// TaskCard is one task file.
type TaskCard struct {
	File     string   `json:"file"`
	NN       string   `json:"nn"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	PR       string   `json:"pr,omitempty"`
	Blockers []string `json:"blockers"`
}

func card(t tasks.Task) TaskCard {
	c := TaskCard{File: t.File, NN: t.NN, Title: t.Title, Status: t.Status, Blockers: t.Blockers}
	if t.PR != "-" {
		c.PR = t.PR
	}
	if c.Blockers == nil {
		c.Blockers = []string{}
	}
	return c
}

// boardOf reads the task lists of the project at dir ("" is the default project, else the first).
func boardOf(dir string) TaskBoard {
	if dir == "" {
		l := Projects()
		dir = l.Default
		if dir == "" && len(l.Projects) > 0 {
			dir = l.Projects[0].Dir
		}
	}
	b := TaskBoard{Project: dir, Statuses: tasks.Statuses, Lists: []TaskList{}}
	if dir == "" {
		return b
	}
	for _, d := range tasks.DirsIn(dir) {
		l := TaskList{Dir: d, Name: filepath.Base(d), Tasks: []TaskCard{}}
		for _, t := range tasks.Load(d) {
			l.Tasks = append(l.Tasks, card(t))
		}
		b.Lists = append(b.Lists, l)
	}
	return b
}

// knownTaskFile reports whether file is a task file inside a task dir of one of this daemon's
// projects: the board moves cards there and nowhere else.
func knownTaskFile(file string) bool {
	if !filepath.IsAbs(file) || !tasks.IsTaskFile(filepath.Base(file)) {
		return false
	}
	f := filepath.Clean(file)
	if real, err := filepath.EvalSymlinks(f); err == nil {
		f = real
	}
	for _, p := range Projects().Projects {
		if slices.Contains(tasks.DirsIn(p.Dir), filepath.Dir(f)) {
			return true
		}
	}
	return false
}

func (sv *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, boardOf(r.URL.Query().Get("project")))
}

// moveTask is PUT /v1/tasks/status: a card moved on the board, written to the task file's Status line.
func (sv *Server) moveTask(w http.ResponseWriter, r *http.Request) {
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var req struct {
		File   string `json:"file"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"file\":…,\"status\":…}"})
		return
	}
	if !slices.Contains(tasks.Statuses, req.Status) {
		writeJSON(w, 400, map[string]string{"error": "status must be one of todo, doing, done, dropped"})
		return
	}
	if !knownTaskFile(req.File) {
		writeJSON(w, 400, map[string]string{"error": "not a task file of a project on this daemon"})
		return
	}
	t, err := tasks.Set(req.File, []string{"Status=" + req.Status})
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, card(t))
}
