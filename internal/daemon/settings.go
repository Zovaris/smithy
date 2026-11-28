//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	authstore "github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/connect"
	"github.com/Zovaris/smithy/internal/text"
)

// SettingRow is one known key with its effective value and where it comes from; secrets are masked.
type SettingRow struct {
	Key     string   `json:"key"`
	Value   string   `json:"value"`
	Src     string   `json:"src"`
	Default string   `json:"default"`
	Meaning string   `json:"meaning"`
	Secret  bool     `json:"secret,omitempty"`
	Kind    string   `json:"kind"`
	Options []string `json:"options,omitempty"`
}

// KeyRow is one stored API key, masked.
type KeyRow struct {
	Service string `json:"service"`
	Masked  string `json:"masked"`
}

// Settings is GET /v1/settings: every setting, the stored API keys and whether serve may launch.
type Settings struct {
	Settings []SettingRow `json:"settings"`
	Keys     []KeyRow     `json:"keys"`
	Serve    bool         `json:"serve"`
	Prompt   PromptState  `json:"prompt"`
}

func loadSettings() (Settings, error) {
	out := Settings{Settings: []SettingRow{}, Keys: []KeyRow{}, Prompt: PromptStatus()}
	eff := config.Snapshot()
	for _, k := range config.Known {
		v, src, err := eff(k.Key)
		if err != nil {
			v, src = "", "unset"
		}
		r := SettingRow{Key: k.Key, Value: text.Clean(v), Src: src, Default: k.Default, Meaning: k.Meaning, Secret: config.Secret(k.Key)}
		if r.Secret && r.Value != "" {
			r.Value = "****"
		}
		r.Kind, r.Options = config.KindOf(k.Key)
		out.Settings = append(out.Settings, r)
	}
	lines, err := authstore.List()
	if err != nil {
		return out, err
	}
	for _, l := range lines {
		svc, rest, _ := strings.Cut(l, " ")
		masked, _, _ := strings.Cut(rest, " ")
		out.Keys = append(out.Keys, KeyRow{Service: svc, Masked: masked})
	}
	sort.Slice(out.Keys, func(i, j int) bool { return out.Keys[i].Service < out.Keys[j].Service })
	paused, _, err := eff("auto.paused")
	out.Serve = err != nil || paused != "true"
	return out, nil
}

// settingsGuard lets only this machine's daemon token write or read settings, from no other origin.
func settingsGuard(w http.ResponseWriter, r *http.Request) bool {
	if !localOnly(w, r) {
		return false
	}
	if r.Method == http.MethodGet {
		return true
	}
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return false
	}
	return true
}

func (sv *Server) listSettings(w http.ResponseWriter, r *http.Request) {
	if !settingsGuard(w, r) {
		return
	}
	s, err := loadSettings()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": text.Clean(err.Error())})
		return
	}
	writeJSON(w, 200, s)
}

func readBody(w http.ResponseWriter, r *http.Request, v any, shape string) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(v); err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be " + shape})
		return false
	}
	return true
}

// putSetting saves a key in the user's file (the daemon has no repo of its own); secrets go through /v1/keys.
func (sv *Server) putSetting(w http.ResponseWriter, r *http.Request) {
	if !settingsGuard(w, r) {
		return
	}
	key := r.PathValue("key")
	if config.Secret(key) {
		writeJSON(w, 400, map[string]string{"error": key + " is a secret: store it with PUT /v1/keys/{service}"})
		return
	}
	var req struct{ Value string }
	if !readBody(w, r, &req, `{"value":…}`) {
		return
	}
	_, saved, note, err := config.Apply(false, key, req.Value)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": text.Clean(err.Error())})
		return
	}
	writeJSON(w, 200, map[string]string{"key": key, "value": saved, "note": note})
}

func (sv *Server) deleteSetting(w http.ResponseWriter, r *http.Request) {
	if !settingsGuard(w, r) {
		return
	}
	key := r.PathValue("key")
	if config.Secret(key) {
		writeJSON(w, 400, map[string]string{"error": key + " is a secret: remove it with DELETE /v1/keys/{service}"})
		return
	}
	_, found, err := config.Remove(false, key)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": text.Clean(err.Error())})
		return
	}
	writeJSON(w, 200, map[string]bool{"found": found})
}

