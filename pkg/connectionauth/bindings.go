package connectionauth

import (
	"context"
	"errors"

	"github.com/urmzd/humagen/pkg/state"
)

type binding struct{ Provider, Grant string }

// Bind records explicit consent to use an account for one host-defined target.
// A source target includes its provider, origin and project. Reconnection or
// revocation invalidates this consent until the person explicitly binds again.
func (m *Manager) Bind(tenant, person, target, provider string) error {
	if m == nil {
		return ErrUnavailable
	}
	if (!identity(tenant, person) && (person != WorkspaceOwner || !identity(tenant, "workspace"))) || target == "" || len(target) > 200 {
		return ErrInvalid
	}
	row, err := m.records.Lookup(tenant, recordKey("account", person, provider))
	if err != nil {
		return ErrReconnect
	}
	var account account
	if err = m.open(tenant, row.Key, row.Value, &account); err != nil {
		return err
	}
	if account.Transaction.Owner != person || account.Transaction.Provider != provider || account.Transaction.Grant == "" {
		return ErrReconnect
	}
	key := recordKey("binding", person, target)
	encoded, err := m.seal(tenant, key, binding{provider, account.Transaction.Grant})
	if err != nil {
		return err
	}
	return m.records.Put(tenant, key, encoded)
}

// Binding identifies consent without returning a token. The opaque marker is
// for a host's before/after check around a provider read, never for browser use.
func (m *Manager) Binding(tenant, person, target string) (provider, marker string, err error) {
	if m == nil {
		return "", "", nil
	}
	row, err := m.records.Lookup(tenant, recordKey("binding", person, target))
	if errors.Is(err, state.ErrNotFound) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	var held binding
	if err = m.open(tenant, row.Key, row.Value, &held); err != nil {
		return "", "", err
	}
	accountRow, err := m.records.Lookup(tenant, recordKey("account", person, held.Provider))
	if err != nil {
		return "", "", ErrReconnect
	}
	var account account
	if err = m.open(tenant, accountRow.Key, accountRow.Value, &account); err != nil {
		return "", "", err
	}
	if held.Grant == "" || account.Transaction.Grant != held.Grant || account.Transaction.Owner != person || account.Transaction.Provider != held.Provider {
		return "", "", ErrReconnect
	}
	return held.Provider, held.Grant, nil
}

func (m *Manager) Unbind(tenant, person, target string) error {
	if m == nil {
		return nil
	}
	return m.records.Delete(tenant, recordKey("binding", person, target))
}

func (m *Manager) BoundToken(ctx context.Context, tenant, person, target, provider string) (string, error) {
	held, marker, err := m.Binding(tenant, person, target)
	if err != nil || marker == "" || held != provider {
		return "", ErrReconnect
	}
	token, err := m.Token(ctx, tenant, person, provider)
	if err != nil {
		return "", err
	}
	next, current, err := m.Binding(tenant, person, target)
	if err != nil || next != held || current != marker {
		return "", ErrReconnect
	}
	return token, nil
}
