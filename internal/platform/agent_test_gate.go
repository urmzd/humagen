package platform

import (
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/operations"
)

// AgentTestGate verifies a durable setup-owner/revision capability. It may
// exempt only an inactive draft agent's own pause, never workspace pause or
// action policy. Implementations must not infer authority from client metadata.
type AgentTestGate interface {
	Ready(tenant, agent string) bool
	AllowTag(envelope.Tag) bool
	AllowRequest(runtime.Request) bool
}

func (p *Platform) RegisterAgentTestGate(gate AgentTestGate) error {
	if gate == nil {
		return operations.ErrInvalid
	}
	if _, loaded := p.agentTestGates.LoadOrStore("setup", gate); loaded {
		return operations.ErrConflict
	}
	return nil
}
func (p *Platform) operationAdmissionTag(tag envelope.Tag) error {
	if p.Operations.Control(tag.Tenant, "").Paused {
		return operations.ErrPaused
	}
	if !p.AgentControl(tag.Tenant, tag.Agent).Paused && p.agentReady(tag.Tenant, tag.Agent) {
		return nil
	}
	if tag.Meta[operations.MetaForceAsk] == "true" {
		if v, ok := p.agentTestGates.Load("setup"); ok && v.(AgentTestGate).AllowTag(tag) {
			return nil
		}
	}
	return operations.ErrPaused
}
func (p *Platform) operationAdmissionRequest(req runtime.Request) error {
	if p.Operations.Control(req.Tenant, "").Paused {
		return operations.ErrPaused
	}
	if !p.Operations.Control(req.Tenant, legacy.BuiltinAgent(req.Agent())).Paused && p.agentReady(req.Tenant, req.Agent()) {
		return nil
	}
	force := p.operationForced(req)
	if force {
		if v, ok := p.agentTestGates.Load("setup"); ok && v.(AgentTestGate).AllowRequest(req) {
			return nil
		}
	}
	return operations.ErrPaused
}

func (p *Platform) agentReady(tenant, agent string) bool {
	if v, ok := p.agentTestGates.Load("setup"); ok {
		return v.(AgentTestGate).Ready(tenant, agent)
	}
	return true
}
