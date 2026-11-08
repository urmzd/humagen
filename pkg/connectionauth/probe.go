package connectionauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProbeNative makes one bounded read-only request to a fixed provider API.
// It returns neither the response's account data nor its diagnostic body.
// A supplied HTTP client is a deployment-owned testing/network boundary.
func ProbeNative(ctx context.Context, name, token, team string, provided *http.Client) error {
	method, target := "GET", ""
	switch name {
	case "drive":
		target = "https://www.googleapis.com/drive/v3/about?fields=kind"
	case "bigquery":
		target = "https://bigquery.googleapis.com/bigquery/v2/projects?maxResults=1"
	case "slack":
		method, target = "POST", "https://slack.com/api/auth.test"
	case "github":
		target = "https://api.github.com/installation/repositories?per_page=1"
	default:
		return ErrInvalid
	}
	if token == "" {
		return ErrReconnect
	}
	client := publicClient()
	if provided != nil {
		copy := *provided
		client = &copy
	}
	client.Timeout = 15 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return ErrReconnect
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", "Humagen-connection-check")
	response, err := client.Do(r)
	if err != nil {
		return ErrReconnect
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return errors.Join(ErrReconnect, ErrRejected)
	}
	if response.StatusCode != http.StatusOK {
		return ErrReconnect
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return ErrReconnect
	}
	var result struct {
		Error        string            `json:"error"`
		OK           bool              `json:"ok"`
		Team         string            `json:"team_id"`
		Kind         string            `json:"kind"`
		Repositories []json.RawMessage `json:"repositories"`
		Count        *int              `json:"total_count"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return ErrReconnect
	}
	switch name {
	case "slack":
		if result.Error == "invalid_auth" || result.Error == "token_revoked" || result.Error == "token_expired" || result.Error == "account_inactive" {
			return errors.Join(ErrReconnect, ErrRejected)
		}
		if !result.OK || team == "" || result.Team != team {
			return ErrReconnect
		}
	case "bigquery":
		if result.Kind != "bigquery#projectList" {
			return ErrReconnect
		}
	case "drive":
		if result.Kind != "drive#about" {
			return ErrReconnect
		}
	case "github":
		if result.Count == nil || result.Repositories == nil {
			return ErrReconnect
		}
	}
	return nil
}

func (m *Manager) CheckNative(ctx context.Context, tenant, owner, provider string) error {
	if m == nil {
		return ErrUnavailable
	}
	base := strings.TrimPrefix(provider, "workspace-")
	token, err := m.Token(ctx, tenant, owner, provider)
	if err != nil {
		return err
	}
	team := ""
	if base == "slack" {
		details, err := m.Details(tenant, owner, provider)
		if err != nil {
			return err
		}
		team = details["team"]
	}
	err = ProbeNative(ctx, base, token, team, m.client)
	if errors.Is(err, ErrRejected) {
		m.ReportRejected(ctx, tenant, owner, provider)
	}
	return err
}
