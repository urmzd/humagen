package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/prompts"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native"
	types "github.com/urmzd/saige/agent/types"
)

const (
	ToolPreviewWorkspacePrompt = "preview_workspace_prompt"
	ToolApplyWorkspacePrompt   = "apply_workspace_prompt"
)

type sharedPromptAgent struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Hash    string `json:"hash"`
	Prefix  string `json:"prefix"`
}
type sharedPromptCandidate struct {
	Config prompts.Config      `json:"config"`
	Agents []sharedPromptAgent `json:"agents"`
}

func promptConfigHash(config prompts.Config) string {
	raw, _ := json.Marshal(config)
	return prompts.NewLayer("config", "", string(raw)).Version
}
func promptSnapshotHash(snapshot prompts.Snapshot) string {
	raw, _ := json.Marshal(snapshot)
	return prompts.NewLayer("snapshot", "", string(raw)).Version
}
func promptAgentsHash(agents []sharedPromptAgent) string {
	raw, _ := json.Marshal(agents)
	return prompts.NewLayer("agents", "", string(raw)).Version
}
func replaceSharedLayers(before prompts.Snapshot, config prompts.Config) (prompts.Snapshot, error) {
	var layers []prompts.Layer
	for _, layer := range before.Layers {
		if layer.Kind == "project" || layer.Kind == "workspace" && layer.From != "workspace" {
			layers = append(layers, layer)
		}
	}
	return prompts.Compose(config, layers)
}

func (p *Platform) prepareSharedPromptChange(ctx context.Context, tenant string, config prompts.Config) (evals.Change, error) {
	if err := config.Validate(); err != nil {
		return evals.Change{}, err
	}
	stored, err := p.settings.get(tenant)
	if err != nil {
		return evals.Change{}, err
	}
	suites, err := p.Evals.List(tenant)
	if err != nil {
		return evals.Change{}, err
	}
	slices.SortFunc(suites, func(a, b evals.Suite) int { return strings.Compare(a.Name, b.Name) })
	candidate := sharedPromptCandidate{Config: config, Agents: []sharedPromptAgent{}}
	card := evals.Change{Agent: BuiltinBuilder, Kind: "workspace_prompt", BeforeHash: promptConfigHash(stored.Prompts), AfterHash: promptConfigHash(config), Scores: []evals.ChangeScore{}, Evaluated: true, Passed: true, At: p.now()}
	for _, suite := range suites {
		if suite.Target != evals.Agent {
			continue
		}
		rev, err := p.Catalog().Get(ctx, tenant, suite.Subject, 0)
		if err != nil {
			return card, err
		}
		project, _ := p.ProjectTree(tenant).Home(suite.Subject)
		before, err := p.promptLayers(tenant, project)
		if err != nil {
			return card, err
		}
		after, err := replaceSharedLayers(before, config)
		if err != nil {
			return card, err
		}
		pin := sharedPromptAgent{Name: suite.Subject, Version: rev.Rev, Hash: rev.Spec.Hash(), Prefix: promptSnapshotHash(before)}
		if old := slices.IndexFunc(candidate.Agents, func(a sharedPromptAgent) bool { return a.Name == pin.Name }); old >= 0 {
			if candidate.Agents[old] != pin {
				return card, wire.ErrConflict
			}
		} else {
			candidate.Agents = append(candidate.Agents, pin)
		}
		fullHash := evals.SuiteHash(suite)
		suite.Cases = suite.Cases[:min(5, len(suite.Cases))]
		score, gateErr := evals.CheckChange(ctx, suite, p.evaluateCandidate(ctx, tenant, rev.Spec, before), p.evaluateCandidate(ctx, tenant, rev.Spec, after))
		// Sampling limits execution, not the immutable suite checked at apply.
		score.SuiteHash = fullHash
		card.Scores = append(card.Scores, score)
		card.Evaluated = card.Evaluated && len(score.Results) == len(suite.Cases)
		if gateErr != nil {
			card.Passed = false
			if card.Error == "" {
				card.Error = gateErr.Error()
			}
		}
	}
	card.ContextHash = promptAgentsHash(candidate.Agents)
	card.Candidate, err = json.Marshal(candidate)
	if err != nil {
		return card, err
	}
	// Nothing may move while the sampled gate is being evaluated.
	if err = p.checkSharedPromptPins(ctx, tenant, card, candidate); err != nil {
		return card, err
	}
	return p.Evals.SaveChange(tenant, card)
}

