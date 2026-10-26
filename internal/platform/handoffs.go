package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func (p *Platform) checkOperationCall(ctx context.Context, parent runs.Run, req call.Request) error {
	restrictionReq := runtime.Request{Tenant: parent.Tenant, RunID: parent.ID}
	if allowed, restricted, err := p.operationTools(restrictionReq); err != nil {
		return err
	} else if restricted && !slices.Contains(allowed, ToolCallAgent) && !slices.Contains(allowed, ToolDelegate) && !slices.Contains(allowed, tagTool+req.Agent) {
		return operations.ErrDenied
	}
	if err := p.operationAdmission(parent.Tenant, parent.Agent); err != nil {
		return err
	}
	person := runs.AskedBy(parent)
	if parent.Parent != "" {
		if root, err := p.Runs.Get(parent.Tenant, parent.Root); err == nil {
			person = runs.AskedBy(root)
		}
	}
	level := p.ActionDecision(parent.Tenant, parent.Project, parent.Agent, person, operations.CallAgents)
	if parent.Tag().Meta[operations.MetaForceAsk] == "true" && level != operations.Never {
		level = operations.Ask
	}
	if level == operations.Never {
		return operations.ErrDenied
	}
	if level == operations.Ask && !approvedCall(ctx, req.Agent) {
		info, _ := saigetypes.ToolCallInfoFromContext(ctx)
		approved := false
		for _, name := range []string{ToolCallAgent, ToolDelegate, "tag_" + req.Agent} {
			if p.approvedCallTo(parent.Tenant, parent.ID, info.ID+":action", "tool:"+name) {
				approved = true
			}
		}
		if r, ok := ctx.Value(operationRequestKey{}).(runtime.Request); ok && r.Resume != nil {
			if a, ok := r.Resume.Answers[info.ID]; ok && a.Decision == payload.DecisionApprove {
				approved = true
			}
		}
		if !approved {
			return fmt.Errorf("%w: a person must approve this agent call", operations.ErrDenied)
		}
	}
	return nil
}
func (p *Platform) recordHandoff(parent runs.Run, req call.Request, tag envelope.Tag, prepared call.Prepared) (operations.Handoff, error) {
	id := runtime.RunID(parent.Tenant, tag.ID)
	if old, ok := p.Operations.Handoff(parent.Tenant, id); ok {
		if old.State == operations.NeedsPerson && old.RunID == "" {
			return old, operations.ErrEscalation
		}
		return old, nil
	}
	chain := []string{parent.Agent}
	ancestor := parent
	for ancestor.Parent != "" && len(chain) < 4 {
		previous, err := p.Runs.Get(parent.Tenant, ancestor.Parent)
		if err != nil {
			return operations.Handoff{}, err
		}
		chain = append(chain, previous.Agent)
		ancestor = previous
	}
	slices.Reverse(chain)
	chain = append(chain, req.Agent)
	goal := strings.TrimSpace(req.Text)
	if goal == "" && len(prepared.Messages) > 0 {
		goal = prepared.Messages[len(prepared.Messages)-1].Text
	}
	goal = clip(goal, 8000)
	done := clip(prepared.Input.ResponseFormat.Instruction(), 4000)
	h := operations.Handoff{ID: id, From: parent.Agent, To: req.Agent, Project: parent.Project, Thread: parent.Thread, Owner: p.AgentControl(parent.Tenant, parent.Agent).Manager, Goal: goal, DoneWhen: done, Inputs: []operations.Input{}, Chain: chain, State: operations.InProgress, RunID: id, DueAt: p.now().Add(24 * time.Hour)}
	if req.Contract != nil {
		if err := req.Contract.Validate(); err != nil {
			return operations.Handoff{}, err
		}
		h.Goal, h.DoneWhen, h.DueAt = req.Contract.Goal, req.Contract.DoneWhen, req.Contract.DueAt
	}
	for _, m := range prepared.Input.Messages {
		for _, part := range m.Content {
			if len(h.Inputs) == 32 {
				break
			}
			switch v := part.(type) {
			case call.RunResultPart:
				h.Inputs = append(h.Inputs, operations.Input{Type: "run", Reference: v.Run})
			case call.ConversationRange:
				h.Inputs = append(h.Inputs, operations.Input{Type: "thread", Reference: v.Thread})
			case call.ArtifactPart:
				h.Inputs = append(h.Inputs, operations.Input{Type: "artifact", Reference: fmt.Sprintf("%s@%d", v.ID, v.Version)})
			case call.TextPart:
				h.Inputs = append(h.Inputs, operations.Input{Type: "text", Reference: clip(v.Text, 4000)})
			}
		}
	}

	out, err := p.Operations.SetHandoff(parent.Tenant, h, 0, p.now())
	if err == operations.ErrEscalation {
		safe := []string{}
		for _, name := range h.Chain {
			if !slices.Contains(safe, name) && len(safe) < 4 {
				safe = append(safe, name)
			}
		}
		h.Chain, h.State, h.Blocker, h.RunID = safe, operations.NeedsPerson, "The handoff contains a cycle or exceeds three hops. A person must choose the next owner.", ""
		if _, saveErr := p.Operations.SetHandoff(parent.Tenant, h, 0, p.now()); saveErr != nil {
			return operations.Handoff{}, saveErr
		}
		p.operationEscalation(parent.Tenant, h.ID, h.Project, h.Thread, parent.ID, h.From, h.Owner, h.Blocker)
		return operations.Handoff{}, err
	}
	if err != nil {
		return out, err
	}
	return out, nil
}
func (p *Platform) operationEscalation(tenant, id, project, thread, run, agent, owner, reason string) {
	// The message shares the original place and uses one deterministic key.
	// Its kind makes it Needs-you; it creates no synthetic runtime question.
	m := inbox.Message{Kind: inbox.KindFailure, Subject: "Agent work needs a person", From: inbox.From{Kind: "system", Name: "Humagen"}, To: owner, Severity: inbox.Warning, Source: inbox.Source{Project: project, Thread: thread, Run: run, Agent: agent}, Parts: []inbox.Part{{ID: "blocker", Type: inbox.PartText, Text: reason}}}
	if owner == "" {
		m.Audience = slices.Clone(fallbackApprovers)
	}
	if _, _, err := p.Inbox.RaiseOnce(tenant, "operation:"+id, m, p.now()); err != nil {
		p.log.Error("platform: operation escalation could not be kept", "error", err)
	}
}

func (p *Platform) sameHandoffTerms(run runs.Run, terms *call.Contract) bool {
	if terms == nil {
		return true
	}
	h, ok := p.Operations.Handoff(run.Tenant, run.ID)
	return ok && h.Goal == terms.Goal && h.DoneWhen == terms.DoneWhen && h.DueAt.Equal(terms.DueAt)
}
