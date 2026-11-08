package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/prompts"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/turn"
	"github.com/urmzd/saige/flow/turn/native"
)

// evaluateCandidate runs a proposed instruction revision before publishing it.
// Tool-bearing suites need an isolated replay Runtime supplied by the host;
// they fail closed rather than execute production mutations during a test.
func (p *Platform) evaluateCandidate(ctx context.Context, tenant string, spec wire.AgentSpec, prefix prompts.Snapshot) evals.Answerer {
	return func(ctx context.Context, c evals.Case) (string, error) {
		bounded, cancel := context.WithTimeout(ctx, caseTimeout)
		defer cancel()
		def, err := wire.EvaluationDefinition(spec)
		if err != nil {
			return "", err
		}
		def.Spec.SystemPrompt = prefix.System(spec.SystemPrompt)
		project, _ := p.ProjectTree(tenant).Home(spec.Name)
		for _, source := range p.ProjectTree(tenant).Knowledge(project) {
			if !slices.ContainsFunc(def.Spec.Sources, func(s native.Source) bool { return s.Name == source.Name }) {
				def.Spec.Sources = append(def.Spec.Sources, native.Source{Name: source.Name})
			}
		}
		retriever, sources, err := replayRetrieval(c, def.Spec.Sources)
		if err != nil {
			return "", err
		}
		def.Spec.Sources = sources
		engine := p.cfg.Runtime
		if retriever != nil {
			// Recorded context always takes the isolated native path, never a
			// host runtime that could perform its own live retrieval.
			engine = nil
		}
		if engine == nil {
			if c.Trace == nil && (len(spec.Tools) > 0 || len(spec.Access.Spawn) > 0 || len(spec.Sources) > 0) {
				return "", fmt.Errorf("agent %s requires an isolated tool replay runtime before its change can be evaluated", spec.Name)
			}
			options := slices.Concat(p.cfg.Ext.agentOptions(), p.cfg.AgentOptions)
			if c.Trace != nil {
				replay, replayErr := evals.NewReplay(*c.Trace)
				if replayErr != nil {
					return "", replayErr
				}
				options = append(options, native.WithTools(replayTools{trace: *c.Trace, replay: replay}), native.WithRetriever(retriever), native.WithStrictTools())
				engine = &replayRuntime{Runtime: &native.Runtime{Options: options}, replay: replay}
			} else {
				engine = &native.Runtime{Options: options}
			}
		}
		trigger := wire.NewEvent(wire.Raise{ID: "candidate-eval", Tenant: tenant, Agent: spec.Name, Origin: OriginEval, Text: c.Input})
		out, err := engine.Turn(bounded, def, turn.Input{Tenant: tenant, Trigger: trigger, Accounting: "candidate:" + spec.Hash()}, func(context.Context, event.Body) error { return nil })
		if err == nil && (out.Parked != nil || out.Stopped != nil) {
			return "", fmt.Errorf("the evaluation did not finish")
		}
		return out.Text, err
	}
}

func (p *Platform) checkAgentEvals(ctx context.Context, tenant string, before, after wire.AgentSpec) ([]evals.ChangeScore, error) {
	suites, err := p.Evals.List(tenant)
	if err != nil {
		return nil, err
	}
	project, _ := p.ProjectTree(tenant).Home(after.Name)
	prefix, err := p.promptLayers(tenant, project)
	if err != nil {
		return nil, err
	}
	var scores []evals.ChangeScore
	var gateErr error
	evaluated := true
	for _, suite := range suites {
		if suite.Target != evals.Agent || suite.Subject != after.Name {
			continue
		}
		score, err := evals.CheckChange(ctx, suite, p.evaluateCandidate(ctx, tenant, before, prefix), p.evaluateCandidate(ctx, tenant, after, prefix))
		scores = append(scores, score)
		evaluated = evaluated && len(score.Results) == len(suite.Cases)
		if err != nil && gateErr == nil {
			gateErr = err
		}
	}
	candidate, err := json.Marshal(after)
	if err != nil {
		return scores, err
	}
	card := evals.Change{Agent: after.Name, Kind: "agent", ContextHash: prompts.NewLayer("prefix", "", prefix.System("")).Version, BeforeHash: before.Hash(), AfterHash: after.Hash(), Candidate: candidate, Scores: scores, Passed: gateErr == nil, Evaluated: evaluated, At: p.now()}
	if before.Name == "" {
		card.BeforeHash = ""
	}
	if gateErr != nil {
		card.Error = gateErr.Error()
	}
	saved, err := p.Evals.SaveChange(tenant, card)
	if err != nil {
		return scores, err
	}
	if gateErr != nil {
		return scores, fmt.Errorf("%w (scorecard %s)", gateErr, saved.ID)
	}
	return scores, nil
}

func (p *Platform) checkSharedPromptEvals(ctx context.Context, tenant string, config prompts.Config, shown func(string) bool) error {
	suites, err := p.Evals.List(tenant)
	if err != nil {
		return err
	}
	var firstFailure error
	for _, suite := range suites {
		if suite.Target != evals.Agent {
			continue
		}
		rev, err := p.Catalog().Get(ctx, tenant, suite.Subject, 0)
		if err != nil {
			return err
		}
		project, _ := p.ProjectTree(tenant).Home(suite.Subject)
		before, err := p.promptLayers(tenant, project)
		if err != nil {
			return err
		}
		var projectLayers []prompts.Layer
		for _, l := range before.Layers {
			if l.Kind == "project" || l.Kind == "workspace" && l.From != "workspace" {
				projectLayers = append(projectLayers, l)
			}
		}
		after, err := prompts.Compose(config, projectLayers)
		if err != nil {
			return err
		}
		// Shared changes sample every applicable suite, bounded to five cases.
		suite.Cases = suite.Cases[:min(len(suite.Cases), 5)]
		score, gateErr := evals.CheckChange(ctx, suite, p.evaluateCandidate(ctx, tenant, rev.Spec, before), p.evaluateCandidate(ctx, tenant, rev.Spec, after))
		candidate, _ := json.Marshal(config)
		card := evals.Change{Agent: suite.Subject, Kind: "shared_prompts", BeforeHash: prompts.NewLayer("prefix", "", before.System("")).Version, AfterHash: prompts.NewLayer("prefix", "", after.System("")).Version, Candidate: candidate, Scores: []evals.ChangeScore{score}, Passed: gateErr == nil, Evaluated: len(score.Results) == len(suite.Cases), At: p.now()}
		if gateErr != nil {
			card.Error = gateErr.Error()
		}
		saved, err := p.Evals.SaveChange(tenant, card)
		if err != nil {
			return err
		}
		if gateErr != nil {
			if firstFailure == nil {
				if shown != nil && !shown(suite.Subject) {
					firstFailure = fmt.Errorf("%w: a hidden agent's sampled eval did not pass", evals.ErrRegression)
				} else {
					firstFailure = fmt.Errorf("%w (scorecard %s)", gateErr, saved.ID)
				}
			}
		}
	}
	return firstFailure
}
