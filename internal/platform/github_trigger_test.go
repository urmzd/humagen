package platform_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/signature"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/reposettings"
	"github.com/urmzd/humagen/pkg/runs"
)

// triggerRoom is a workspace whose GitHub client answers with docs by
// default and may also reach ops. The app's own login is mapped to ops, as
// production maps it to its reviewer, which no longer makes "@humagen" ops's.
// comment posts a signed issue comment on issue number from login.
func triggerRoom(t *testing.T, mutate func(*platform.ClientInput)) (p *platform.Platform, comment func(delivery string, number int, login, body string)) {
	t.Helper()
	f := &fakes{}
	answers := map[string][][]saigetypes.Delta{}
	for _, name := range []string{"docs", "ops"} {
		for range 6 {
			answers[name] = append(answers[name], say(name+" here."))
		}
	}
	p = stage(t, &cast{says: answers}, f.config())
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}})
	in := platform.ClientInput{
		Client: clients.Client{
			ID: "gh", Name: "gh", Surface: clients.GitHub, Enabled: true,
			Identity: clients.Identity{Owners: []string{"acme"}, Installation: "101"}, DefaultAgent: "docs", Agents: []string{"ops"},
			Bots:   map[string]string{"humagen[bot]": "ops"},
			GitHub: &clients.GitHubOptions{Login: "humagen[bot]"},
		},
		Token: clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	}
	if mutate != nil {
		mutate(&in)
	}
	create(t, p, tenant, in)
	ingress := p.IngressHandler()
	comment = func(delivery string, number int, login, text string) {
		t.Helper()
		quoted, _ := json.Marshal(text)
		body := `{"action":"created","installation":{"id":101},"repository":{"full_name":"acme/site","name":"site","owner":{"login":"acme"}},` +
			`"sender":{"login":"` + login + `","type":"User"},"issue":{"number":` + strconv.Itoa(number) + `},"comment":{"id":` + strconv.Itoa(1000+number) + `,"body":` + string(quoted) + `,"user":{"login":"` + login + `"},"author_association":"COLLABORATOR"}}`
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
	return p, comment
}

// answeredBy waits for a delivery's run to finish and returns it.
func answeredBy(t *testing.T, p *platform.Platform, delivery string) runs.Run {
	t.Helper()
	return await(t, p, runtime.RunID(tenant, delivery), runs.Completed)
}

// startedNothing reports whether a delivery started no run. It is read after
// a later delivery's run has finished, so anything it started would show.
func startedNothing(p *platform.Platform, delivery string) bool {
	_, err := p.Runs.Get(tenant, runtime.RunID(tenant, delivery))
	return err != nil
}

// "@humagen" asks; the agent named right after it answers, else the client's
// default, never the agent the app's login is mapped to. An agent's name
// alone asks nobody, nor does the trigger in code or a quoted reply.
func TestOnlyTheTriggerAsksOnGitHub(t *testing.T) {
	p, comment := triggerRoom(t, nil)

	comment("bare", 1, "ada", "@ops can you deploy it?")
	comment("in-code", 2, "ada", "run `@humagen help` to see the commands")
	comment("in-quote", 3, "ada", "> @humagen what changed?\n\nI wonder too")
	comment("in-template", 15, "ada", "<!-- @humagen review -->\nMy change")

	comment("named", 4, "ada", "@Humagen ops: deploy it")
	if run := answeredBy(t, p, "named"); run.Agent != "ops" || run.Text != "deploy it" {
		t.Fatalf("the trigger naming ops was answered by %s with %q", run.Agent, run.Text)
	}
	comment("ask", 5, "ada", "@humagen ask ops: roll it back")
	if run := answeredBy(t, p, "ask"); run.Agent != "ops" || run.Text != "roll it back" {
		t.Fatalf("asking ops was answered by %s with %q", run.Agent, run.Text)
	}
	comment("plain", 6, "ada", "@humagen what changed?")
	if run := answeredBy(t, p, "plain"); run.Agent != "docs" || run.Text != "what changed?" {
		t.Fatalf("a plain question was answered by %s with %q, want the client's default", run.Agent, run.Text)
	}
	comment("bare-word", 8, "ada", "@humagen ops deploy it")
	if run := answeredBy(t, p, "bare-word"); run.Agent != "docs" || run.Text != "ops deploy it" {
		t.Fatalf("a bare first word was answered by %s with %q, want the default with the whole text", run.Agent, run.Text)
	}
	comment("mid", 7, "ada", "Thanks! Could @humagen summarize the thread?")
	if run := answeredBy(t, p, "mid"); run.Agent != "docs" || run.Text != "Thanks! Could @humagen summarize the thread?" {
		t.Fatalf("a trigger inside a sentence was answered by %s with %q", run.Agent, run.Text)
	}
	for _, d := range []string{"bare", "in-code", "in-quote", "in-template"} {
		if !startedNothing(p, d) {
			t.Errorf("%s started a run", d)
		}
	}
}