func (p *Platform) checkSharedPromptPins(ctx context.Context, tenant string, card evals.Change, candidate sharedPromptCandidate) error {
	stored, err := p.settings.get(tenant)
	if err != nil {
		return err
	}
	if promptConfigHash(stored.Prompts) != card.BeforeHash || promptConfigHash(candidate.Config) != card.AfterHash || promptAgentsHash(candidate.Agents) != card.ContextHash {
		return wire.ErrConflict
	}
	suites, err := p.Evals.List(tenant)
	if err != nil {
		return err
	}
	count := 0
	for _, suite := range suites {
		if suite.Target != evals.Agent {
			continue
		}
		count++
		if !slices.ContainsFunc(card.Scores, func(s evals.ChangeScore) bool { return s.Suite == suite.Name && s.SuiteHash == evals.SuiteHash(suite) }) {
			return wire.ErrConflict
		}
	}
	if count != len(card.Scores) {
		return wire.ErrConflict
	}
	for _, pin := range candidate.Agents {
		rev, err := p.Catalog().Get(ctx, tenant, pin.Name, 0)
		if err != nil || rev.Rev != pin.Version || rev.Spec.Hash() != pin.Hash {
			return wire.ErrConflict
		}
		project, _ := p.ProjectTree(tenant).Home(pin.Name)
		prefix, err := p.promptLayers(tenant, project)
		if err != nil {
			return err
		}
		if promptSnapshotHash(prefix) != pin.Prefix {
			return wire.ErrConflict
		}
	}
	return nil
}

// applySharedPromptChange is reachable from the independently approved native
// tool only. Ordinary settings updates still reject every measured regression.
func (p *Platform) applySharedPromptChange(ctx context.Context, tenant, id, reason string) error {
	card, err := p.Evals.Change(tenant, BuiltinBuilder, id)
	if err != nil {
		return err
	}
	if card.Kind != "workspace_prompt" || !card.Evaluated {
		return fmt.Errorf("%w: incomplete evaluations cannot be overridden", evals.ErrRegression)
	}
	reason = strings.TrimSpace(reason)
	if !card.Passed && (reason == "" || len(reason) > 1000) {
		return fmt.Errorf("%w: a bounded explicit override reason is required", evals.ErrRegression)
	}
	var candidate sharedPromptCandidate
	if err = json.Unmarshal(card.Candidate, &candidate); err != nil {
		return err
	}
	if err = candidate.Config.Validate(); err != nil {
		return err
	}
	if err = p.checkSharedPromptPins(ctx, tenant, card, candidate); err != nil {
		return err
	}
	if !card.Passed {
		card.OverrideBy = "approved:agent:" + BuiltinBuilder
		card.OverrideReason = reason
		if _, err = p.Evals.SaveChange(tenant, card); err != nil {
			return err
		}
	}
	return p.settings.update(tenant, func(stored *storedSettings) error {
		if promptConfigHash(stored.Prompts) != card.BeforeHash {
			return wire.ErrConflict
		}
		if err = p.checkSharedPromptPins(ctx, tenant, card, candidate); err != nil {
			return err
		}
		stored.Prompts = candidate.Config
		return nil
	})
}

