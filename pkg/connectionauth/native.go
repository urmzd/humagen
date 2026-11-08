package connectionauth

import (
	"golang.org/x/oauth2"
	"regexp"
	"strings"
)

var nativeID = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,63}$`)

func nativeToken(tx transaction, tok *oauth2.Token) (map[string]string, bool) {
	if tok == nil || tok.AccessToken == "" {
		return nil, false
	}
	if tx.Native && strings.HasPrefix(strings.TrimPrefix(tx.Provider, "workspace-"), "slack") {
		if ok, exists := tok.Extra("ok").(bool); exists && !ok {
			return nil, false
		}
		if !strings.EqualFold(tok.Type(), "bot") && !strings.EqualFold(tok.Type(), "Bearer") {
			return nil, false
		}
		tok.TokenType = "Bearer"
		team, _ := tok.Extra("team").(map[string]any)
		id, _ := team["id"].(string)
		app, _ := tok.Extra("app_id").(string)
		// Rotation responses may omit installation identity. It is retained
		// from the initial exchange, whose host requires it before creating a client.
		if id == "" && app == "" {
			return nil, true
		}
		if !nativeID.MatchString(id) || !nativeID.MatchString(app) {
			return nil, false
		}
		return map[string]string{"team": id, "app_id": app}, true
	}
	return nil, strings.EqualFold(tok.Type(), "Bearer")
}

// FinalizeSlack moves the short-lived install slot to a verified team-specific
// account, so adding another installation cannot replace an existing team's token.
func (m *Manager) FinalizeSlack(tenant, actor, team, app string) (string, error) {
	if m == nil {
		return "", ErrUnavailable
	}
	oldKey := recordKey("account", WorkspaceOwner, "workspace-slack")
	row, err := m.records.Lookup(tenant, oldKey)
	if err != nil {
		return "", ErrReconnect
	}
	var held account
	if err = m.open(tenant, oldKey, row.Value, &held); err != nil {
		return "", err
	}
	if held.Transaction.Person != actor || held.Details["team"] != team || held.Details["app_id"] != app || !nativeID.MatchString(team) || !strings.HasPrefix(team, "T") {
		return "", ErrReconnect
	}
	provider := "workspace-slack-" + strings.ToLower(team)
	held.Transaction.Provider = provider
	key := recordKey("account", WorkspaceOwner, provider)
	encoded, err := m.seal(tenant, key, held)
	if err != nil {
		return "", err
	}
	batch := m.records.Batch()
	batch.DeleteAt(tenant, oldKey, row.Version)
	batch.Put(tenant, key, encoded)
	if err = batch.Commit(); err != nil {
		return "", ErrReconnect
	}
	return provider, nil
}

func (m *Manager) Details(tenant, owner, provider string) (map[string]string, error) {
	if m == nil {
		return nil, ErrUnavailable
	}
	row, err := m.records.Lookup(tenant, recordKey("account", owner, provider))
	if err != nil {
		return nil, ErrReconnect
	}
	var held account
	if err = m.open(tenant, row.Key, row.Value, &held); err != nil {
		return nil, err
	}
	return held.Details, nil
}

// SigningSecret is deployment-only material, never included in connection APIs.
func (m *Manager) SigningSecret(provider string) string {
	if m == nil {
		return ""
	}
	return m.providers[provider].SigningSecret
}
