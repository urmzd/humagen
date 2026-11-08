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
	if p.Clients == nil || !who.Can(auth.ClientsRead) {
		return ""
	}
	all, err := p.Clients.List(who.Tenant)
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
			provider, marker, err := p.ConnectionAuth.Binding(who.Tenant, connectionauth.WorkspaceOwner, "client:"+held.ID)
			if err != nil || marker == "" {
				return ""
			}
			part = append(part, p.ConnectionAuth.Revision(who.Tenant, connectionauth.WorkspaceOwner, provider))
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
	key := connectionObservationKey(who, name, client)
	observation := connectionObservation{Revision: revision, Health: result}
	for range 4 {
		row, err := p.connectionHealth.Lookup(who.Tenant, key)
		batch := p.connectionHealth.Batch()
		if errors.Is(err, state.ErrNotFound) {
			batch.Create(who.Tenant, key, observation)
		} else if err != nil {
			return err
		} else {
			if row.Value.Revision == revision && row.Value.Health.CheckedAt.After(result.CheckedAt) {
				return nil
			}
			batch.PutAt(who.Tenant, key, observation, row.Version)
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