func (p *Platform) builderPreviewWorkspacePromptTool(req runtime.Request) types.Tool {
	return &types.ToolFunc{Def: types.ToolDef{Name: ToolPreviewWorkspacePrompt, Description: "Preview shared instructions and every agent suite's sampled scores. A regression requires a separate administrator's explicit approval and override reason before applying the card.", Parameters: schema([]string{"config"}, map[string]string{"config": "Full JSON prompts config with platform,workspace,slack_word_budget"})}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		who, err := p.builderPerson(req)
		if err != nil {
			return "", err
		}
		if !who.Can(auth.AgentsWrite) || !who.Can(auth.EvalsRead) {
			return "", auth.ErrForbidden
		}
		var config prompts.Config
		dec := json.NewDecoder(strings.NewReader(arg(args, "config")))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&config); err != nil {
			return "", err
		}
		before, err := p.settings.get(req.Tenant)
		if err != nil {
			return "", err
		}
		card, err := p.prepareSharedPromptChange(ctx, req.Tenant, config)
		if err != nil {
			return "", err
		}
		card = p.sharedPromptCardView(req.Tenant, card, p.agentOpenFor(req.Tenant, p.ownProject(req.Tenant, req.RunID)))
		old, _ := prompts.Compose(before.Prompts, nil)
		next, _ := prompts.Compose(config, nil)
		raw, err := json.Marshal(struct {
			Change evals.Change `json:"change"`
			Before string       `json:"before_prompt"`
			After  string       `json:"after_prompt"`
			Tools  []string     `json:"tools"`
		}{card, old.System(""), next.System(""), []string{}})
		return string(raw), err
	}}
}

type sharedPromptApproval struct {
	*types.ToolFunc
	prompt func(types.ToolCallPart) string
}

func (t *sharedPromptApproval) Approval(call types.ToolCallPart) (native.ApprovalRequest, bool) {
	return native.ApprovalRequest{Prompt: t.prompt(call), Approvers: []string{"role:admin"}, SelfApproval: false}, true
}
func (p *Platform) builderApplyWorkspacePromptTool(req runtime.Request) types.Tool {
	prompt := func(call types.ToolCallPart) string {
		card, err := p.Evals.Change(req.Tenant, BuiltinBuilder, arg(call.Arguments, "change"))
		if err != nil {
			return "Approve the reviewed shared instruction change?"
		}
		action := "Apply"
		if !card.Passed {
			action = "Override the measured regression and apply"
		}
		return fmt.Sprintf("%s shared instructions? Change %s. Reason: %s", action, card.ID, arg(call.Arguments, "override_reason"))
	}
	return &sharedPromptApproval{ToolFunc: &types.ToolFunc{Def: types.ToolDef{Name: ToolApplyWorkspacePrompt, Description: "Apply an evaluated shared-instruction change after a different administrator approves. Regressions require override_reason; incomplete or stale cards are refused.", Parameters: schema([]string{"change"}, map[string]string{"change": "Reviewed change card id", "override_reason": "Explicit reason for accepting a measured regression"})}, Fn: func(ctx context.Context, args map[string]any) (string, error) {
		who, err := p.builderPerson(req)
		if err != nil {
			return "", err
		}
		if !who.Can(auth.AgentsWrite) {
			return "", auth.ErrForbidden
		}
		if err = p.applySharedPromptChange(ctx, req.Tenant, arg(args, "change"), arg(args, "override_reason")); err != nil {
			return "", err
		}
		return "Shared instructions saved; accepted runs retain their pinned versions.", nil
	}}, prompt: prompt}
}

func (p *Platform) sharedPromptCardView(tenant string, card evals.Change, shown func(string) bool) evals.Change {
	card.Candidate = json.RawMessage(`{}`)
	card.Scores = slices.Clone(card.Scores)
	card.Scores = slices.DeleteFunc(card.Scores, func(score evals.ChangeScore) bool {
		suite, err := p.Evals.Get(tenant, score.Suite)
		return err != nil || !shown(suite.Subject)
	})
	for i := range card.Scores {
		card.Scores[i].Results = []evals.Result{}
	}
	if card.Error != "" {
		card.Error = "One or more sampled suites did not pass; review the visible scores before approval."
	}
	return card
}
