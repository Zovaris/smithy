//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Zovaris/smithy/internal/models"
)

func TestAgentModels(t *testing.T) {
	sv, url, _ := setupTest(t)
	var gotRefresh bool
	sv.Models = func(_ context.Context, agent string, refresh bool) models.List {
		gotRefresh = refresh
		return models.List{Agent: agent, Models: []models.Model{{ID: "m1", Default: true}}, Source: "test"}
	}
	res := do(t, "GET", url+"/v1/agents/api:openai/models?refresh=1", "", auth)
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var l models.List
	json.NewDecoder(res.Body).Decode(&l)
	if l.Agent != "api:openai" || len(l.Models) != 1 || !gotRefresh {
		t.Fatalf("got %+v refresh=%v", l, gotRefresh)
	}
}

func TestAgentModelsRejectsUnknownAgent(t *testing.T) {
	_, url, _ := setupTest(t)
	res := do(t, "GET", url+"/v1/agents/nope/models", "", auth)
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestAgentModelsNeedsAuth(t *testing.T) {
	_, url, _ := setupTest(t)
	res := do(t, "GET", url+"/v1/agents/claude/models", "", nil)
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("status %d", res.StatusCode)
	}
}
