package connectionauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"

	"golang.org/x/oauth2"
)

// Registration is accepted only with explicit custom-service consent. Its
// secret is kept in the encrypted pending/account record and never projected.
type Registration struct {
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

func (r Registration) valid() bool {
	u, err := url.Parse(r.Issuer)
	return err == nil && safeURL(r.Issuer) && u.RawQuery == "" && r.ClientID != "" && len(r.ClientID) <= 4096 && len(r.ClientSecret) <= 8192
}

func grantedScopes(token *oauth2.Token) ([]string, bool) {
	raw, ok := token.Extra("scope").(string)
	if !ok || len(raw) > 32768 {
		return nil, false
	}
	scopes := strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == ',' })
	if len(scopes) > 128 {
		return nil, false
	}
	out := []string{}
	seen := map[string]bool{}
	for _, s := range scopes {
		if len(s) > 512 || strings.IndexFunc(s, unicode.IsControl) >= 0 || (token.AccessToken != "" && strings.Contains(s, token.AccessToken)) || (token.RefreshToken != "" && strings.Contains(s, token.RefreshToken)) {
			return nil, false
		}
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out, true
}

func rejectedToken(err error) bool {
	var response *oauth2.RetrieveError
	if !errors.As(err, &response) {
		return false
	}
	return response.ErrorCode == "invalid_grant" || response.ErrorCode == "invalid_client" || response.Response != nil && (response.Response.StatusCode == 401 || response.Response.StatusCode == 403)
}
func (m *Manager) authFailure(ctx context.Context, tenant string, tx transaction) {
	if m.cfg.OnAuthFailure == nil || !identity(tenant, tx.Person) || !m.currentTransaction(tenant, tx) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = m.cfg.OnAuthFailure(ctx, tenant, tx.Person, tx.Provider)
}

// ReportTokenRejected records an API rejection only for the exact consent and
// access token used by that request. Capture Revision before obtaining the token.
// A late response cannot reject a reconnected or refreshed account.
func (m *Manager) ReportTokenRejected(ctx context.Context, tenant, owner, provider, revision, token string) {
	if m == nil || revision == "" || token == "" {
		return
	}
	row, err := m.records.Lookup(tenant, recordKey("account", owner, provider))
	if err != nil {
		return
	}
	var held account
	if m.open(tenant, row.Key, row.Value, &held) != nil || held.Transaction.Owner != owner || held.Transaction.Provider != provider {
		return
	}
	if grantRevision(held.Transaction.Grant) != revision || held.Token.AccessToken != token || held.LeaseUntil.After(m.now()) {
		return
	}
	if !held.Rejected {
		held.Rejected = true
		encoded, err := m.seal(tenant, row.Key, held)
		if err != nil {
			return
		}
		batch := m.records.Batch()
		batch.PutAt(tenant, row.Key, encoded, row.Version)
		if batch.Commit() != nil {
			return
		}
	}
	m.authFailure(ctx, tenant, held.Transaction)
}

// Revision is an opaque comparison value for observations. It changes on every
// reconnection; it never exposes the grant nonce or a token.
func (m *Manager) Revision(tenant, owner, provider string) string {
	if m == nil {
		return ""
	}
	row, err := m.records.Lookup(tenant, recordKey("account", owner, provider))
	if err != nil {
		return ""
	}
	var held account
	if m.open(tenant, row.Key, row.Value, &held) != nil || held.Transaction.Owner != owner || held.Transaction.Provider != provider {
		return ""
	}
	return grantRevision(held.Transaction.Grant)
}

func grantRevision(grant string) string {
	sum := sha256.Sum256([]byte(grant))
	return hex.EncodeToString(sum[:])
}

func (m *Manager) authRecovered(ctx context.Context, tenant string, tx transaction) {
	if m.cfg.OnAuthRecovered == nil || !identity(tenant, tx.Person) || !m.currentTransaction(tenant, tx) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = m.cfg.OnAuthRecovered(ctx, tenant, tx.Person, tx.Provider)
}

// ReportRecovered is called after a staged native installation commits its final binding.
func (m *Manager) ReportRecovered(ctx context.Context, tenant, owner, provider string) {
	if m == nil {
		return
	}
	row, err := m.records.Lookup(tenant, recordKey("account", owner, provider))
	if err != nil {
		return
	}
	var held account
	if m.open(tenant, row.Key, row.Value, &held) != nil || held.Transaction.Owner != owner || held.Transaction.Provider != provider {
		return
	}
	m.authRecovered(ctx, tenant, held.Transaction)
}

func (m *Manager) currentTransaction(tenant string, tx transaction) bool {
	row, err := m.records.Lookup(tenant, recordKey("account", tx.Owner, tx.Provider))
	if err != nil {
		return false
	}
	var held account
	return m.open(tenant, row.Key, row.Value, &held) == nil && held.Transaction.Grant == tx.Grant && held.Transaction.Person == tx.Person && held.Transaction.Owner == tx.Owner && held.Transaction.Provider == tx.Provider
}
