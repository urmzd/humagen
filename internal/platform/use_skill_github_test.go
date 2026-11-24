package platform_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/signature"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/runs"
)

// A skill loaded on a pull request reads that pull request's repository as
// its own place, as its caller does: no grant to read from another surface
// is needed, and none is given. A repository nothing grants stays refused,
// and the client is asked for the caller it routes, not for skill-runner.
func TestALoadedSkillReadsItsCallersRepositoryAsItsOwn(t *testing.T) {
	f := &fakes{}
	c := &cast{says: map[string][][]saigetypes.Delta{
		"reviewer": {loadingSkill("load-gh", "repo-check"), say("Reviewed.")},
		platform.BuiltinSkillRunner: {
			agenttest.ToolCallResponse("own", "github_read_context", map[string]any{"address": "github://acme/repo/pull/7"}),
			agenttest.ToolCallResponse("other", "github_read_context", map[string]any{"address": "github://acme/secret/issues/1"}),
			say(skillFound),
		},
	}}
	fake := f.githubFor("token-gh")
	cfg := f.config()
	cfg.GitHubAPI = func(token string) github.API {
		if token == "token-gh" {
			return fake
		}
		return f.githubFor(token)
	}
	cfg.Limits = call.Limits{MaxDepth: 2, MaxFanOut: 4}
	p := loading(t, c, map[string][]string{"reviewer": {wire.AnySkill}}, cfg)
	publishSkill(t, p, admin.Subject, library.Scope{Kind: "workspace"}, "repo-check", "READ THE PULL REQUEST", nil)
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{
			ID: "gh", Name: "gh", Surface: clients.GitHub, Enabled: true,
			Identity:     clients.Identity{Owners: []string{"acme"}, Installation: "101"},
			DefaultAgent: "reviewer", Bots: map[string]string{"reviewer": "reviewer"},
			GitHub: &clients.GitHubOptions{Login: "humagen-token"},
		},
		Token: clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	})
	pull := github.Context{Subject: github.ContextSubject{Number: 7, Title: "PULL-REQUEST-TITLE", State: "open", URL: "https://github.com/acme/repo/pull/7"}}
	pull.Subject.PullRequest = &struct {
		URL string `json:"url"`
	}{URL: "https://api.github.com/repos/acme/repo/pulls/7"}
	pull.Subject.Head.Ref, pull.Subject.Head.SHA = "change", strings.Repeat("a", 40)
	secret := github.Context{Subject: github.ContextSubject{Number: 1, Title: "SECRET-REPOSITORY-CANARY", State: "open"}}
	for ref, read := range map[github.Ref]github.Context{{Owner: "acme", Repo: "repo", Number: 7}: pull, {Owner: "acme", Repo: "secret", Number: 1}: secret} {
		if err := fake.SeedContext(ref, read); err != nil {
			t.Fatal(err)
		}
	}
	body := `{"action":"created","installation":{"id":101},"repository":{"name":"repo","owner":{"login":"acme"}},` +
		`"sender":{"login":"ana","type":"User"},"issue":{"number":7,"pull_request":{"url":"https://api.github.com/repos/acme/repo/pulls/7"}},` +
		`"comment":{"id":907,"body":"@humagen Check this change.","user":{"login":"ana"},"author_association":"MEMBER"}}`
	mac := hmac.New(sha256.New, []byte("secret-gh"))
	mac.Write([]byte(body))
	r := httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(body))
	r.Header.Set(signature.HeaderGitHubSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	r.Header.Set("X-GitHub-Event", "issue_comment")
	r.Header.Set("X-GitHub-Delivery", "use-skill-1")
	w := httptest.NewRecorder()
	p.IngressHandler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("the delivery answered %d", w.Code)
	}
	finalComment(t, fake, 7, "Reviewed.")
	reviewer, ok := runOf(p, tenant, "reviewer")
	if !ok {
		t.Fatal("the comment started no run of reviewer")
	}
	called(t, p, reviewer.ID, platform.BuiltinSkillRunner, runs.Completed)
	sent := c.requests(platform.BuiltinSkillRunner)
	last := sent[len(sent)-1]
	if !strings.Contains(last, "PULL-REQUEST-TITLE") {
		t.Fatalf("skill-runner could not read its caller's pull request: %s", last)
	}
	if strings.Contains(last, "SECRET-REPOSITORY-CANARY") {
		t.Fatalf("skill-runner read a repository nothing grants: %s", last)
	}
	if result := strings.Join(c.requests("reviewer")[1:], "\n"); !strings.Contains(result, "skill-runner#repo-check") {
		t.Fatalf("the reviewer was answered %s", result)
	}
}
