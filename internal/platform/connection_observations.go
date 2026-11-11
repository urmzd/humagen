package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/state"
)

type connectionObservation struct {
	Revision string
	Health   ConnectionHealth
}

func connectionObservationKey(who auth.Principal, name string, client bool) string {
	owner := connectionOwner(who, name)
	if client || name == "github" || name == "slack" {
		owner = connectionauth.WorkspaceOwner
	}
	kind := "provider"
	if client {
		kind = "client"
	}
	sum := sha256.Sum256([]byte(owner + "\x00" + kind + "\x00" + name))
	return hex.EncodeToString(sum[:])
}
func (p *Platform) connectionHealthRevision(who auth.Principal, name string, client bool) string {
	if !client && name != "slack" && name != "workspace-slack" && name != "github" {
		return p.ConnectionAuth.Revision(who.Tenant, connectionOwner(who, name), name)
	}
	if !who.Can(auth.ClientsRead) {
		return ""
	}
	return p.clientsHealthRevision(who.Tenant, name, client)
}

// clientsHealthRevision is the revision of a client's record, or of every
// installed client of a surface, that a health observation is good for.
func (p *Platform) clientsHealthRevision(tenant, name string, client bool) string {
	if p.Clients == nil {
		return ""
	}
	all, err := p.Clients.List(tenant)
	if err != nil {
		return ""
	}
	parts := []any{}
	for _, held := range all {
		if client && held.ID != name || !client && (string(held.Surface) != strings.TrimPrefix(name, "workspace-") || held.Managed != clients.ManagedInstall) {
			continue
		}
		part := []any{held.ID, held.Version}
		if held.Credentials.Token.Ref == slackOAuthRef {
			provider, marker, err := p.ConnectionAuth.Binding(tenant, connectionauth.WorkspaceOwner, "client:"+held.ID)
			if err != nil || marker == "" {
				return ""
			}
			part = append(part, p.ConnectionAuth.Revision(tenant, connectionauth.WorkspaceOwner, provider))
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return ""
	}
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (p *Platform) saveConnectionHealth(who auth.Principal, name string, client bool, revision string, result ConnectionHealth) error {
	if revision == "" || result.CheckedAt.IsZero() || revision != p.connectionHealthRevision(who, name, client) {
		return nil
	}
	if !p.connectionHealth.Enabled() {
		return nil
	}
	return p.putConnectionHealth(who.Tenant, connectionObservationKey(who, name, client), connectionObservation{Revision: revision, Health: result})
}

// putConnectionHealth keeps an observation unless a newer one of the same
// revision is kept already.
func (p *Platform) putConnectionHealth(tenant, key string, observation connectionObservation) error {
	for range 4 {
		row, err := p.connectionHealth.Lookup(tenant, key)
		batch := p.connectionHealth.Batch()
		if errors.Is(err, state.ErrNotFound) {
			batch.Create(tenant, key, observation)
		} else if err != nil {
			return err
		} else {
			if row.Value.Revision == observation.Revision && row.Value.Health.CheckedAt.After(observation.Health.CheckedAt) {
				return nil
			}
			batch.PutAt(tenant, key, observation, row.Version)
		}
		if err = batch.Commit(); !errors.Is(err, state.ErrConflict) {
			return err
		}
	}
	return state.ErrConflict
}
func (p *Platform) lastConnectionHealth(who auth.Principal, name string, client bool) (*ConnectionHealth, error) {
	revision := p.connectionHealthRevision(who, name, client)
	if revision == "" {
		return nil, nil
	}
	row, err := p.connectionHealth.Lookup(who.Tenant, connectionObservationKey(who, name, client))
	if errors.Is(err, state.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if row.Value.Revision != revision {
		return nil, nil
	}
	if row.Value.Health.Healthy && p.connectionNeedsReconnect(who, name, client) {
		return nil, nil
	}
	return &row.Value.Health, nil
}

func (p *Platform) connectionNeedsReconnect(who auth.Principal, name string, client bool) bool {
	if !client && name != "slack" && name != "workspace-slack" && name != "github" {
		status, err := p.ConnectionAuth.Status(who.Tenant, connectionOwner(who, name), name)
		return err != nil || status.ReconnectRequired
	}
	if p.Clients == nil {
		return false
	}
	all, err := p.Clients.List(who.Tenant)
	if err != nil {
		return true
	}
	for _, c := range all {
		if client && c.ID != name || !client && string(c.Surface) != strings.TrimPrefix(name, "workspace-") {
			continue
		}
		if c.Credentials.Token.Ref != slackOAuthRef {
			continue
		}
		provider, marker, err := p.ConnectionAuth.Binding(who.Tenant, connectionauth.WorkspaceOwner, "client:"+c.ID)
		if err != nil || marker == "" {
			return true
		}
		status, err := p.ConnectionAuth.Status(who.Tenant, connectionauth.WorkspaceOwner, provider)
		if err != nil || status.ReconnectRequired {
			return true
		}
	}
	return false
}
