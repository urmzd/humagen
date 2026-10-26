package platform

import (
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
)

// AuditAttempt observes one already-authorized mutation. A failed Finish leaves
// a durable pending record; callers must not report that a committed mutation
// rolled back merely because its completion marker could not be recorded.
type AuditAttempt struct {
	p      *Platform
	tenant string
	id     string
}

// BeginAudit is called after permission and visibility checks, before mutation.
// Actor and time always come from the server, never the submitted change.
func (p *Platform) BeginAudit(who auth.Principal, change audit.Change) (*AuditAttempt, error) {
	if who.Subject == "" || who.Tenant == "" {
		return nil, auth.ErrUnauthenticated
	}
	attempt, err := p.Audit.Begin(who.Tenant, who.Subject, change, p.now())
	if err != nil {
		return nil, err
	}
	return &AuditAttempt{p: p, tenant: who.Tenant, id: attempt.ID}, nil
}

// ID identifies the attempt in a resource's own change metadata.
func (a *AuditAttempt) ID() string { return a.id }

// Finish observes the domain result. A false successful outcome is never saved.
func (a *AuditAttempt) Finish(result error) error {
	return a.p.Audit.Finish(a.tenant, a.id, result, a.p.now())
}

// RecordAudit records an already-observed successful event. It must not be used
// before a mutation as a promise that the mutation will succeed.
func (p *Platform) RecordAudit(who auth.Principal, change audit.Change) error {
	if who.Subject == "" || who.Tenant == "" {
		return auth.ErrUnauthenticated
	}
	return p.Audit.Record(who.Tenant, who.Subject, change, p.now())
}
