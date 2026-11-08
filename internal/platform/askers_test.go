package platform_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/orchagent/flow/signature"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// On a public repository a stranger mentioning the app starts nothing, and
// the log says why; a collaborator's mention starts a run; named askers
// narrow who may ask further.
func TestOnlyPeopleWhoMayWriteAskThroughGitHub(t *testing.T) {
	said := &logs{}
	cfg := (&fakes{}).config()
	cfg.Log = slog.New(slog.NewTextHandler(said, nil))
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"reviewer": {say("Looked."), say("Looked again.")}}}, cfg)
	client := func(askers ...string) platform.ClientInput {
		return platform.ClientInput{
			Client: clients.Client{ID: "gh", Name: "GitHub", Surface: clients.GitHub, Enabled: true, Identity: clients.Identity{Owners: []string{"acme"}, Installation: "101"}, DefaultAgent: "reviewer", GitHub: &clients.GitHubOptions{Login: "humagen[bot]", Askers: askers}},
			Token:  clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
		}
	}
	create(t, p, tenant, client())
	ingress := p.IngressHandler()
	comment := func(delivery, login, association string) {
		t.Helper()
		body := `{"action":"created","installation":{"id":101},"repository":{"full_name":"acme/repo","name":"repo","owner":{"login":"acme"}},` +
			`"sender":{"login":"` + login + `","type":"User"},"issue":{"number":7},"comment":{"id":91,"body":"@humagen review this","user":{"login":"` + login + `"},"author_association":"` + association + `"}}`
		mac := hmac.New(sha256.New, []byte("secret-gh"))
		mac.Write([]byte(body))
		r := httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(body))
		r.Header.Set(signature.HeaderGitHubSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
		r.Header.Set("X-GitHub-Event", "issue_comment")
		r.Header.Set("X-GitHub-Delivery", delivery)
		w := httptest.NewRecorder()
		ingress.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s answered %d", delivery, w.Code)
		}
	}
	reviews := func() int {
		n := 0
		for _, r := range p.Runs.List(tenant, 20) {
			if r.Agent == "reviewer" {
				n++
			}
		}
		return n
	}

	comment("d-stranger", "mallory", "NONE")
	if n := reviews(); n != 0 {
		t.Fatalf("a stranger's mention started %d runs", n)
	}
	if !strings.Contains(said.String(), `delivery=d-stranger status=200 why="mallory has no write access to the repository (none)"`) {
		t.Fatalf("the stranger's mention was not logged as ignored:\n%s", said.String())
	}
	comment("d-collaborator", "ada", "COLLABORATOR")
	eventually(t, "the collaborator's mention started a run", func() bool { return reviews() == 1 })

	update := client("octocat")
	current, err := p.Clients.Get(tenant, "gh")
	if err != nil {
		t.Fatal(err)
	}
	update.Version = current.Version
	if _, err := p.UpdateClient(t.Context(), tenant, "gh", "test", update); err != nil {
		t.Fatal(err)
	}
	comment("d-not-an-asker", "ada", "COLLABORATOR")
	if n := reviews(); n != 1 {
		t.Fatalf("a collaborator who is not a named asker started a run (%d)", n)
	}
	if !strings.Contains(said.String(), `why="ada is not one of the client's askers"`) {
		t.Fatalf("the narrowed mention was not logged:\n%s", said.String())
	}
}
