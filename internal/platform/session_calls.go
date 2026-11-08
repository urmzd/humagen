package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/runtime"
)

type sessionCallKey struct{}
type admittedSessionCall struct {
	call   runtime.SessionCall
	parent string
}

// startSessionChild keeps call policy, leases and run accounting on pkg/call.
// Returning after admission lets orchagent park the caller without a worker.
func (p *Platform) startSessionChild(ctx context.Context, parent runtime.Input, req runtime.SessionCall) (string, error) {
	runID := parent.Run
	if runID == "" {
		runID = runtime.RunID(parent.Trigger.Tenant, parent.Trigger.ID)
	}
	pr, err := p.runRecordOf(parent.Trigger.Tenant, runID)
	if err != nil {
		return "", err
	}
	th, err := p.Threads.Get(pr.Tenant, pr.Thread)
	if err != nil {
		return "", err
	}
	if parent.Session == nil || th.Session == nil || parent.Session.ID != th.ID {
		return "", fmt.Errorf("%w: caller has no session", call.ErrInvalid)
	}
	child := threads.Thread{ID: req.Session.ID, Project: th.Project, Channel: th.Channel, Visibility: th.Visibility, CreatedBy: th.CreatedBy, Participants: slices.Clone(th.Participants), Default: req.Agent, Agents: []string{req.Agent}, Title: firstWords(req.Text, threads.MaxTitle), Session: &threads.SessionIdentity{Agent: req.Agent, Parent: th.ID, Label: req.Session.Key.Label, SourceAlert: th.Session.SourceAlert}}
	if _, err = p.Threads.Create(pr.Tenant, child, p.now()); err != nil && !errors.Is(err, threads.ErrExists) {
		return "", err
	}
	ctx = context.WithValue(ctx, sessionCallKey{}, admittedSessionCall{call: req, parent: runID})
	accepted, err := p.call(ctx, pr.Tenant, runID, call.Request{Key: req.ID, Agent: req.Agent, Text: req.Text, Mode: call.Subagent}, true, nil)
	if err != nil {
		return "", err
	}
	return accepted.ID, nil
}
