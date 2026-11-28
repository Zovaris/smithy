package skills

import (
	"reflect"
	"testing"

	"github.com/Zovaris/smithy/internal/agents/catalog"
)

func TestInstallHarnessesFromCatalog(t *testing.T) {
	if want := catalog.Names(func(catalog.Agent) bool { return true }); !reflect.DeepEqual(installHarnesses, want) {
		t.Errorf("installHarnesses = %v, want %v", installHarnesses, want)
	}
	if harnessName("claude") != "claude-cli" || harnessName("pi") != "pi" {
		t.Error("harnessName")
	}
}
