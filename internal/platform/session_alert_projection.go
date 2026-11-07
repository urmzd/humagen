package platform

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/runs"
)

// projectSessionAlert is the migration seam: transport delivery can still use
// inbox records for one release while all new attention has one session anchor.
func (p *Platform) projectSessionAlert(tenant, key string, m inbox.Message, at time.Time) error {
	action := ""
	switch m.Kind {
	case inbox.KindApproval:
		action = "approve"
	case inbox.KindClarification, inbox.KindMention:
		action = "answer"
	case inbox.KindFailure:
		action = "retry"
	case inbox.KindSource:
		action = "reconnect"
	case inbox.KindAlert, inbox.KindBudget, inbox.KindOperation:
	default:
		return nil
	}
	sessionID := m.Source.Thread
	if m.Source.Run != "" {
		if r, err := p.Runs.Get(tenant, m.Source.Run); err == nil {
			if IsEvaluation(r) || r.Origin == runs.OriginSystem {
				return nil
			}
			if sessionID == "" {
				sessionID = r.Thread
			}
		}
	}
	th, err := p.ThreadRecord(tenant, sessionID)
	if err == nil && th.Session == nil {
		agent := m.Source.Agent
		if agent == "" {
			agent = th.Default
		}
		if agent == "" {
			agent = BuiltinAnalyst
		}
		sessionID, err = p.EnsureAlertSessionFromThread(context.Background(), tenant, th.ID, agent, m.Subject)
		if err != nil {
			return err
		}
		th, err = p.Threads.Get(tenant, sessionID)
		if err != nil {
			return err
		}
	} else if err != nil {
		agent := m.Source.Agent
		if agent == "" {
			agent = BuiltinAnalyst
		}
		owner := m.To
		if owner == "" && m.Source.Run != "" {
			if run, e := p.Runs.Get(tenant, m.Source.Run); e == nil {
				owner = personOf(run.Actor)
			}
		}
		if owner == "" && len(m.Audience) == 1 && !strings.HasPrefix(m.Audience[0], alerts.RolePrefix) {
			owner = m.Audience[0]
		}
		sessionID, err = p.EnsureAlertSession(context.Background(), tenant, agent, m.Source.Project, owner, m.Subject)
		if err != nil {
			return err
		}
		th, err = p.Threads.Get(tenant, sessionID)
		if err != nil {
			return err
		}
	}
	if m.Source.Alert != "" {
		_, err = p.Alerts.AttachSession(tenant, m.Source.Alert, th.ID, action)
		return err
	}
	who := slices.Clone(m.Audience)
	if m.To != "" {
		who = append(who, m.To)
	}
	priority, severity := alerts.Normal, alerts.Warning
	if m.Severity == inbox.Critical {
		priority, severity = alerts.Urgent, alerts.Critical
	}
	a, err := p.Alerts.ProjectSession(tenant, key, alerts.Alert{Projection: m.ID, Session: th.ID, Project: th.Project, RunID: m.Source.Run, Agent: m.Source.Agent, Kind: alerts.Kind(m.Kind), Title: m.Subject, Severity: severity, Priority: priority, State: alerts.OpenState, At: at, Who: who, Notify: who, Action: action})
	if err != nil {
		return err
	}
	if answeredSessionAlert(m) {
		_, err = p.Alerts.ChangeState(tenant, a.ID, "system", alerts.Resolved, time.Time{}, p.now())
	}
	return err
}

// settleSessionAlerts resolves question/approval alerts when their authoritative
// inbox action has settled. A failure or manual alert needs an explicit choice.
func (p *Platform) settleSessionAlerts(tenant, sessionID string) error {
	all, err := p.Alerts.Alerts(tenant)
	if err != nil {
		return err
	}
	for _, a := range all {
		projectedQuestion := a.Kind == alerts.Kind(inbox.KindApproval) || a.Kind == alerts.Kind(inbox.KindClarification)
		if !projectedQuestion || !a.Open() || a.RunID == "" || (a.Action != "answer" && a.Action != "approve") {
			continue
		}
		r, err := p.Runs.Get(tenant, a.RunID)
		if err != nil {
			if errors.Is(err, runs.ErrNotFound) {
				continue
			}
			return err
		}
		if a.Session != sessionID && r.Thread != sessionID {
			continue
		}
		m, ok := p.Inbox.ByID(tenant, a.Projection)
		if ok && answeredSessionAlert(m) {
			if _, err = p.Alerts.ChangeState(tenant, a.ID, "system", alerts.Resolved, time.Time{}, p.now()); err != nil {
				return err
			}
		}
	}
	return nil
}

// A terminal run or a dismissed card is not an answer. Only a persisted,
// authoritative answer outcome settles its corresponding question alert.
func answeredSessionAlert(m inbox.Message) bool {
	if m.Kind != inbox.KindApproval && m.Kind != inbox.KindClarification && m.Kind != inbox.KindMention {
		return false
	}
	answered := false
	for _, part := range m.Parts {
		if part.Question == nil && part.Mention == nil {
			continue
		}
		if part.Outcome == nil || part.Outcome.By == "" {
			return false
		}
		switch part.Outcome.Action {
		case ActApprove, ActDeny, ActChoose, ActReply, ActDone:
			answered = true
		default:
			return false
		}
	}
	return answered
}

func (p *Platform) evaluationNotice(tenant string, m inbox.Message) bool {
	if m.Source.Run == "" {
		return false
	}
	r, err := p.Runs.Get(tenant, m.Source.Run)
	return err == nil && IsEvaluation(r)
}
