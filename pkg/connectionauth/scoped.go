package connectionauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/state"
)

// WorkspaceOwner cannot be an authenticated person's subject: public actor
// validation refuses NUL. Only an authorized platform action supplies this key.
const WorkspaceOwner = "\x00workspace"
const MaxAccounts = 128

type Connection struct {
	Provider string
	Resource string
	Status
}

// OtherProvider makes an endpoint-bound name without exposing a credential or
// changing the manager's deployment-wide provider configuration.
func OtherProvider(resource string) string {
	digest := sha256.Sum256([]byte(resource))
	return "other-" + hex.EncodeToString(digest[:12])
}

// BeginFor binds consent to its authenticated actor and its explicit owner.
// The platform, not browser input, authorizes use of WorkspaceOwner.
func (m *Manager) BeginFor(ctx context.Context, tenant, actor, owner, provider, resource string) (Authorization, error) {
	return m.BeginRegistered(ctx, tenant, actor, owner, provider, resource, nil)
}

// BeginRegistered accepts an issuer-bound client only for an explicit custom service.
func (m *Manager) BeginRegistered(ctx context.Context, tenant, actor, owner, provider, resource string, registration *Registration) (Authorization, error) {
	if m == nil {
		return Authorization{}, ErrUnavailable
	}
	if !identity(tenant, actor) || (owner != actor && owner != WorkspaceOwner) {
		return Authorization{}, ErrInvalid
	}
	base := strings.TrimPrefix(provider, "workspace-")
	p, ok := m.providers[base]
	if base == "other" || strings.HasPrefix(base, "other-") {
		if !safeURL(resource) {
			return Authorization{}, fmt.Errorf("%w: a public HTTPS tool endpoint is required", ErrInvalid)
		}
		id := OtherProvider(resource)
		if base != "other" && base != id {
			return Authorization{}, ErrInvalid
		}
		base = id
		p = Provider{Name: id, Resource: resource}
		if registration != nil {
			if !registration.valid() {
				return Authorization{}, ErrInvalid
			}
			p.Issuer, p.ClientID, p.ClientSecret = registration.Issuer, registration.ClientID, registration.ClientSecret
		} else {
			accountProvider := id
			if owner == WorkspaceOwner {
				accountProvider = "workspace-" + id
			}
			if row, err := m.records.Lookup(tenant, recordKey("account", owner, accountProvider)); err == nil {
				var held account
				if m.open(tenant, row.Key, row.Value, &held) != nil || held.Transaction.Resource != resource {
					return Authorization{}, ErrReconnect
				}
				p.Issuer, p.ClientID, p.ClientSecret = held.Transaction.Issuer, held.Transaction.ClientID, held.Transaction.ClientSecret
			} else if !errors.Is(err, state.ErrNotFound) {
				return Authorization{}, err
			}
		}
	} else if registration != nil || !ok || (resource != "" && resource != p.Resource) {
		return Authorization{}, ErrInvalid
	}
	provider = base
	if owner == WorkspaceOwner {
		provider = "workspace-" + base
	}
	all, err := m.Connections(tenant, owner)
	if err != nil {
		return Authorization{}, err
	}
	if len(all) >= MaxAccounts {
		exists := false
		for _, held := range all {
			if held.Provider == provider {
				exists = true
			}
		}
		if !exists {
			return Authorization{}, fmt.Errorf("%w: disconnect an account before adding another", ErrInvalid)
		}
	}
	if err = m.limitPending(tenant, owner, provider); err != nil {
		return Authorization{}, err
	}
	return m.begin(ctx, tenant, actor, owner, provider, p)
}

// Connections lists only the selected owner's encrypted records in one tenant.
func (m *Manager) Connections(tenant, owner string) ([]Connection, error) {
	out := []Connection{}
	if m == nil {
		return out, nil
	}
	if !identity(tenant, owner) && (owner != WorkspaceOwner || !identity(tenant, "workspace")) {
		return nil, ErrInvalid
	}
	records, _, err := m.records.List(tenant, state.Query{Prefix: ownerPrefix("account", owner), Limit: MaxAccounts})
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		var held account
		if err = m.open(tenant, record.Key, record.Value, &held); err != nil {
			return nil, err
		}
		if held.Transaction.Owner != owner || record.Key != recordKey("account", owner, held.Transaction.Provider) {
			return nil, ErrReconnect
		}
		status, err := m.Status(tenant, owner, held.Transaction.Provider)
		if err != nil {
			return nil, err
		}
		out = append(out, Connection{Provider: held.Transaction.Provider, Resource: held.Transaction.Resource, Status: status})
	}
	return out, nil
}

// Resource binds a saved token to its consented endpoint before any HTTP call.
func (m *Manager) Resource(tenant, owner, provider string) (string, error) {
	if m == nil {
		return "", ErrUnavailable
	}
	record, err := m.records.Lookup(tenant, recordKey("account", owner, provider))
	if err != nil {
		return "", ErrReconnect
	}
	var held account
	if err = m.open(tenant, record.Key, record.Value, &held); err != nil {
		return "", err
	}
	if held.Transaction.Owner != owner || held.Transaction.Provider != provider {
		return "", ErrReconnect
	}
	return held.Transaction.Resource, nil
}

// PendingResource lets the host recheck current egress grants before consuming
// a callback. Actor, owner, nonce and expiry match the eventual exchange.
func (m *Manager) PendingResource(tenant, actor, owner, provider, nonce string) (string, error) {
	if m == nil {
		return "", ErrUnavailable
	}
	if !identity(tenant, actor) || (owner != actor && owner != WorkspaceOwner) {
		return "", ErrInvalid
	}
	key := recordKey("pending", owner, provider)
	item, err := m.records.Lookup(tenant, key)
	if err != nil {
		return "", ErrInvalid
	}
	var pending struct {
		State       string
		Transaction transaction
	}
	if m.open(tenant, key, item.Value, &pending) != nil || pending.State != nonce || pending.Transaction.Person != actor || pending.Transaction.Owner != owner || pending.Transaction.Provider != provider || !m.now().Before(pending.Transaction.Expires) {
		return "", ErrInvalid
	}
	return pending.Transaction.Resource, nil
}

func (m *Manager) limitPending(tenant, owner, provider string) error {
	rows, _, err := m.records.List(tenant, state.Query{Prefix: ownerPrefix("pending", owner), Limit: MaxAccounts})
	if err != nil {
		return err
	}
	count := 0
	exists := false
	batch := m.records.Batch()
	for _, row := range rows {
		var pending struct {
			State       string
			Transaction transaction
		}
		if err = m.open(tenant, row.Key, row.Value, &pending); err != nil {
			return err
		}
		if !m.now().Before(pending.Transaction.Expires) {
			batch.DeleteAt(tenant, row.Key, row.Version)
			continue
		}
		count++
		if row.Key == recordKey("pending", owner, provider) {
			exists = true
		}
	}
	if err = batch.Commit(); err != nil {
		return err
	}
	if count >= MaxAccounts && !exists {
		return fmt.Errorf("%w: too many pending connections; finish one or wait ten minutes", ErrInvalid)
	}
	return nil
}
