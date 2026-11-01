package platform

import (
	"errors"
	"strconv"
	"strings"

	"github.com/urmzd/playscape/pkg/isolate/egress"

	"github.com/urmzd/humagen/fiber/pkg/isolation"
	"github.com/urmzd/humagen/pkg/audit"
)

// Egress decisions in the audit log (docs/reference/spec.md, Egress).
const (
	// AuditEgressAction is the action every egress decision is recorded
	// under, on resource AuditEgressResource.
	AuditEgressAction   = "sandbox.egress"
	AuditEgressResource = "egress_host"
	// egressActor is who a decision is recorded as when its run names no
	// agent: the sandbox itself.
	egressActor = "sandbox"
)

// AuditEgress records one decision of the egress gate for the instance
// leased with req: allowed as a succeeded record, refused as a failed one
// with the reason. The run's agent and conversation are added when the run
// is known. It records nothing for a request no tenant is known for.
func (p *Platform) AuditEgress(req isolation.Request, d egress.Decision) error {
	if req.Tenant == "" || p.Audit == nil {
		return nil
	}
	actor := egressActor
	change := audit.Change{
		Action:   AuditEgressAction,
		Resource: AuditEgressResource,
		ID:       d.Host + ":" + strconv.Itoa(d.Port),
		Rule:     "run egress ceiling",
		After: map[string]string{
			"method":  d.Method,
			"host":    d.Host,
			"port":    strconv.Itoa(d.Port),
			"allowed": strconv.FormatBool(d.Allowed),
			"run":     req.Run,
		},
	}
	if len(d.Injected) > 0 {
		change.After["injected"] = strings.Join(d.Injected, ",")
	}
	if d.Reason != "" {
		change.After["reason"] = d.Reason
	}
	if p.Runs != nil && req.Run != "" {
		if r, err := p.Runs.Get(req.Tenant, req.Run); err == nil {
			change.Agent, change.Thread, change.Project = r.Agent, r.Thread, r.Project
			if r.Agent != "" {
				actor = "agent:" + r.Agent
			}
		}
	}
	attempt, err := p.Audit.Begin(req.Tenant, actor, change, p.now())
	if err != nil {
		return err
	}
	var result error
	if !d.Allowed {
		result = errors.New("egress refused: " + d.Reason)
	}
	return p.Audit.Finish(req.Tenant, attempt.ID, result, p.now())
}
