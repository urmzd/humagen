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

// Every GitHub delivery says in one log line whether it became a request,
// and when it did not, why: a comment that mentions the app is accepted, one
// that mentions nobody is ignored for that, and one signed with the wrong
// secret is refused before any client reads it.
func TestEveryGitHubDeliverySaysWhatBecameOfIt(t *testing.T) {
	said := &logs{}
	cfg := (&fakes{}).config()
	cfg.Log = slog.New(slog.NewTextHandler(said, nil))
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"reviewer": {say("Looked.")}}}, cfg)
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{ID: "gh", Name: "GitHub", Surface: clients.GitHub, Enabled: true, Identity: clients.Identity{Owners: []string{"acme"}, Installation: "101"}, DefaultAgent: "reviewer", GitHub: &clients.GitHubOptions{Login: "humagen[bot]"}},
		Token:  clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	})
	ingress := p.IngressHandler()
	post := func(secret, delivery, comment string) int {
		body := `{"action":"created","installation":{"id":101},"repository":{"full_name":"acme/repo","name":"repo","owner":{"login":"acme"}},` +
			`"sender":{"login":"ada","type":"User"},"issue":{"number":7},"comment":{"id":91,"body":"` + comment + `","user":{"login":"ada"},"author_association":"COLLABORATOR"}}`
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(body))
		r := httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(body))
		r.Header.Set(signature.HeaderGitHubSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
		r.Header.Set("X-GitHub-Event", "issue_comment")
		r.Header.Set("X-GitHub-Delivery", delivery)
		w := httptest.NewRecorder()
		ingress.ServeHTTP(w, r)
		return w.Code
	}
	if code := post("secret-gh", "d-mention", "@humagen review this"); code != http.StatusOK {
		t.Fatalf("the mention answered %d", code)
	}
	if code := post("secret-gh", "d-plain", "looks good to me"); code != http.StatusOK {
		t.Fatalf("the plain comment answered %d", code)
	}
	if code := post("wrong-secret", "d-forged", "@humagen review this"); code != http.StatusUnauthorized {
		t.Fatalf("the forged delivery answered %d", code)
	}
	pull := `{"action":"opened","number":8,"installation":{"id":101},"repository":{"full_name":"acme/repo","name":"repo","owner":{"login":"acme"}},` +
		`"sender":{"login":"ada","type":"User"},"pull_request":{"number":8,"state":"open","draft":false,"user":{"login":"ada"},"author_association":"MEMBER","head":{"sha":"` + strings.Repeat("c", 40) + `"}}}`
	mac := hmac.New(sha256.New, []byte("secret-gh"))
	mac.Write([]byte(pull))
	r := httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(pull))
	r.Header.Set(signature.HeaderGitHubSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	r.Header.Set("X-GitHub-Event", "pull_request")
	r.Header.Set("X-GitHub-Delivery", "d-pull")
	ingress.ServeHTTP(httptest.NewRecorder(), r)
	lines := said.String()
	for _, want := range []string{
		`msg="platform: a webhook delivery was accepted" surface=github tenant=` + tenant + ` client=gh event=issue_comment action=created place=acme/repo delivery=d-mention status=200 requests=1`,
		`delivery=d-plain status=200 why="it mentions neither the app (@humagen) nor one of the client's agents"`,
		`delivery=d-forged status=401 why="its signature matches the secret of none of the 1 clients that claim it"`,
		`event=pull_request action=opened place=acme/repo delivery=d-pull status=200 requests=1`,
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("the logs do not say %q:\n%s", want, lines)
		}
	}
}
