package platform_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/orchagent/flow/runtime"
	saigetypes "github.com/urmzd/saige/agent/types"
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

func TestAnAutomationAnswersWhereItIsToldAndAlwaysKeepsItsSession(t *testing.T) {
	f := &fakes{}
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"lead": {say("Looked into it."), say("Looked into it."), say("Looked into it."), say("Looked into it.")}}}, f.config())
	create(t, p, tenant, slackClient("slack-app", "A0SLACK1", "T0TEAM1", "lead"))
	rule := func(name, deliver, slackClient, channel string) clients.GitHubAutomation {
		return clients.GitHubAutomation{Name: name, Repository: "acme/repo", Event: "issues", Action: "opened", Agent: "lead", Instructions: "Investigate this issue.", Deliver: deliver, SlackClient: slackClient, SlackChannel: channel}
	}
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{ID: "gh", Name: "GitHub", Surface: clients.GitHub, Enabled: true, Identity: clients.Identity{Owners: []string{"acme"}, Installation: "101"}, DefaultAgent: "lead", GitHub: &clients.GitHubOptions{Automations: []clients.GitHubAutomation{
			rule("on-the-issue", clients.DeliverGitHub, "", ""),
			rule("as-before", "", "", ""),
			rule("to-slack", clients.DeliverSlack, "slack-app", "C0ALERTS1"),
			rule("kept", clients.DeliverNone, "", ""),
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
	for _, name := range []string{"on-the-issue", "as-before", "to-slack", "kept"} {
		run := await(t, p, runtime.RunID(tenant, "automation/gh/d1/"+name), runs.Completed)
		if run.Thread == "" {
			t.Fatalf("%s kept no session", name)
		}
	}
	eventually(t, "the issue automation answered on the issue", func() bool {
		for _, c := range f.githubFor("token-gh").Comments() {
			if strings.Contains(c.Body, "Looked into it.") {
				return true
			}
		}
		return false
	})
	eventually(t, "the Slack automation posted in its channel", func() bool {
		for _, m := range f.slackFor("token-slack-app").Messages() {
			if m.Channel == "C0ALERTS1" && strings.Contains(m.Text, "Looked into it.") {
				return true
			}
		}
		return false
	})
	if n := len(f.githubFor("token-gh").Comments()); n != 1 {
		t.Fatalf("%d comments on the issue, want only the GitHub automation's; an unset delivery stays in its session", n)
	}
}
