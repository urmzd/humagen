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
