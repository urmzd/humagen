package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/threads"
)

// RecordConnectionAuthFailure records attention for the credential's person,
// never for a whole workspace. Callers pass a provider identifier, not an error
// body, URL or token. Repeated failures share one open alert across replicas.
func (p *Platform) RecordConnectionAuthFailure(ctx context.Context, tenant, person, provider string) error {
	if tenant == "" || person == "" || len(tenant) > 200 || len(person) > 200 || strings.ContainsRune(tenant+person, 0) {
		return fmt.Errorf("%w: connection attention needs an owner", alerts.ErrInvalid)
	}
	name, ok := connectionAlertName(provider)
	if !ok {
		return fmt.Errorf("%w: connection provider is unavailable", alerts.ErrInvalid)
	}
	title := "Reconnect " + name
	id, err := p.EnsureAlertSession(ctx, tenant, BuiltinBuilder, "", person, title)
	if err != nil {
		return err
	}
	th, err := p.Threads.Get(tenant, id)
	if err != nil {
		return err
	}
	if th.Visibility != threads.Private || th.CreatedBy != person {
		return fmt.Errorf("%w: connection attention needs a private owner session", alerts.ErrInvalid)
	}
	key := connectionAuthAlertKey(person, provider)
	a, raised, err := p.Alerts.Raise(tenant, key, alerts.Alert{
		Session: id, Project: th.Project, Agent: BuiltinBuilder, Kind: alerts.Kind("connection"),
		Title: title, Body: "Open Connections and reconnect this service to restore access.",
		Priority: alerts.Normal, Severity: alerts.Warning, State: alerts.OpenState,
		Action: "reconnect", ResolveWhen: "The connection has been authorized again.",
		Who: []string{person}, Notify: []string{person}, At: p.now(),
	})
	if err != nil {
		return err
	}
	if !raised {
		// Repair delivery when the alert committed before its transport record.
		all, err := p.Alerts.Alerts(tenant)
		if err != nil {
			return err
		}
		for _, held := range all {
			if held.Rule == key && held.Open() {
				return p.inboxAlertCompletion(tenant, held)
			}
		}
		return nil
	}
	return p.inboxAlertCompletion(tenant, a)
}

// ResolveConnectionAuthFailure is called only after a provider has accepted
// renewed credentials. A health timeout or a local disconnect is not recovery.
func (p *Platform) ResolveConnectionAuthFailure(ctx context.Context, tenant, person, provider string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := connectionAlertName(provider); !ok || tenant == "" || person == "" {
		return alerts.ErrInvalid
	}
	all, err := p.Alerts.Alerts(tenant)
	if err != nil {
		return err
	}
	key := connectionAuthAlertKey(person, provider)
	for _, a := range all {
		if a.Rule != key || !a.Open() {
			continue
		}
		th, err := p.Threads.Get(tenant, a.Session)
		if err != nil {
			return err
		}
		if th.Visibility != threads.Private || th.CreatedBy != person || len(a.Who) != 1 || a.Who[0] != person {
			return alerts.ErrInvalid
		}
		if _, err := p.AckAlert(tenant, a.ID, person); err != nil {
			return err
		}
	}
	return nil
}

func connectionAuthAlertKey(person, provider string) string {
	digest := sha256.Sum256([]byte(person + "\x00" + provider))
	return "connection-auth/" + hex.EncodeToString(digest[:])
}

func connectionAlertName(provider string) (string, bool) {
	if provider == "" || len(provider) > 100 {
		return "", false
	}
	base := strings.TrimPrefix(provider, "workspace-")
	for _, r := range base {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return "", false
		}
	}
	switch base {
	case "github":
		return "GitHub", true
	case "slack":
		return "Slack", true
	case "drive":
		return "Google Drive", true
	case "notion":
		return "Notion", true
	case "linear":
		return "Linear", true
	case "bigquery":
		return "BigQuery", true
	case "granola":
		return "Granola", true
	}
	if strings.HasPrefix(base, "other-") || strings.HasPrefix(base, "slack-") {
		return "your connection", true
	}
	return "", false
}
