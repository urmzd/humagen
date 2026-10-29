package platform_test

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
)

func TestTheWelcomeIsTheBrandings(t *testing.T) {
	for name, tc := range map[string]struct {
		wording     platform.Wording
		title, text string
	}{
		"Humagen's own":             {platform.Wording{}, "Welcome to Humagen", "personal project"},
		"Humagen's, under a name":   {platform.Wording{Name: "Acme"}, "Welcome to Acme", "personal project"},
		"the workspace's own words": {platform.Wording{Name: "Acme", WelcomeTitle: "Hello from {name}", WelcomeText: "Ask the field team."}, "Hello from Acme", "Ask the field team."},
	} {
		t.Run(name, func(t *testing.T) {
			p, err := platform.New(platform.Config{Backends: platform.Backends{State: state.NewMemory()}, Workspace: t.TempDir(),
				Wording: func(string) platform.Wording { return tc.wording }})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = p.Close() }()
			m, err := p.PostWelcome(tenant)
			if err != nil {
				t.Fatal(err)
			}
			if m.Subject != tc.title || len(m.Parts) == 0 || !strings.Contains(m.Parts[0].Text, tc.text) {
				t.Fatalf("welcome = %q %+v", m.Subject, m.Parts)
			}
		})
	}
}

func TestNotificationEmailsSayTheBrandingsWords(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	policy, err := auth.ParsePolicy("email-test", `role reader { allow read on * } grant reader to subject "ada"`)
	if err != nil {
		t.Fatal(err)
	}
	relay := &testEmailRelay{}
	p, err := platform.New(platform.Config{
		Workspace: t.TempDir(), Backends: platform.Backends{State: state.NewMemory()}, Now: func() time.Time { return now }, Policy: policy,
		Email: &platform.EmailConfig{Sender: relay, PublicURL: "https://acme.example.test", Recipients: map[string]map[string]string{tenant: {"ada": "ada@example.test"}}},
		Wording: func(string) platform.Wording {
			return platform.Wording{Name: "Acme", EmailSingle: "One thing waits for you in {name}."}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	pr, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: "Field", Members: []projects.Entry{{Who: "ada", Role: projects.Owner}}})
	if err != nil {
		t.Fatal(err)
	}
	pref := prefs.DefaultPersonal()
	pref.Email = true
	pref.Notifications["alert"] = prefs.Immediate
	if _, err = p.Prefs.SetPersonal(tenant, "ada", pref); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one"} {
		if _, _, err := p.Inbox.Raise(tenant, id, inbox.Message{Kind: inbox.KindAlert, To: "ada", Subject: "Alert", Source: inbox.Source{Project: pr.Name}, Parts: []inbox.Part{{Type: inbox.PartAlert, Alert: &inbox.AlertPart{ID: id, Rule: "rule", Kind: "run_failed", Severity: "warning", Title: "Alert"}}}}, now); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := p.DeliverEmailNotifications(t.Context()); err != nil || n == 0 {
		t.Fatalf("delivered %d: %v", n, err)
	}
	m := relay.messages[0]
	if m.Subject != "Acme updates" {
		t.Errorf("subject = %q: Humagen's own, under the name", m.Subject)
	}
	if !strings.HasPrefix(m.Body, "One thing waits for you in Acme.") || !strings.Contains(m.Body, "available in Acme") {
		t.Errorf("body = %q", m.Body)
	}
}
