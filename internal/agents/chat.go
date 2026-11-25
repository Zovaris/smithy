package agents

import (
	"errors"
	"fmt"

	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/apiloop/tools"
	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/proof"
)

// APIChat is what one daemon session on api:<provider>/<model> needs: the provider, the tools
// under the --profile auto rules (Tools.Permit is the policy a person may overrule) and Hard,
// the secret rules no answer overrules.
type APIChat struct {
	Provider    apiloop.Provider
	Name, Model string
	Tools       apiloop.Tools
	Hard        apiloop.Tools
	Close       func()
}

// NewAPIChat builds an APIChat for target in cwd, with the same key, sandbox and tool rules as
// `smithy agent --backend api`.
func NewAPIChat(target, cwd string) (*APIChat, error) {
	name, model, err := apiloop.ParseTarget(target)
	if err != nil {
		return nil, err
	}
	if APIProvider == nil {
		return nil, errors.New("the api backend needs the harness build")
	}
	mode, err := SandboxMode()
	if err == nil && mode == "container" {
		err = errAPIContainer
	}
	if err != nil {
		return nil, fmt.Errorf("auto.sandbox: %v (fails closed)", err)
	}
	key := ""
	if svc := apiloop.Providers[name].Service; svc != "" {
		if key = auth.Stored(svc); key == "" {
			return nil, fmt.Errorf("no %s key; store one with: smithy auth set %s", name, svc)
		}
	}
	p, err := APIProvider(name, key, "")
	if err != nil {
		return nil, errors.New(proof.Redact(err.Error()))
	}
	set, err := apiTools(Options{Cwd: cwd}, model, mode, HostProbe())
	if err != nil {
		return nil, fmt.Errorf("auto.sandbox: %v (fails closed)", err)
	}
	hard := &tools.Set{Cwd: cwd, Model: model, Allow: []string{"Read", "Edit", "Write", "Glob", "Grep", "Bash(*)"}, Deny: SecretDenied(), Hosts: set.Hosts}
	return &APIChat{Provider: p, Name: name, Model: model, Hard: hard,
		Tools: withCodeMode(set, config.Get("agent.code_mode", "false") == "true"), Close: func() { set.Close() }}, nil
}
