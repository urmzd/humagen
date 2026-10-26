package platform

import (
	"context"
	"fmt"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"slices"
)

type InstallationEvent struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}
type InstallationAction struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Category    string `json:"category"`
}
type InstallationCapabilities struct {
	Version         int                  `json:"version"`
	Managed         bool                 `json:"managed"`
	Legacy          bool                 `json:"legacy"`
	Events          []InstallationEvent  `json:"events"`
	Actions         []InstallationAction `json:"actions"`
	DeliverOutcomes bool                 `json:"deliver_outcomes"`
}

func (p *Platform) ClientCapabilities(tenant, id string) (InstallationCapabilities, error) {
	c, err := p.Clients.Get(tenant, id)
	if err != nil {
		return InstallationCapabilities{}, err
	}
	out := InstallationCapabilities{Version: c.Version, Managed: c.Managed != "", Legacy: c.Capabilities == nil, Events: []InstallationEvent{}, Actions: []InstallationAction{}, DeliverOutcomes: c.AllowsOutcomes()}
	for _, id := range clients.EventIDs(c.Surface) {
		out.Events = append(out.Events, InstallationEvent{ID: id, Enabled: c.AllowsEvent(id)})
	}
	if st := p.surfaces[string(c.Surface)].seat(tenant, id); st != nil {
		for _, a := range st.peer.Actions() {
			if len(out.Actions) >= 32 {
				break
			}
			out.Actions = append(out.Actions, InstallationAction{ID: a.Name, Description: a.Description, Enabled: c.AllowsAction(a.Name), Category: string(categoryOf(a.Name))})
		}
	}
	return out, nil
}
func (p *Platform) SetClientCapabilities(_ context.Context, who auth.Principal, id string, version int, next clients.Capabilities) (InstallationCapabilities, error) {
	if !who.Can(auth.ClientsWrite) {
		return InstallationCapabilities{}, auth.ErrForbidden
	}
	p.clientsMu.Lock()
	defer p.clientsMu.Unlock()
	c, err := p.Clients.Get(who.Tenant, id)
	if err != nil {
		return InstallationCapabilities{}, err
	}
	if c.Managed != "" {
		return InstallationCapabilities{}, clients.ErrManaged
	}
	if c.Version != version {
		return InstallationCapabilities{}, fmt.Errorf("%w: installation changed; reload", clients.ErrExists)
	}
	inventory, err := p.ClientCapabilities(who.Tenant, id)
	if err != nil {
		return InstallationCapabilities{}, err
	}
	for _, name := range next.Actions {
		if !slices.ContainsFunc(inventory.Actions, func(a InstallationAction) bool { return a.ID == name }) {
			return InstallationCapabilities{}, fmt.Errorf("%w: action is not registered by this installation", clients.ErrInvalid)
		}
	}
	c.Capabilities = &next
	if err := c.Validate(); err != nil {
		return InstallationCapabilities{}, err
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "client.capabilities", Resource: "client", ID: id, Route: "PUT /api/clients/{id}/capabilities", Rule: string(auth.ClientsWrite), After: map[string]string{"events": fmt.Sprint(len(next.Events)), "actions": fmt.Sprint(len(next.Actions)), "deliver_outcomes": fmt.Sprint(next.DeliverOutcomes)}})
	if err != nil {
		return InstallationCapabilities{}, err
	}
	_, err = p.Clients.Update(who.Tenant, c, p.now())
	if done := attempt.Finish(err); done != nil {
		p.LogAuditCompletionFailure(done)
	}
	if err != nil {
		return InstallationCapabilities{}, err
	}
	return p.ClientCapabilities(who.Tenant, id)
}
