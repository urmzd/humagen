package platform_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/notifications"
	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

type testEmailRelay struct {
	fail     bool
	messages []notifications.EmailMessage
}

func (s *testEmailRelay) Send(_ context.Context, message notifications.EmailMessage) error {
	s.messages = append(s.messages, message)
	if s.fail {
		return errors.New("test relay unavailable")
	}
	return nil
}

func TestEmailDeliveryRetriesReplaysAndRechecksVisibility(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
		policy, err := auth.ParsePolicy("email-test", `role reader { allow read on * } grant reader to subject "ada"`)
		if err != nil {
			t.Fatal(err)
		}
		relay := &testEmailRelay{fail: true}
		cfg := platform.Config{Workspace: t.TempDir(), Backends: platform.Backends{State: st}, Now: func() time.Time { return now }, Policy: policy, Email: &platform.EmailConfig{Sender: relay, PublicURL: "https://humagen.example.test", Recipients: map[string]map[string]string{tenant: {"ada": "ada@example.test"}}}}
		p, err := platform.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer func(first *platform.Platform) { _ = first.Close() }(p)
		who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{"reader"}}.WithPolicy(policy)
		pr, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: "Private Email", Members: []projects.Entry{{Who: "ada", Role: projects.Owner}}})
		if err != nil {
			t.Fatal(err)
		}
		pref := prefs.DefaultPersonal()
		pref.Email = true
		pref.Notifications["alert"] = prefs.Immediate
		if _, err = p.Prefs.SetPersonal(tenant, "ada", pref); err != nil {
			t.Fatal(err)
		}
		raise := func(id string) {
			t.Helper()
			_, _, err := p.Inbox.Raise(tenant, id, inbox.Message{Kind: inbox.KindAlert, To: "ada", Subject: "Private email body must not be copied", Source: inbox.Source{Project: pr.Name}, Parts: []inbox.Part{{Type: inbox.PartAlert, Alert: &inbox.AlertPart{ID: id, Rule: "rule", Kind: "run_failed", Severity: "warning", Title: "Alert"}}}}, now)
			if err != nil {
				t.Fatal(err)
			}
		}
		raise("one")
		if n, err := p.DeliverEmailNotifications(t.Context()); err == nil || n != 0 {
			t.Fatalf("failed SMTP must remain pending: %d %v", n, err)
		}
		firstID := relay.messages[0].ID
		// Native-console acknowledgment does not acknowledge an email delivery.
		native, err := p.NativeNotifications(who)
		if err != nil || len(native) != 1 {
			t.Fatalf("native queue: %+v %v", native, err)
		}
		if err = p.AcknowledgeNotification(who, native[0].ID); err != nil {
			t.Fatal(err)
		}
		again, err := platform.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = again.Close() }()
		p = again
		relay.fail = false
		if n, err := p.DeliverEmailNotifications(t.Context()); err != nil || n != 1 {
			t.Fatalf("restart retry: %d %v", n, err)
		}
		if relay.messages[1].ID != firstID {
			t.Fatal("restart changed stable relay identity")
		}
		if n, err := p.DeliverEmailNotifications(t.Context()); err != nil || n != 0 {
			t.Fatalf("duplicate delivery: %d %v", n, err)
		}
		pref.Notifications["alert"] = prefs.Digest
		if _, err = p.Prefs.SetPersonal(tenant, "ada", pref); err != nil {
			t.Fatal(err)
		}
		raise("revoked-before-digest")
		if n, err := p.DeliverEmailNotifications(t.Context()); err != nil || n != 0 {
			t.Fatalf("early digest: %d %v", n, err)
		}
		current, _ := p.Projects.Tree(tenant).Get(pr.Name)
		if _, err = p.Projects.Update(tenant, pr.Name, current.Version, govern, now, func(project *projects.Project) error {
			project.Members = []projects.Entry{{Who: "ben", Role: projects.Owner}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		now = now.Add(2 * time.Hour)
		if n, err := p.DeliverEmailNotifications(t.Context()); err != nil || n != 0 {
			t.Fatalf("revoked project delivery: %d %v", n, err)
		}
		for _, message := range relay.messages {
			if bytes.Contains([]byte(message.Body), []byte(pr.Name)) || bytes.Contains([]byte(message.Body), []byte("Private email body")) {
				t.Fatal("private object names or bodies were copied into email")
			}
		}
		rows, err := st.Load(t.Context(), "email_notifications")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if bytes.Contains(row.Value, []byte(pr.Name)) || bytes.Contains(row.Value, []byte("Private email body")) {
				t.Fatal("private body persisted by delivery")
			}
		}
		// A remembered directory administrator cannot replace a standing grant.
		empty, err := auth.ParsePolicy("no-standing-grant", `role reader { allow read on * }`)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Policy = empty
		withoutGrant, err := platform.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = withoutGrant.Close() }()
		withoutGrant.SeePerson(auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}})
		if withoutGrant.EmailAvailable(who) {
			t.Fatal("old sign-in roles enabled background delivery")
		}
	})
}

