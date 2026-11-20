package platform

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/prompts"
	types "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/runtime"
)

const (
	ToolConnections     = "list_connections"
	ToolConnect         = "connect_connection"
	ToolWorkspacePrompt = "set_workspace_prompt"
)

func (p *Platform) builderPerson(req runtime.Request) (auth.Principal, error) {
	if !p.isHelper(req.Agent()) {
		return auth.Principal{}, ErrBuilder
	}
	_, person := p.operationPlace(req)
	if person == "" {
		return auth.Principal{}, auth.ErrForbidden
	}
	return p.byPolicy(req.Tenant, person), nil
}

// Personal status and consent links may reach only the initiating person in
// their current private console session, even if the builder serves others too.
func (p *Platform) builderConnectionPerson(req runtime.Request) (auth.Principal, error) {
	who, err := p.builderPerson(req)
	if err != nil || !who.Can(auth.RunsWrite) {
		return auth.Principal{}, auth.ErrForbidden
	}
	run, person, err := p.ownPrivateConversation(req)
	if err != nil || person != who.Subject || !p.Seer(who).Run(run) {
		return auth.Principal{}, auth.ErrForbidden
	}
	return who, nil
}

func (p *Platform) builderConnectionsTool(req runtime.Request, connect bool) types.Tool {
	name, description := ToolConnections, "List available connection presets and the current person's connection status. Never return credentials."
	required := []string{}
	params := map[string]string{}
	if connect {
		name, description = ToolConnect, "Prepare a secure connection link for the current person. They must open the link and explicitly authorize access in the browser."
		required = []string{"provider"}
		params["provider"] = "Connection preset id"
	}
	return &types.ToolFunc{Def: types.ToolDef{Name: name, Description: description, Parameters: schema(required, params)}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		who, err := p.builderConnectionPerson(req)
		if err != nil {
			return "", err
		}
		var out any
		if connect {
			out, err = p.BeginConnection(ctx, who, arg(args, "provider"))
		} else {
			out, err = p.ConnectionPresets(who)
		}
		if err != nil {
			return "", err
		}
		// Discovery can take time. A new participant or revoked reader must not
		// receive status, endpoints or a consent link read before that change.
		if current, err := p.builderConnectionPerson(req); err != nil || current.Subject != who.Subject {
			return "", auth.ErrForbidden
		}
		raw, err := json.Marshal(out)
		return string(raw), err
	}}
}

// sharedPromptApproval is the approval the tools that change shared
// instructions declare in the registry: an administrator other than the
// person asking approves every call.
func sharedPromptApproval(prompt func(ApprovalCall) string) ToolApproval {
	return ToolApproval{Mode: ApprovalAlways, Approvers: []string{"role:admin"}, SelfApproval: false, Prompt: prompt}
}

// workspacePromptPrompt words the approval of a set_workspace_prompt call.
func workspacePromptPrompt(ApprovalCall) string {
	return "Apply these shared instructions after every agent's sampled evals pass?"
}
func (p *Platform) builderWorkspacePromptTool(req runtime.Request) types.Tool {
	return &types.ToolFunc{Def: types.ToolDef{Name: ToolWorkspacePrompt, Description: "Change versioned shared instructions after administrator approval and sampled eval gates. Provide the full prompts config as JSON. Existing runs retain their pinned versions.", Parameters: schema([]string{"config"}, map[string]string{"config": "JSON with platform,workspace,slack_word_budget"})}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
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
	}}
}
