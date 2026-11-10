package platform

import (
	"context"
	"fmt"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/signals"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/prompts"
	"github.com/urmzd/humagen/pkg/runs"
)

func (p *Platform) scoreOnline(run runs.Run) error {
	if !run.Status.Terminal() || IsEvaluation(run) || run.Origin == runs.OriginSystem {
		return nil
	}
	// The accepted surface and prompt budget remain fixed across retries.
	slack := strings.HasPrefix(run.Tag().Channel, "slack://")
	budget := prompts.DefaultSlackWords
	if pin, ok, err := prompts.Read(wire.Meta(run.Tag())); err != nil {
		return err
	} else if ok {
		budget = pin.SlackWords
	}
	question := strings.ToLower(run.Text)
	detail := false
	for _, request := range []string{"in detail", "detailed", "go deep", "step by step", "comprehensive"} {
		detail = detail || strings.Contains(question, request)
	}
	result := evals.Online{Passed: true, Verdicts: []evals.Verdict{}}
	if slack {
		result = evals.CheckStyle(run.Answer, budget, detail)
	}
	if verdict, applicable := p.onlineFormat(run); applicable {
		result.Verdicts = append(result.Verdicts, verdict)
	}
	events, err := p.Journal.Events(run.Tenant, []string{run.ID})
	if err != nil {
		return err
	}
	result.Signals = signals.Fold(run.ID, events, nil)
	evidence := len(run.Citations) > 0 || strings.Contains(run.Answer, "https://") || strings.Contains(run.Answer, "http://")
	result.Verdicts = append(result.Verdicts, evals.CheckSignals(result.Signals, p.Settings(run.Tenant).EvalLimits, run.Status == runs.Completed, evidence)...)
	for _, v := range result.Verdicts {
		result.Passed = result.Passed && v.Passed
	}
	result.Run, result.Agent, result.Version, result.At = run.ID, run.Agent, run.Rev, run.UpdatedAt
	if err := p.Evals.SaveOnline(run.Tenant, result); err != nil {
		return err
	}
	if !result.Passed && run.Status == runs.Completed {
		return p.raiseOnlineHealth(run, result)
	}
	return nil
}

// RecordFeedbackCandidate is called only after the source feedback is saved.
// Its input remains under the source run's project and visibility checks.
func (p *Platform) RecordFeedbackCandidate(tenant string, record feedback.Record) error {
	p.RefreshGoodAsk(tenant, record.Run)
	if record.Rating != "down" || strings.TrimSpace(record.Reason) == "" {
		return nil
	}
	run, err := p.Runs.Get(tenant, record.Run)
	if err != nil {
		return err
	}
	return p.Evals.SaveCandidate(tenant, record.Subject, evals.Candidate{Run: run.ID, Agent: run.Agent, Project: run.Project, Input: run.Text, Reason: record.Reason, Comment: record.Comment, At: record.UpdatedAt})
}

func (p *Platform) raiseOnlineHealth(run runs.Run, result evals.Online) error {
	if err := p.EnsureBuiltins(context.Background(), run.Tenant); err != nil {
		return err
	}
	owner := personOf(run.Actor)
	// Private source content never reaches a shared builder session.
	if run.UsesPrivateContext() && owner == "" {
		return nil
	}
	if !p.openWork(run.Tenant, run.Project)(run) && owner == "" {
		return nil
	}
	sessionID, err := p.EnsureAlertSession(context.Background(), run.Tenant, p.cfg.EntryAgent, run.Project, owner, "Agent health: "+run.Agent)
	if err != nil {
		return err
	}
	checks := []string{}
	for _, v := range result.Verdicts {
		if !v.Passed {
			checks = append(checks, v.Check)
		}
	}
	_, err = p.Alerts.ProjectSession(run.Tenant, "online-eval:"+run.ID, alerts.Alert{Session: sessionID, RunID: run.ID, Project: run.Project, Agent: run.Agent, Kind: alerts.Kind("eval"), Title: fmt.Sprintf("%s needs review: %s", run.Agent, strings.Join(checks, ", ")), Priority: alerts.Normal, State: alerts.OpenState, At: p.now(), Action: "review"})
	return err
}
