package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/notifications"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

type emailPreferencesRelay struct{ calls atomic.Int64 }

func (s *emailPreferencesRelay) Send(context.Context, notifications.EmailMessage) error {
	s.calls.Add(1)
	return errors.New("no email is sent by HTTP preference tests")
}

func emailPreferencesConfig(t *testing.T) (platform.Config, *emailPreferencesRelay) {
	t.Helper()
	policy, err := auth.ParsePolicy("email-preferences-test", `role viewer { allow read on * } grant viewer to subject "ana" grant viewer to subject "unmapped"`)
	if err != nil {
		t.Fatal(err)
	}
	relay := &emailPreferencesRelay{}
	return platform.Config{Policy: policy, Email: &platform.EmailConfig{Sender: relay, PublicURL: "https://private-relay.example.test", Recipients: map[string]map[string]string{"acme": {"ana": "ana-notifications@example.test", "signed-in-only": "session-only@example.test"}}}}, relay
}

func emailPreferenceBody(enabled bool, version int64) string {
	return fmt.Sprintf(`{"display_name":"Ana","theme":"system","accent":"purple","personal":{"email":%t,"email_version":%d,"notifications":{},"timezone":"UTC"}}`, enabled, version)
}
func emailPreferenceSnapshot(t *testing.T, f *fixture, token string) (bool, int64) {
	t.Helper()
	body := want(t, f, token, "GET", "/api/preferences", "", http.StatusOK)
	personal, ok := body["personal"].(map[string]any)
	if !ok {
		t.Fatal("missing personal settings")
	}
	enabled, _ := personal["email"].(bool)
	version, _ := personal["email_version"].(float64)
	return enabled, int64(version)
}
func emailPreferencesNoMailbox(t *testing.T, value any) {
	t.Helper()
	wire, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"ana-notifications@example.test", "session-only@example.test", "private-relay.example.test"} {
		if strings.Contains(string(wire), private) {
			t.Fatal("operator-owned email configuration leaked into a DTO")
		}
	}
}

func TestEmailPreferencesDefaultDenyWithoutOperatorConfiguration(t *testing.T) {
	f := newFixture(t)
	ana := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	me := want(t, f, ana, "GET", "/api/me", "", http.StatusOK)
	if me["email_notifications_available"] != false {
		t.Fatal("unconfigured email appeared available")
	}
	if enabled, version := emailPreferenceSnapshot(t, f, ana); enabled || version != 0 {
		t.Fatal("new human was opted in by default")
	}
	want(t, f, ana, "PUT", "/api/preferences", emailPreferenceBody(true, 0), http.StatusBadRequest)
	if enabled, _ := emailPreferenceSnapshot(t, f, ana); enabled {
		t.Fatal("unavailable opt-in persisted")
	}
	want(t, f, "", "GET", "/api/preferences", "", http.StatusUnauthorized)
}

func TestEmailCapabilityRequiresMappedHumanAndStandingPolicy(t *testing.T) {
	cfg, relay := emailPreferencesConfig(t)
	f := newFixtureWith(t, cfg)
	for _, tc := range []struct {
		name, tenant, subject string
		machine, want         bool
	}{
		{name: "mapped human", tenant: "acme", subject: "ana", want: true},
		{name: "standing but unmapped", tenant: "acme", subject: "unmapped"},
		{name: "group login has no standing grant", tenant: "acme", subject: "signed-in-only"},
		{name: "another tenant", tenant: "other", subject: "ana"},
		{name: "machine using mapped subject", tenant: "acme", subject: "ana", machine: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var token string
			if tc.machine {
				token = f.machineAs(t, tc.subject, tc.tenant, everything...)
			} else {
				token = f.person(t, tc.tenant, tc.subject, "Email Tester", "humagen-admins")
			}
			me := want(t, f, token, "GET", "/api/me", "", http.StatusOK)
			if me["email_notifications_available"] != tc.want {
				t.Fatalf("email availability=%v, want%v", me["email_notifications_available"], tc.want)
			}
			emailPreferencesNoMailbox(t, me)
			if !tc.want {
				want(t, f, token, "PUT", "/api/preferences", emailPreferenceBody(true, 0), http.StatusBadRequest)
			}
		})
	}
	if relay.calls.Load() != 0 {
		t.Fatal("capability checks sent email")
	}
}