func TestFailedEmailPreparationUsesLatestOffQuietAndDigestPolicy(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		for _, scenario := range []string{"off", "quiet", "digest"} {
			t.Run(scenario, func(t *testing.T) {
				st := open(t)
				now := time.Date(2026, 10, 26, 12, 10, 0, 0, time.UTC)
				pol, err := auth.ParsePolicy("email-policy", `role reader { allow read on * } grant reader to subject "ada"`)
				if err != nil {
					t.Fatal(err)
				}
				relay := &testEmailRelay{fail: true}
				cfg := platform.Config{Workspace: t.TempDir(), Backends: platform.Backends{State: st}, Now: func() time.Time { return now }, Policy: pol, Email: &platform.EmailConfig{Sender: relay, PublicURL: "https://humagen.example.test", Recipients: map[string]map[string]string{tenant: {"ada": "ada@example.test"}}}}
				p, err := platform.New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = p.Close() }()
				settings := prefs.DefaultPersonal()
				settings.Email = true
				settings.Notifications["alert"] = prefs.Immediate
				if _, err = p.Prefs.SetPersonal(tenant, "ada", settings); err != nil {
					t.Fatal(err)
				}
				_, _, err = p.Inbox.Raise(tenant, "prepared", inbox.Message{Kind: inbox.KindAlert, To: "ada", Subject: "Confined update", Parts: []inbox.Part{{Type: inbox.PartAlert, Alert: &inbox.AlertPart{ID: "a", Rule: "r", Kind: "run_failed", Severity: "warning", Title: "Alert"}}}}, now)
				if err != nil {
					t.Fatal(err)
				}
				if n, err := p.DeliverEmailNotifications(t.Context()); err == nil || n != 0 {
					t.Fatalf("prepare failed relay: %d %v", n, err)
				}
				relay.fail = false
				switch scenario {
				case "off":
					settings.Notifications["alert"] = prefs.Off
				case "quiet":
					settings.QuietStart = "12:00"
					settings.QuietEnd = "13:00"
				case "digest":
					settings.Notifications["alert"] = prefs.Digest
				}
				if _, err = p.Prefs.SetPersonal(tenant, "ada", settings); err != nil {
					t.Fatal(err)
				}
				replay, err := platform.New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = replay.Close() }()
				if n, err := replay.DeliverEmailNotifications(t.Context()); err != nil || n != 0 || len(relay.messages) != 1 {
					t.Fatalf("prepared message ignored changed %s policy: sent=%d attempts=%d error=%v", scenario, n, len(relay.messages), err)
				}
				now = now.Add(time.Hour)
				n, err := replay.DeliverEmailNotifications(t.Context())
				want := 1
				if scenario == "off" {
					want = 0
				}
				if err != nil || n != want {
					t.Fatalf("release after %s window: %d want %d err %v", scenario, n, want, err)
				}
			})
		}
	})
}
