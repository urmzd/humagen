package platform

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/prompts"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native"
	types "github.com/urmzd/saige/agent/types"
)

const (
	ToolWorkspacePrompt = "set_workspace_prompt"
)

func (p *Platform) builderPerson(req runtime.Request) (auth.Principal, error) {
	if legacy.BuiltinAgent(req.Agent()) != BuiltinBuilder {
		return auth.Principal{}, ErrBuilder
	}
	_, person := p.operationPlace(req)
	if person == "" {
		return auth.Principal{}, auth.ErrForbidden
	}
	return p.byPolicy(req.Tenant, person), nil
}

type workspaceApproval struct{ *types.ToolFunc }

func (t *workspaceApproval) Approval(types.ToolCallPart) (native.ApprovalRequest, bool) {
	return native.ApprovalRequest{Prompt: "Apply these shared instructions after every agent's sampled evals pass?", Approvers: []string{"role:admin"}, SelfApproval: false}, true
}
func (p *Platform) builderWorkspacePromptTool(req runtime.Request) types.Tool {
	return &workspaceApproval{&types.ToolFunc{Def: types.ToolDef{Name: ToolWorkspacePrompt, Description: "Change versioned shared instructions after administrator approval and sampled eval gates. Provide the full prompts config as JSON. Existing runs retain their pinned versions.", Parameters: schema([]string{"config"}, map[string]string{"config": "JSON with platform,workspace,slack_word_budget"})}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		who, err := p.builderPerson(req)
		if err != nil {
			return "", err
		}
		if !who.Can(auth.AgentsWrite) {
			return "", auth.ErrForbidden
		}
		var cfg prompts.Config
		dec := json.NewDecoder(strings.NewReader(arg(args, "config")))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&cfg); err != nil {
			return "", err
		}
		settings := p.Settings(req.Tenant)
		settings.Prompts = cfg
		if err = p.SetSettingsBy(ctx, who, settings); err != nil {
			return "", err
		}
		return "Shared instructions saved; existing runs keep their pinned versions.", nil
	}}}
}