func TestEmailPreferenceCASAndPrivateAuditThroughHTTP(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		cfg, relay := emailPreferencesConfig(t)
		cfg.Backends.State = open(t)
		f := newFixtureWith(t, cfg)
		ana := f.person(t, "acme", "ana", "Ana", "humagen-admins")
		ben := f.person(t, "acme", "ben", "Ben", "humagen-admins")
		if enabled, version := emailPreferenceSnapshot(t, f, ana); enabled || version != 0 {
			t.Fatal("email not default deny")
		}
		saved := want(t, f, ana, "PUT", "/api/preferences", emailPreferenceBody(true, 0), http.StatusOK)
		emailPreferencesNoMailbox(t, saved)
		if enabled, version := emailPreferenceSnapshot(t, f, ana); !enabled || version != 1 {
			t.Fatalf("opt-in=%v version%d", enabled, version)
		}
		want(t, f, ana, "PUT", "/api/preferences", emailPreferenceBody(false, 0), http.StatusConflict)
		if enabled, version := emailPreferenceSnapshot(t, f, ana); !enabled || version != 1 {
			t.Fatal("stale opt-out changed preferences")
		}
		want(t, f, ana, "PUT", "/api/preferences", emailPreferenceBody(false, 1), http.StatusOK)
		if enabled, version := emailPreferenceSnapshot(t, f, ana); enabled || version != 2 {
			t.Fatalf("opt-out=%v version%d", enabled, version)
		}
		want(t, f, ana, "PUT", "/api/preferences", emailPreferenceBody(true, 0), http.StatusConflict)
		if enabled, version := emailPreferenceSnapshot(t, f, ana); enabled || version != 2 {
			t.Fatal("stale form re-enabled opted-out email")
		}
		want(t, f, ana, "PUT", "/api/preferences", `{"theme":"dark","accent":"purple"}`, http.StatusOK)
		if enabled, version := emailPreferenceSnapshot(t, f, ana); enabled || version != 2 {
			t.Fatal("profile edit reset email preference or version")
		}
		for _, body := range []string{`{"personal":{"email":true,"email_version":2,"recipient":"attacker@example.test"}}`, `{"subject":"ben","personal":{"email":true,"email_version":2}}`, `{"tenant":"other","personal":{"email":true,"email_version":2}}`} {
			want(t, f, ana, "PUT", "/api/preferences", body, http.StatusBadRequest)
		}
		own := want(t, f, ana, "GET", "/api/audit?resource=notification_email", "", http.StatusOK)
		records := own["records"].([]any)
		if len(records) != 2 {
			t.Fatalf("expected opt-in and opt-out audit records, got%d", len(records))
		}
		outcomes := map[string]bool{}
		for _, raw := range records {
			row := raw.(map[string]any)
			if row["actor"] != "ana" || row["subject"] != "ana" || row["scope"] != "personal" || row["outcome"] != "succeeded" || row["action"] != "notifications.email.preference" {
				t.Fatalf("wrong preference provenance: %+v", row)
			}
			if row["route"] != "PUT /api/preferences" || row["rule"] == nil || row["rule"] == "" {
				t.Fatal("preference audit lacks real route/rule provenance")
			}
			after := row["after"].(map[string]any)
			outcomes[fmt.Sprint(after["enabled"])] = true
		}
		if !outcomes["true"] || !outcomes["false"] {
			t.Fatal("audit does not distinguish opt-in and opt-out")
		}
		emailPreferencesNoMailbox(t, own)
		other := want(t, f, ben, "GET", "/api/audit?resource=notification_email", "", http.StatusOK)
		if len(other["records"].([]any)) != 0 {
			t.Fatal("another administrator saw private email preference history")
		}
		emailPreferencesNoMailbox(t, other)
		if relay.calls.Load() != 0 {
			t.Fatal("preference mutations themselves sent email")
		}
	})
}
