package platform_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestNativeNotificationsJourneyUsesCurrentReaderAndNeverChangesInbox(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		now := time.Date(2026, 10, 26, 12, 10, 0, 0, time.UTC)
		cfg := platform.Config{Workspace: t.TempDir(), Backends: platform.Backends{State: st}, Now: func() time.Time { return now }}
		p, err := platform.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		firstPlatform := p
		t.Cleanup(func() { _ = firstPlatform.Close() })
		who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Operator}}
		pr, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: "Private Notifications", Members: []projects.Entry{{Who: "ada", Role: projects.Owner}}})
		if err != nil {
			t.Fatal(err)
		}
		policy := prefs.DefaultPersonal()
		policy.Notifications["alert"] = prefs.Immediate
		if _, err := p.Prefs.SetPersonal(tenant, who.Subject, policy); err != nil {
			t.Fatal(err)
		}
		raise := func(key string) inbox.Message {
			t.Helper()
			m, _, err := p.Inbox.Raise(tenant, key, inbox.Message{Kind: inbox.KindAlert, To: who.Subject, Subject: "Private notification body must never be persisted by delivery", Source: inbox.Source{Project: pr.Name}, Parts: []inbox.Part{{Type: inbox.PartAlert, Alert: &inbox.AlertPart{ID: key, Rule: "rule", Kind: "run_failed", Severity: "warning", Title: "Alert"}}}}, now)
			if err != nil {
				t.Fatal(err)
			}
			return m
		}
		first := raise("immediate")
		rows, err := p.NativeNotifications(who)
		if err != nil || len(rows) != 1 {
			t.Fatalf("immediate: %+v %v", rows, err)
		}
		id := rows[0].ID
		// Rebuild all read models and the delivery queue from the same backend.
		again, err := platform.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = again.Close() })
		p = again
		rows, err = p.NativeNotifications(who)
		if err != nil || len(rows) != 1 || rows[0].ID != id {
			t.Fatalf("restart duplicated delivery: %+v %v", rows, err)
		}
		if err := p.AcknowledgeNotification(who, id); err != nil {
			t.Fatal(err)
		}
		if rows, err = p.NativeNotifications(who); err != nil || len(rows) != 0 {
			t.Fatalf("ack: %+v %v", rows, err)
		}
		original, err := p.Inbox.Get(tenant, first.ID, who.Subject)
		if err != nil || original.State != inbox.Open || !original.Unread {
			t.Fatalf("notification changed authoritative inbox: %+v %v", original, err)
		}
		policy.Notifications["alert"] = prefs.Digest
		if _, err := p.Prefs.SetPersonal(tenant, who.Subject, policy); err != nil {
			t.Fatal(err)
		}
		raise("digest-one")
		raise("digest-two")
		if rows, err = p.NativeNotifications(who); err != nil || len(rows) != 0 {
			t.Fatalf("early digest: %+v %v", rows, err)
		}
		now = now.Add(50 * time.Minute)
		rows, err = p.NativeNotifications(who)
		if err != nil || len(rows) != 1 || rows[0].Mode != prefs.Digest || len(rows[0].Items) != 2 {
			t.Fatalf("hourly digest: %+v %v", rows, err)
		}
		if err := p.AcknowledgeNotification(who, rows[0].ID); err != nil {
			t.Fatal(err)
		}
		policy.Notifications["alert"] = prefs.Off
		if _, err := p.Prefs.SetPersonal(tenant, who.Subject, policy); err != nil {
			t.Fatal(err)
		}
		raise("off")
		if rows, err = p.NativeNotifications(who); err != nil || len(rows) != 0 {
			t.Fatalf("off delivered: %+v %v", rows, err)
		}
		policy.Notifications["alert"] = prefs.Immediate
		policy.Timezone = "America/Toronto"
		policy.QuietStart = "22:00"
		policy.QuietEnd = "07:00"
		if _, err := p.Prefs.SetPersonal(tenant, who.Subject, policy); err != nil {
			t.Fatal(err)
		}
		now = time.Date(2026, 10, 27, 2, 30, 0, 0, time.UTC)
		raise("quiet-private")
		if rows, err = p.NativeNotifications(who); err != nil || len(rows) != 0 {
			t.Fatalf("quiet delivered: %+v %v", rows, err)
		}
		current, _ := p.Projects.Tree(tenant).Get(pr.Name)
		if _, err := p.Projects.Update(tenant, pr.Name, current.Version, govern, now, func(project *projects.Project) error {
			project.Members = []projects.Entry{{Who: "ben", Role: projects.Owner}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		now = time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC)
		if rows, err = p.NativeNotifications(who); err != nil || len(rows) != 0 {
			t.Fatalf("revoked private content delivered: %+v %v", rows, err)
		}
		stored, err := st.Load(context.Background(), "notifications")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range stored {
			if bytes.Contains(row.Value, []byte("Private notification body")) || bytes.Contains(row.Value, []byte(pr.Name)) {
				t.Fatal("private body or project name copied into notification queue")
			}
		}
	})
}
