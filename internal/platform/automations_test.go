package platform_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
)

func TestAutomationRequiresTheInstallationsSignature(t *testing.T) {
	f := &fakes{}
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"lead": {say("Investigated.")}}}, f.config())
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{ID: "automations", Name: "Automations", Surface: clients.GitHub, Enabled: true, Identity: clients.Identity{Owners: []string{"acme"}, Installation: "101"}, DefaultAgent: "lead", GitHub: &clients.GitHubOptions{Automations: []clients.GitHubAutomation{{Name: "triage", Repository: "acme/repo", Event: "issues", Action: "opened", Agent: "lead", Instructions: "Investigate this issue."}}}},
		Token:  clients.SecretInput{Value: "token-automation"}, SigningSecret: clients.SecretInput{Value: "secret-automation"},
	})
	body := `{"action":"opened","installation":{"id":101},"repository":{"full_name":"acme/repo","name":"repo","owner":{"login":"acme"}},"sender":{"login":"human","type":"User"},"issue":{"number":7}}`
	post := func(secret, delivery string) int {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(body))
		r := httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(body))
		r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		r.Header.Set("X-GitHub-Event", "issues")
		r.Header.Set("X-GitHub-Delivery", delivery)
		w := httptest.NewRecorder()
		p.IngressHandler().ServeHTTP(w, r)
		return w.Code
	}
	if got := post("foreign-secret", "wrong"); got != http.StatusUnauthorized {
		t.Fatalf("invalid signature = %d", got)
	}
	id := runtime.RunID(tenant, "automation/automations/wrong/triage")
	if _, err := p.Runs.Get(tenant, id); err == nil {
		t.Fatal("unsigned event raised a run")
	}
	if got := post("secret-automation", "valid"); got != http.StatusOK {
		t.Fatalf("valid signature = %d", got)
	}
	id = runtime.RunID(tenant, "automation/automations/valid/triage")
	run, err := p.Runs.Get(tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	if run.Agent != "lead" || !strings.Contains(run.Text, "#7") {
		t.Fatalf("run = %+v", run)
	}
	if _, err := p.Runs.Get("other-tenant", id); err == nil {
		t.Fatal("run visible across tenants")
	}
	if got := post("secret-automation", "valid"); got != http.StatusOK {
		t.Fatalf("redelivery = %d", got)
	}
}

func TestAnAutomationAnswersOnTheThreadThatTriggeredIt(t *testing.T) {
	f := &fakes{}
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"lead": {say("Looked into it.")}}}, f.config())
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{ID: "gh", Name: "GitHub", Surface: clients.GitHub, Enabled: true, Identity: clients.Identity{Owners: []string{"acme"}, Installation: "101"}, DefaultAgent: "lead", GitHub: &clients.GitHubOptions{Automations: []clients.GitHubAutomation{
			{Name: "triage", Repository: "acme/repo", Event: "issues", Action: "opened", Agent: "lead", Instructions: "Investigate this issue."},
		}}},
		Token: clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	})
	body := `{"action":"opened","installation":{"id":101},"repository":{"full_name":"acme/repo","name":"repo","owner":{"login":"acme"}},"sender":{"login":"human","type":"User"},"issue":{"number":7}}`
	mac := hmac.New(sha256.New, []byte("secret-gh"))
	mac.Write([]byte(body))
	r := httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(body))
	r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	r.Header.Set("X-GitHub-Event", "issues")
	r.Header.Set("X-GitHub-Delivery", "d1")
	w := httptest.NewRecorder()
	p.IngressHandler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("delivery = %d", w.Code)
	}
	run := await(t, p, runtime.RunID(tenant, "automation/gh/d1/triage"), runs.Completed)
	if run.Thread == "" {
		t.Fatal("the automation kept no session")
	}
	eventually(t, "the automation answered on the issue", func() bool {
		for _, c := range f.githubFor("token-gh").Comments() {
			if strings.Contains(c.Body, "Looked into it.") {
				return true
			}
		}
		return false
	})
}

