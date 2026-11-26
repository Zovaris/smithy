//go:build harness

package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/Zovaris/smithy/internal/layout"
)

// Project is one folder new sessions on this daemon can start in.
type Project struct {
	Dir  string `json:"dir"`
	Name string `json:"name"`
}

// ProjectList is GET /v1/projects: this daemon's projects in the order added, and the default.
type ProjectList struct {
	Projects []Project `json:"projects"`
	Default  string    `json:"default,omitempty"`
}

// DirList is GET /v1/fs: the folders inside Dir, hidden ones left out.
type DirList struct {
	Dir    string   `json:"dir"`
	Parent string   `json:"parent,omitempty"`
	Dirs   []string `json:"dirs"`
}

var projMu sync.Mutex

func projectsFile() string { return filepath.Join(layout.Home(), "projects.json") }

// Projects reads this daemon's saved projects; a missing file is an empty list.
func Projects() ProjectList {
	projMu.Lock()
	defer projMu.Unlock()
	return loadProjects()
}

func loadProjects() ProjectList {
	var l ProjectList
	if b, err := os.ReadFile(projectsFile()); err == nil && json.Unmarshal(b, &l) != nil {
		l = ProjectList{}
	}
	if l.Projects == nil {
		l.Projects = []Project{}
	}
	return l
}

func saveProjects(l ProjectList) error {
	b, _ := json.MarshalIndent(l, "", "  ")
	f := projectsFile()
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}

// AddProject saves dir as a project (once) and, with def, makes it the default.
func AddProject(dir string, def bool) (ProjectList, error) {
	d, err := cleanDir(dir)
	if err != nil {
		return ProjectList{}, err
	}
	projMu.Lock()
	defer projMu.Unlock()
	l := loadProjects()
	if !slices.ContainsFunc(l.Projects, func(p Project) bool { return p.Dir == d }) {
		l.Projects = append(l.Projects, Project{Dir: d, Name: filepath.Base(d)})
	}
	if def {
		l.Default = d
	}
	return l, saveProjects(l)
}

// RemoveProject forgets dir; sessions already started there keep it.
func RemoveProject(dir string) (ProjectList, error) {
	d := filepath.Clean(dir)
	if real, err := filepath.EvalSymlinks(d); err == nil {
		d = real
	}
	projMu.Lock()
	defer projMu.Unlock()
	l := loadProjects()
	l.Projects = slices.DeleteFunc(l.Projects, func(p Project) bool { return p.Dir == d })
	if l.Default == d {
		l.Default = ""
	}
	return l, saveProjects(l)
}

// ListDirs is the file picker's view of dir on this machine ("" is the home folder).
func ListDirs(dir string) (DirList, error) {
	if dir == "" {
		dir = layout.UserHome()
	}
	d, err := cleanDir(dir)
	if err != nil {
		return DirList{}, err
	}
	es, err := os.ReadDir(d)
	if err != nil {
		return DirList{}, errors.New("dir: not readable")
	}
	l := DirList{Dir: d, Dirs: []string{}}
	if p := filepath.Dir(d); p != d {
		l.Parent = p
	}
	for _, e := range es {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() || (e.Type()&os.ModeSymlink != 0 && isDir(filepath.Join(d, e.Name()))) {
			l.Dirs = append(l.Dirs, e.Name())
		}
	}
	sort.Strings(l.Dirs)
	return l, nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func (sv *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, Projects())
}

func (sv *Server) addProject(w http.ResponseWriter, r *http.Request) {
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var req struct {
		Dir     string `json:"dir"`
		Default bool   `json:"default"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"dir\":…,\"default\":true|false}"})
		return
	}
	l, err := AddProject(req.Dir, req.Default)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, l)
}

func (sv *Server) removeProject(w http.ResponseWriter, r *http.Request) {
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var req struct {
		Dir string `json:"dir"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.Dir == "" {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"dir\":…}"})
		return
	}
	l, err := RemoveProject(req.Dir)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "projects not saved"})
		return
	}
	writeJSON(w, 200, l)
}

func (sv *Server) browse(w http.ResponseWriter, r *http.Request) {
	l, err := ListDirs(r.URL.Query().Get("dir"))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, l)
}