// On a pull request or issue an agent already answered, a plain question to
// the trigger asks the default, not that agent; a reply without the trigger
// continues with it, and an agent's name in that reply does not take it over.
func TestTheTriggerAsksTheDefaultAfterANamedAgentAnswered(t *testing.T) {
	p, comment := triggerRoom(t, nil)
	comment("first", 9, "ada", "@humagen ask ops to look at the deploy")
	if run := answeredBy(t, p, "first"); run.Agent != "ops" {
		t.Fatalf("first answered by %s", run.Agent)
	}
	comment("again", 9, "ada", "@humagen and what about the docs?")
	if run := answeredBy(t, p, "again"); run.Agent != "docs" {
		t.Fatalf("the plain question after ops answered went to %s, want the default", run.Agent)
	}
	comment("asked", 10, "ada", "@humagen what changed?")
	if run := answeredBy(t, p, "asked"); run.Agent != "docs" {
		t.Fatalf("asked answered by %s", run.Agent)
	}
	// A reply continues an answer a person can see: one posted on the
	// issue, which is on record once it is delivered, a moment after its
	// run completes.
	eventually(t, "the answer on issue 10 was delivered", func() bool {
		return p.SurfaceAnswered(tenant, "gh", address.MustParse("github://acme/site/issues/10"))
	})
	comment("reply", 10, "ada", "@ops thanks, one more thing")
	if run := answeredBy(t, p, "reply"); run.Agent != "docs" || run.Text != "@ops thanks, one more thing" {
		t.Fatalf("the reply was answered by %q with %q, want docs continuing", run.Agent, run.Text)
	}
}

// The repository's agent wins over the client's default.
func TestTheTriggerFollowsTheRepositorysAgent(t *testing.T) {
	p, comment := triggerRoom(t, func(in *platform.ClientInput) { in.GitHub.ReadRepositories = []string{"acme/site"} })
	if _, err := p.SetGitHubRepository(t.Context(), admin, "gh", "acme/site", reposettings.Settings{Agent: "ops"}, 0); err != nil {
		t.Fatal(err)
	}
	comment("repo", 11, "ada", "@humagen what changed?")
	if run := answeredBy(t, p, "repo"); run.Agent != "ops" {
		t.Fatalf("answered by %s, want the repository's agent", run.Agent)
	}
}

// With no default anywhere, the trigger lands on the workspace's entry agent.
func TestTheTriggerLandsOnTheEntryAgent(t *testing.T) {
	f := &fakes{}
	cfg := f.config()
	p := withEntry(t, cfg)
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{
			ID: "gh", Name: "gh", Surface: clients.GitHub, Enabled: true, Agents: []string{clients.AnyAgent},
			Identity: clients.Identity{Owners: []string{"acme"}, Installation: "101"},
			GitHub:   &clients.GitHubOptions{Login: "humagen[bot]"},
		},
		Token: clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	})
	body := `{"action":"created","installation":{"id":101},"repository":{"full_name":"acme/site","name":"site","owner":{"login":"acme"}},` +
		`"sender":{"login":"ada","type":"User"},"issue":{"number":12},"comment":{"id":12,"body":"@humagen what changed?","user":{"login":"ada"},"author_association":"COLLABORATOR"}}`
	mac := hmac.New(sha256.New, []byte("secret-gh"))
	mac.Write([]byte(body))
	r := httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(body))
	r.Header.Set(signature.HeaderGitHubSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	r.Header.Set("X-GitHub-Event", "issue_comment")
	r.Header.Set("X-GitHub-Delivery", "entry")
	w := httptest.NewRecorder()
	p.IngressHandler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("answered %d", w.Code)
	}
	var run runs.Run
	eventually(t, "the trigger started a run", func() bool {
		got, err := p.Runs.Get(tenant, runtime.RunID(tenant, "entry"))
		run = got
		return err == nil
	})
	if run.Agent != platform.BuiltinEntry {
		t.Fatalf("answered by %s, want the entry agent %s", run.Agent, platform.BuiltinEntry)
	}
}

// Asking stays with the people who may: a stranger's trigger starts nothing,
// whichever agent it names.
func TestAStrangersTriggerStartsNothing(t *testing.T) {
	p, comment := triggerRoom(t, func(in *platform.ClientInput) { in.GitHub.Askers = []string{"ada"} })
	comment("stranger", 13, "mallory", "@humagen ask ops to deploy it")
	comment("asker", 14, "ada", "@humagen ask ops to deploy it")
	answeredBy(t, p, "asker")
	time.Sleep(50 * time.Millisecond)
	if !startedNothing(p, "stranger") {
		t.Fatal("a stranger's trigger started a run")
	}
}

// A review requested from the humagen account, the one people add as a
// collaborator, is the repository's agent's, as "@humagen review" is.
func TestAReviewRequestedFromTheTriggerAccountIsTheRepositorysAgents(t *testing.T) {
	// No bot login is mapped: production drops the app's own.
	p, _ := triggerRoom(t, func(in *platform.ClientInput) {
		in.GitHub.ReadRepositories, in.Bots = []string{"acme/site"}, nil
	})
	if _, err := p.SetGitHubRepository(t.Context(), admin, "gh", "acme/site", reposettings.Settings{Agent: "ops"}, 0); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	body := `{"action":"review_requested","installation":{"id":101},"repository":{"full_name":"acme/site","name":"site","owner":{"login":"acme"}},` +
		`"sender":{"login":"ada","type":"User"},"requested_reviewer":{"login":"humagen","type":"User"},` +
		`"pull_request":{"number":21,"title":"Speed up search","body":"Please review.","state":"open","draft":false,"user":{"login":"ada"},"head":{"ref":"fast","sha":"` + sha + `"},"base":{"ref":"main"},"author_association":"COLLABORATOR"}}`
	mac := hmac.New(sha256.New, []byte("secret-gh"))
	mac.Write([]byte(body))
	r := httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(body))
	r.Header.Set(signature.HeaderGitHubSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	r.Header.Set("X-GitHub-Event", "pull_request")
	r.Header.Set("X-GitHub-Delivery", "review-from-humagen")
	w := httptest.NewRecorder()
	p.IngressHandler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("answered %d", w.Code)
	}
	if run := answeredBy(t, p, "review-from-humagen"); run.Agent != "ops" {
		t.Fatalf("the review request was answered by %s, want the repository's agent", run.Agent)
	}
}
