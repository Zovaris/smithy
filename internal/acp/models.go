package acp

import (
	"io"
	"slices"
)

// Model is one model an agent offers for its sessions.
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ModelList is what session/new reported: the models on offer and the one a session starts with.
type ModelList struct {
	Models  []Model
	Current string
}

type configOption struct {
	ID           string `json:"id"`
	Category     string `json:"category"`
	CurrentValue string `json:"currentValue"`
	Options      []struct {
		Value string `json:"value"`
		Name  string `json:"name"`
	} `json:"options"`
}

type nopSink struct{}

func (nopSink) Text(string)              {}
func (nopSink) Tool(Tool)                {}
func (nopSink) Permit(Permission) string { return "" }

// Models opens a session in cwd and returns the models its session/new answer lists, read from
// the model config option or the older models field; empty when the agent reports none.
func Models(from io.Reader, to io.Writer, cwd string) (ModelList, error) {
	var sess struct {
		ConfigOptions []configOption `json:"configOptions"`
		Models        *struct {
			Current   string `json:"currentModelId"`
			Available []struct {
				ID   string `json:"modelId"`
				Name string `json:"name"`
			} `json:"availableModels"`
		} `json:"models"`
	}
	var out ModelList
	if err := newClient(from, to, nopSink{}).open(cwd, &sess); err != nil {
		return out, err
	}
	if i := slices.IndexFunc(sess.ConfigOptions, func(o configOption) bool { return o.Category == "model" || o.ID == "model" }); i >= 0 {
		o := sess.ConfigOptions[i]
		out.Current = o.CurrentValue
		for _, v := range o.Options {
			if v.Value != "" {
				out.Models = append(out.Models, Model{ID: v.Value, Name: v.Name})
			}
		}
		return out, nil
	}
	if m := sess.Models; m != nil {
		out.Current = m.Current
		for _, v := range m.Available {
			if v.ID != "" {
				out.Models = append(out.Models, Model{ID: v.ID, Name: v.Name})
			}
		}
	}
	return out, nil
}
