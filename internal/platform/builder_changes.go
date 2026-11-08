package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/prompts"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native"
	types "github.com/urmzd/saige/agent/types"
)

const (
	ToolPreviewChange = "preview_agent_change"
	ToolApplyChange   = "apply_agent_change"
	ToolRecordEval    = "add_recorded_eval_case"
)

func (p *Platform) builderPreviewChangeTool(req runtime.Request) types.Tool {
	return &types.ToolFunc{Def: types.ToolDef{Name: ToolPreviewChange, Description: "Prepare a change card before approval: show prompt changes, tools and deterministic eval scores without saving the agent. Apply its returned id only after the person approves.", Parameters: schema([]string{"name", "system_prompt"}, map[string]string{"name": "Agent name", "description": "Description", "system_prompt": "Proposed complete instructions", "tools": "Complete comma-separated tools. Include person_tools only when the person requests use of their separately authorized and reviewed connections; omitted means no personal tools."})}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		if legacy.BuiltinAgent(req.Agent()) != BuiltinBuilder {
			return "", ErrBuilder
		}
		candidate, before, _, err := p.prepareBuilderAgent(ctx, req.Tenant, args)
		if err != nil {
			return "", err
		}
		placement, err := p.builderPlacementPreview(req, candidate)
		if err != nil {
			return "", err
		}
		_, gateErr := p.checkAgentEvals(ctx, req.Tenant, before.Spec, candidate)
		changes, err := p.Evals.Changes(req.Tenant, candidate.Name)
		if err != nil {
			return "", err
		}
		slices.SortFunc(changes, func(a, b evals.Change) int { return b.At.Compare(a.At) })
		for _, c := range changes {
			if c.AfterHash == candidate.Hash() {
				out := struct {
					builderPlacements
					Change evals.Change `json:"change"`
					Before string       `json:"before_prompt"`
					After  string       `json:"after_prompt"`
					Tools  []string     `json:"tools"`
				}{placement, c, before.Spec.SystemPrompt, candidate.SystemPrompt, candidate.Tools}
				raw, err := json.Marshal(out)
				return string(raw), err
			}
		}
		return "", gateErr
	}}
}

type changeApproval struct {
	*types.ToolFunc
	prompt func(types.ToolCallPart) string
}