// An automation rule's runs answer with the rule's shape: a rule on
// pull_request reviews whatever its instructions say, unless it says
// reply, and a rule on any other event replies.
func TestAnAutomationRuleCarriesItsShape(t *testing.T) {
	f := &fakes{}
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"lead": {say("Looked."), say("Looked."), say("Looked.")}}}, f.config())
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{ID: "gh", Name: "GitHub", Surface: clients.GitHub, Enabled: true, Identity: clients.Identity{Owners: []string{"acme"}, Installation: "101"}, DefaultAgent: "lead", GitHub: &clients.GitHubOptions{Automations: []clients.GitHubAutomation{
			{Name: "security", Repository: "acme/repo", Event: "pull_request", Action: "opened", Agent: "lead", Instructions: "Check the change for leaked secrets."},
			{Name: "notes", Repository: "acme/repo", Event: "pull_request", Action: "opened", Agent: "lead", Instructions: "Review the change and draft release notes.", Shape: "reply"},
		}}},
		Token: clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	})
	body := `{"action":"opened","installation":{"id":101},"repository":{"full_name":"acme/repo","name":"repo","owner":{"login":"acme"}},"sender":{"login":"human","type":"User"},"pull_request":{"number":7,"user":{"login":"human"},"author_association":"OWNER","head":{"sha":"` + strings.Repeat("a", 40) + `"}},"number":7}`
	mac := hmac.New(sha256.New, []byte("secret-gh"))
	mac.Write([]byte(body))
	r := httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(body))
	r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	r.Header.Set("X-GitHub-Event", "pull_request")
	r.Header.Set("X-GitHub-Delivery", "d2")
	w := httptest.NewRecorder()
	p.IngressHandler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("delivery = %d", w.Code)
	}
	for rule, want := range map[string]string{"security": wire.ShapeReview, "notes": wire.ShapeReply} {
		run := await(t, p, runtime.RunID(tenant, "automation/gh/d2/"+rule), runs.Completed)
		if got := wire.ShapeOf(run.Tag()); got != want {
			t.Errorf("rule %s: shape %q, want %q", rule, got, want)
		}
	}
}

// A schedule that sends its answer to Slack posts through the Slack connection
// it names, even when another one serves the same workspace, and only there.
func TestAScheduleDeliversThroughTheSlackConnectionItNames(t *testing.T) {
	f := &fakes{}
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"lead": {say("Nightly: all green.")}}}, f.config())
	create(t, p, tenant, slackClient("slack-a", "A0SLACKA", "T0TEAM1", "lead"))
	create(t, p, tenant, slackClient("slack-b", "A0SLACKB", "T0TEAM1", "lead"))
	run, err := p.Invoke(t.Context(), wire.NewEvent(wire.Raise{ID: "manual-nightly", Tenant: tenant, Agent: "lead", Origin: runs.OriginManual, Text: "Report.", Actor: event.Actor{ID: "ada"}, Context: map[string]string{
		"cron_schedule": "nightly", "cron_expression": "0 9 * * *", "created_by": "ada",
		platform.MetaDeliver: platform.DeliverSlack, platform.MetaDeliverClient: "slack-a", platform.MetaDeliverTo: "C0ALERTS1",
	}}))
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, run.ID, runs.Completed)
	eventually(t, "the schedule posted in its channel through slack-a", func() bool {
		for _, m := range f.slackFor("token-slack-a").Messages() {
			if m.Channel == "C0ALERTS1" && strings.Contains(m.Text, "Nightly: all green.") {
				return true
			}
		}
		return false
	})
	if n := len(f.slackFor("token-slack-b").Messages()); n != 0 {
		t.Fatalf("slack-b posted %d messages; only the named connection delivers", n)
	}
}

// A schedule names a place its connection serves, or it is refused when written.
func TestAScheduleDeliveryTargetIsChecked(t *testing.T) {
	f := &fakes{}
	p := stage(t, &cast{}, f.config())
	create(t, p, tenant, slackClient("slack-a", "A0SLACKA", "T0TEAM1", "lead"))
	for _, c := range []struct{ deliver, client, to string }{
		{platform.DeliverSlack, "slack-a", "D0DIRECT1"},
		{platform.DeliverSlack, "missing", "C0ALERTS1"},
		{platform.DeliverGitHub, "slack-a", "acme/repo#7"},
	} {
		if _, _, err := p.DeliveryTarget(tenant, c.deliver, c.client, c.to); !errors.Is(err, platform.ErrDeliveryTarget) {
			t.Errorf("%+v: err = %v, want a refused target", c, err)
		}
	}
	if _, to, err := p.DeliveryTarget(tenant, platform.DeliverSlack, "slack-a", "C0ALERTS1"); err != nil || to != "slack://T0TEAM1/C0ALERTS1" {
		t.Fatalf("to = %q, err = %v", to, err)
	}
}
