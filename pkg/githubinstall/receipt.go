package githubinstall

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/state"
)

// OwnerReceipt is private host metadata from a verified callback. It must not
// be included in connection DTOs. The external GitHub user ID is not an owner.
type OwnerReceipt struct {
	Person     string
	Generation string
}

type installationReceipt struct {
	Person       string       `json:"person"`
	Generation   string       `json:"generation"`
	Binding      string       `json:"binding"`
	Epoch        string       `json:"epoch"`
	Installation Installation `json:"installation"`
}

func receiptKey(app, installation string) string { return "owner:" + hash(app+"\x00"+installation) }

// epoch fences all pending callbacks and owner receipts on disconnect. Keeping
// a tombstone prevents an earlier provider response from reviving a grant.
func (m *Manager) epoch(ctx context.Context, tenant string) (state.Record, error) {
	for range 3 {
		r, err := m.store.Lookup(ctx, namespace, tenant, "epoch")
		if err == nil {
			return r, nil
		}
		if !errors.Is(err, state.ErrNotFound) {
			return state.Record{}, err
		}
		value, err := receiptNonce()
		if err != nil {
			return state.Record{}, err
		}
		if err = m.store.Write(ctx, namespace, state.Create(tenant, "epoch", value)); err != nil && !errors.Is(err, state.ErrConflict) {
			return state.Record{}, err
		}
	}
	return state.Record{}, connectionauth.ErrBusy
}

func receiptNonce() ([]byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	return []byte(hex.EncodeToString(raw)), nil
}

// Revoke invalidates receipts and in-flight callbacks without inferring an
// owner for legacy installations. It does not uninstall anything in GitHub.
func (m *Manager) Revoke(ctx context.Context, tenant string) error {
	if m == nil {
		return connectionauth.ErrUnavailable
	}
	if !identity(tenant, "owner") {
		return connectionauth.ErrInvalid
	}
	value, err := receiptNonce()
	if err != nil {
		return err
	}
	return m.store.Write(ctx, namespace, state.Put(tenant, "epoch", value))
}

// Receipt returns only a currently verified consenting person and generation.
// Missing older receipts, another app, and disconnected receipts fail closed.
func (m *Manager) Receipt(ctx context.Context, tenant, app, installation string) (OwnerReceipt, error) {
	if m == nil {
		return OwnerReceipt{}, connectionauth.ErrUnavailable
	}
	if !identity(tenant, "owner") || app != m.cfg.AppID || !number(installation) {
		return OwnerReceipt{}, connectionauth.ErrInvalid
	}
	r, err := m.store.Lookup(ctx, namespace, tenant, receiptKey(app, installation))
	if err != nil {
		return OwnerReceipt{}, err
	}
	epoch, err := m.store.Lookup(ctx, namespace, tenant, "epoch")
	if err != nil {
		return OwnerReceipt{}, err
	}
	var saved installationReceipt
	if json.Unmarshal(r.Value, &saved) != nil || !identity(tenant, saved.Person) || len(saved.Generation) != 64 || saved.Binding != m.binding || saved.Epoch == "" || saved.Epoch != string(epoch.Value) || saved.Installation.AppID != app || saved.Installation.ID != installation {
		return OwnerReceipt{}, connectionauth.ErrReconnect
	}
	return OwnerReceipt{Person: saved.Person, Generation: saved.Generation}, nil
}