func (t *changeApproval) Approval(call types.ToolCallPart) (native.ApprovalRequest, bool) {
	return native.ApprovalRequest{Prompt: t.prompt(call), Approvers: []string{"role:admin", "role:manager"}, SelfApproval: true}, true
}
func (p *Platform) builderApplyChangeTool(req runtime.Request) types.Tool {
	prompt := func(call types.ToolCallPart) string {
		c, err := p.Evals.Change(req.Tenant, arg(call.Arguments, "name"), arg(call.Arguments, "change"))
		if err != nil {
			return "Approve this agent change?"
		}
		action := "Apply"
		if !c.Passed {
			action = "Override the eval regression and apply"
		}
		scores := []string{}
		for _, score := range c.Scores {
			scores = append(scores, fmt.Sprintf("%s %.0f%% to %.0f%%", score.Suite, score.Before, score.After))
		}
		var candidate wire.AgentSpec
		if json.Unmarshal(c.Candidate, &candidate) == nil && (slices.Contains(candidate.Tools, "person_tools") || slices.Contains(candidate.Tools, personToolsGrant)) {
			scores = append(scores, "May use each initiating person's separately authorized and reviewed connection tools")
		}
		return fmt.Sprintf("%s %s? %s. Reason: %s", action, c.Agent, strings.Join(scores, "; "), arg(call.Arguments, "override_reason"))
	}
	return &changeApproval{ToolFunc: &types.ToolFunc{Def: types.ToolDef{Name: ToolApplyChange, Description: "Apply a previously evaluated change card after approval. A regression requires an explicit override_reason. Incomplete evaluations cannot be overridden.", Parameters: schema([]string{"name", "change"}, map[string]string{"name": "Agent name", "change": "Change card id", "override_reason": "Why a person accepts this measured regression; required only for failed scores"})}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		if legacy.BuiltinAgent(req.Agent()) != BuiltinBuilder {
			return "", ErrBuilder
		}
		name := arg(args, "name")
		c, err := p.Evals.Change(req.Tenant, name, arg(args, "change"))
		if err != nil {
			return "", err
		}
		if c.Kind != "agent" || !c.Evaluated {
			return "", fmt.Errorf("%w: the change has not completed its evals", evals.ErrRegression)
		}
		reason := strings.TrimSpace(arg(args, "override_reason"))
		if !c.Passed && (reason == "" || len(reason) > 1000) {
			return "", fmt.Errorf("%w: a bounded override reason is required", evals.ErrRegression)
		}
		var candidate wire.AgentSpec
		if err = json.Unmarshal(c.Candidate, &candidate); err != nil {
			return "", err
		}
		if candidate.Name != name || candidate.Hash() != c.AfterHash {
			return "", ErrBuilder
		}
		current, err := p.builderTarget(ctx, req, name)
		if c.BeforeHash == "" {
			if !errors.Is(err, wire.ErrNotFound) {
				return "", wire.ErrConflict
			}
			if err = p.CheckAgentName(req.Tenant, name); err != nil {
				return "", err
			}
		} else if err != nil || current.Spec.Hash() != c.BeforeHash {
			return "", wire.ErrConflict
		}
		project, _ := p.ProjectTree(req.Tenant).Home(name)
		prefix, err := p.promptLayers(req.Tenant, project)
		if err != nil {
			return "", err
		}
		if c.ContextHash != prompts.NewLayer("prefix", "", prefix.System("")).Version {
			return "", wire.ErrConflict
		}
		suites, err := p.Evals.List(req.Tenant)
		if err != nil {
			return "", err
		}
		for _, suite := range suites {
			if suite.Subject != name || suite.Target != evals.Agent {
				continue
			}
			if !slices.ContainsFunc(c.Scores, func(score evals.ChangeScore) bool {
				return score.Suite == suite.Name && score.SuiteHash == evals.SuiteHash(suite)
			}) {
				return "", wire.ErrConflict
			}
		}
		// Keep the override on the reviewed scorecard before publishing anything.
		if !c.Passed {
			c.OverrideBy = "approved:agent:" + BuiltinBuilder
			c.OverrideReason = reason
			if _, err = p.Evals.SaveChange(req.Tenant, c); err != nil {
				return "", err
			}
		}
		var rev wire.Revision
		if c.BeforeHash == "" {
			rev, err = p.Agents.Create(ctx, req.Tenant, candidate, "agent:"+BuiltinBuilder)
		} else {
			rev, err = p.Agents.Revise(ctx, req.Tenant, candidate, current.Rev, "agent:"+BuiltinBuilder)
		}
		if err != nil {
			return "", err
		}
		raw, err := json.Marshal(rev)
		return string(raw), err
	}}, prompt: prompt}
}
func (p *Platform) builderRecordEvalTool(req runtime.Request) types.Tool {
	return &approvedBuilderTool{&types.ToolFunc{Def: types.ToolDef{Name: ToolRecordEval, Description: "Add one real session input and its recorded tool outcomes to a suite, with explicit deterministic checks. Replay never calls the live tools.", Parameters: schema([]string{"suite", "run", "case", "checks"}, map[string]string{"suite": "Existing suite name", "run": "Readable source run id", "case": "Unique case id", "checks": "JSON array of deterministic checks"})}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		suite, err := p.Evals.Get(req.Tenant, arg(args, "suite"))
		if err != nil {
			return "", err
		}
		if _, err = p.builderTarget(ctx, req, suite.Subject); err != nil {
			return "", err
		}
		var checks []evals.Check
		if err = json.Unmarshal([]byte(arg(args, "checks")), &checks); err != nil {
			return "", err
		}
		// Cases are retained for the suite's audience, which can be broader
		// than the builder session that is recording this example.
		suiteHome, _ := p.ProjectTree(req.Tenant).Home(suite.Subject)
		c, err := p.recordedEvalCase(req.Tenant, suiteHome, arg(args, "run"), arg(args, "case"), checks)
		if err != nil {
			return "", err
		}
		source, err := p.Runs.Get(req.Tenant, c.Trace.Run)
		if err != nil || source.Agent != suite.Subject {
			return "", ErrBuilder
		}
		who, err := p.builderPerson(req)
		candidate := suite
		candidate.Cases = []evals.Case{c}
		if err != nil || !p.EvalSuiteSourcesVisible(who, candidate) {
			return "", ErrForbidden
		}
		suite.Cases = append(suite.Cases, c)
		suite.CreatedBy = who.Subject
		suite.UpdatedAt = p.now()
		if err = p.Evals.Put(req.Tenant, suite, true); err != nil {
			return "", err
		}
		return "Recorded case added to " + suite.Name + ".", nil
	}}}
}