func (sv *Server) putKey(w http.ResponseWriter, r *http.Request) {
	if !settingsGuard(w, r) {
		return
	}
	var req struct{ Key string }
	if !readBody(w, r, &req, `{"key":…}`) {
		return
	}
	if err := authstore.Set(r.PathValue("service"), req.Key); err != nil {
		writeJSON(w, 400, map[string]string{"error": text.Clean(err.Error())})
		return
	}
	writeJSON(w, 200, map[string]string{"masked": authstore.Mask(strings.TrimSpace(req.Key))})
}

func (sv *Server) deleteKey(w http.ResponseWriter, r *http.Request) {
	if !settingsGuard(w, r) {
		return
	}
	found, err := authstore.Remove(r.PathValue("service"))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": text.Clean(err.Error())})
		return
	}
	writeJSON(w, 200, map[string]bool{"found": found})
}

// putServe turns serve mode on or off: off sets auto.paused, so serve launches nothing new.
func (sv *Server) putServe(w http.ResponseWriter, r *http.Request) {
	if !settingsGuard(w, r) {
		return
	}
	var req struct {
		On *bool `json:"on"`
	}
	if !readBody(w, r, &req, `{"on":true|false}`) {
		return
	}
	if req.On == nil {
		writeJSON(w, 400, map[string]string{"error": `body must be {"on":true|false}`})
		return
	}
	paused := "true"
	if *req.On {
		paused = "false"
	}
	if _, _, _, err := config.Apply(false, "auto.paused", paused); err != nil {
		writeJSON(w, 500, map[string]string{"error": text.Clean(err.Error())})
		return
	}
	writeJSON(w, 200, map[string]bool{"on": *req.On})
}

// doctorBusy keeps doctor runs to one at a time.
var doctorBusy sync.Mutex

// runDoctor runs this binary's `smithy doctor` with a 20 s cap.
func runDoctor(ctx context.Context) []string {
	exe, err := os.Executable()
	if err != nil {
		return []string{"doctor: " + text.Clean(err.Error())}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "doctor").CombinedOutput()
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		lines = append(lines, text.Clean(l))
	}
	if err != nil {
		lines = append(lines, "doctor: "+text.Clean(err.Error()))
	}
	return lines
}

func (sv *Server) doctor(w http.ResponseWriter, r *http.Request) {
	if !settingsGuard(w, r) {
		return
	}
	if !doctorBusy.TryLock() {
		writeJSON(w, 409, map[string]string{"error": "smithy doctor is already running; try again when it ends"})
		return
	}
	defer doctorBusy.Unlock()
	run := sv.Doctor
	if run == nil {
		run = runDoctor
	}
	writeJSON(w, 200, map[string][]string{"lines": run(r.Context())})
}

// ConnectTestResult is POST /v1/connect/test: one line per channel tried, or why none was.
type ConnectTestResult struct {
	Results []string `json:"results"`
	Error   string   `json:"error,omitempty"`
}

// connectTest sends a test message through each chat connector built from the current config
// (so a settings change is tried without a restart), to its notifications and chat channels.
func (sv *Server) connectTest(w http.ResponseWriter, r *http.Request) {
	if !settingsGuard(w, r) {
		return
	}
	build := sv.Connectors
	if build == nil {
		build = connect.FromConfig
	}
	cs, err := build()
	out := ConnectTestResult{Results: []string{}}
	if err != nil {
		out.Error = text.Clean(err.Error())
	}
	if len(cs) == 0 && out.Error == "" {
		out.Error = "no chat connector is set up (connect.<telegram|slack|discord>.notify or .chat, and its token)"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	for _, l := range (&connect.Hub{Conns: cs}).Test(ctx) {
		out.Results = append(out.Results, text.Clean(l))
	}
	writeJSON(w, 200, out)
}
