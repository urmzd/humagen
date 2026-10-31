package platform

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/wire"
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
	if err := p.operationAdmissionRequest(runtime.Request{Tenant: parent.Tenant, RunID: parent.ID, Origin: parent.Origin, Meta: wire.Meta(parent.Tag()), Definition: native.Revision{Name: parent.Agent, Version: wire.VersionOf(parent.Rev), Spec: native.Spec{Name: parent.Agent}}}); err != nil {
		return err
	}
	person := p.operationPerson(parent)
	level := p.ActionDecision(parent.Tenant, p.RunPlace(parent.Tenant, parent).Project, parent.Agent, person, operations.CallAgents)
	if wire.Meta(parent.Tag())[operations.MetaForceAsk] == "true" && level != operations.Never {
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
			if a, ok := r.Resume.Answers[info.ID]; ok && a.Decision == event.DecisionApprove {
				approved = true
			}
		}
		if !approved {
			return fmt.Errorf("%w: a person must approve this agent call", operations.ErrDenied)
		}
	}
	return nil
}
func (p *Platform) recordHandoff(parent runs.Run, req call.Request, tag event.Envelope, prepared call.Prepared) (operations.Handoff, error) {
	p.admission.Lock()
	defer p.admission.Unlock()
	id := runtime.RunID(parent.Tenant, tag.ID)
	old, ok, err := p.Operations.Handoff(parent.Tenant, id)
	if err != nil {
		return operations.Handoff{}, err
	}
	if ok {
		if old.RunID == "" {
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
	control, err := p.AgentControl(parent.Tenant, parent.Agent)
	if err != nil {
		return operations.Handoff{}, err
	}
	h := operations.Handoff{Requester: p.operationPerson(parent), ID: id, From: parent.Agent, To: req.Agent, Project: parent.Project, Thread: parent.Thread, Owner: control.Manager, Goal: goal, DoneWhen: done, Inputs: []operations.Input{}, Chain: chain, State: operations.InProgress, RunID: id, DueAt: p.now().Add(24 * time.Hour)}
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
		p.operationEscalation(parent.Tenant, "handoff", h.ID, h.Project, h.Thread, parent.ID, h.From, h.Owner, h.Blocker)
		return operations.Handoff{}, err
	}
	if err != nil {
		return out, err
	}
	return out, nil
}
func (p *Platform) operationEscalation(tenant, kind, id, project, thread, run, agent, owner, reason string) {
	// The message shares the original place and uses one deterministic key.
	// Its kind makes it Needs-you; it creates no synthetic runtime question.
	version := 0
	switch kind {
	case "assignment":
		a, ok, err := p.Operations.Assignment(tenant, id)
		if err != nil {
			p.log.Error("platform: assignment could not be read", "error", err)
		}
		if !ok || (a.State != operations.Blocked && a.State != operations.NeedsPerson) {
			return
		}
		version = a.Version
	case "handoff":
		h, ok, err := p.Operations.Handoff(tenant, id)
		if err != nil {
			p.log.Error("platform: handoff could not be read", "error", err)
		}
		if !ok || (h.State != operations.Blocked && h.State != operations.NeedsPerson) {
			return
		}
		version = h.Version
	default:
		return
	}
	key := "operation:" + kind + ":" + id + ":" + strconv.Itoa(version)
	if err := p.clearOperationAttention(tenant, kind, id, key); err != nil {
		p.log.Error("platform: previous operation attention could not be settled", "error", err)
		return
	}
	m := inbox.Message{Kind: inbox.KindOperation, Subject: "Agent work needs a person", From: inbox.From{Kind: "system", Name: SystemSender}, Severity: inbox.Warning, Source: inbox.Source{Project: project, Thread: thread, Run: run, Agent: agent}, Parts: []inbox.Part{{ID: "blocker", Type: inbox.PartOperation, Operation: &inbox.OperationPart{Kind: kind, ID: id, Reason: reason}}}}
	m.Audience = slices.Clone(fallbackApprovers)
	if owner != "" {
		m.Audience = append(m.Audience, owner)
	}
	if kind == "handoff" {
		if contract, ok, _ := p.Operations.Handoff(tenant, id); ok {
			requester := contract.Requester
			if requester == "" && contract.RunID != "" {
				if run, err := p.Runs.Get(tenant, contract.RunID); err == nil {
					requester = p.operationPerson(run)
				}
			}
			if requester != "" && !slices.Contains(m.Audience, requester) {
				m.Audience = append(m.Audience, requester)
			}
		}
	}
	if _, _, err := p.Inbox.RaiseOnce(tenant, key, m, p.now()); err != nil {
		p.log.Error("platform: operation escalation could not be kept", "error", err)
	}
}

func (p *Platform) sameHandoffTerms(run runs.Run, terms *call.Contract) bool {
	if terms == nil {
		return true
	}
	h, ok, err := p.Operations.Handoff(run.Tenant, run.ID)
	return err == nil && ok && h.Goal == terms.Goal && h.DoneWhen == terms.DoneWhen && h.DueAt.Equal(terms.DueAt)
}

func (p *Platform) operationTagPerson(tag event.Envelope, edge call.Edge) string {
	if edge.Parent != "" {
		if root, err := p.Runs.Get(tag.Tenant, runtime.RunID(tag.Tenant, edge.Root)); err == nil {
			return p.operationPerson(root)
		}
	}
	if id := wire.Meta(tag)[metaAssignment]; id != "" {
		a, ok, err := p.Operations.Assignment(tag.Tenant, id)
		if err != nil {
			p.log.Error("platform: assignment owner could not be read", "error", err)
		}
		if ok {
			return a.Owner
		}
	}
	if wire.Origin(tag) == runs.OriginAPI && !wire.ActorOf(tag).Bot && wire.ActorOf(tag).User == "" {
		return wire.ActorOf(tag).ID
	}
	return personOf(wire.ActorOf(tag))
}
