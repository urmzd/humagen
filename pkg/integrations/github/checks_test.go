package github_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/orchagent/flow/address"
	"github.com/urmzd/orchagent/flow/connectors/mention"
	"github.com/urmzd/orchagent/flow/event"
)

var commitSHA = strings.Repeat("a", 40)

func TestChecksStreamAndKeepTheirPinnedCommit(t *testing.T) {
	fake := github.NewFake()
	c, err := github.New(github.Config{API: fake, Checks: true, Interval: -1})
	if err != nil {
		t.Fatal(err)
	}
	target := address.MustParse("github://acme/repo/pull/42").WithParam("head_sha", commitSHA)
	events := []event.Envelope{runEvent(1, event.TypeAccepted, `{}`), runEvent(2, event.TypeStarted, `{}`), runEvent(3, event.TypeToolCall, `{"id":"t1","name":"github_read_context"}`), runEvent(4, event.TypeCompleted, `{"text":"Review complete. Tests not run."}`)}
	for _, e := range events {
		for range 2 {
			if err := c.Deliver(context.Background(), target, e); err != nil {
				t.Fatal(err)
			}
		}
	}
	checks := fake.Checks()
	if len(checks) != 1 || checks[0].Check.HeadSHA != commitSHA || checks[0].Check.Status != "completed" || checks[0].Check.Conclusion != "neutral" || checks[0].Edits != 3 {
		t.Fatalf("checks %+v", checks)
	}
	if len(fake.Comments()) != 1 {
		t.Fatal("duplicate comments")
	}
	// A restarted sink finds the check rather than creating another check run.
	recovered, _ := github.New(github.Config{API: fake, Checks: true, Interval: -1})
	if err := recovered.Deliver(context.Background(), target, events[3]); err != nil {
		t.Fatal(err)
	}
	if len(fake.Checks()) != 1 {
		t.Fatal("duplicate check after recovery")
	}
}

func TestCheckConclusionReflectsReviewErrorsAndMalformedReports(t *testing.T) {
	for _, tc := range []struct{ text, conclusion string }{
		{`{"format":"humagen.github.review.v1","summary":"Found a bug","findings":[{"severity":"error","title":"Bug","suggestion":"Fix it","evidence":[{"observation":"Failure","source":"test.go:1"}],"prompt":"Add a regression test"}]}`, "failure"},
		{`{"format":"humagen.github.review.v1","summary":"Missing evidence","findings":[{"severity":"error"}]}`, "action_required"},
		{"```json\n" + `{"format":"humagen.github.review.v1","summary":"Found a bug","findings":[{"severity":"error","title":"Bug","suggestion":"Fix it","evidence":[{"observation":"Failure","source":"test.go:1"}],"prompt":"Add a regression test"}]}` + "\n```", "failure"},
	} {
		fake := github.NewFake()
		c, _ := github.New(github.Config{API: fake, Checks: true})
		p, _ := json.Marshal(map[string]string{"text": tc.text})
		if err := c.Deliver(context.Background(), address.MustParse("github://acme/repo/pull/1").WithParam("head_sha", commitSHA), runEvent(1, event.TypeCompleted, string(p))); err != nil {
			t.Fatal(err)
		}
		if got := fake.Checks()[0].Check.Conclusion; got != tc.conclusion {
			t.Fatalf("conclusion %s", got)
		}
	}
}

func TestCheckRecoveryUsesExternalIDAndNeverPatchesTheHead(t *testing.T) {
	posts, patches := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			fmt.Fprintf(w, `{"check_runs":[{"id":17,"external_id":"run-1","name":"fiber/reviewer","head_sha":%q}]}`, commitSHA)
		case "PATCH":
			patches++
			var patch map[string]any
			_ = json.NewDecoder(r.Body).Decode(&patch)
			if _, ok := patch["head_sha"]; ok {
				t.Error("patch moved check head")
			}
			fmt.Fprint(w, `{}`)
		case "POST":
			posts++
			fmt.Fprint(w, `{"id":18}`)
		}
	}))
	defer server.Close()
	api := github.NewHTTPAPIAt(server.URL, "token", server.Client())
	id, err := api.EnsureCheck(context.Background(), github.Ref{Owner: "acme", Repo: "repo", Number: 1}, github.Check{Name: "fiber/reviewer", ExternalID: "run-1", HeadSHA: commitSHA, Status: "in_progress"})
	if err != nil || id != 17 || posts != 0 || patches != 1 {
		t.Fatalf("id=%d posts=%d patches=%d err=%v", id, posts, patches, err)
	}
}

// Labels start nothing, and neither does a push or a pull request opening
// without a mention: automatic review is the platform's, from the GitHub
// connection's switch, not the connector's.
func TestLabelsAndPushesStartNothingAtTheConnector(t *testing.T) {
	for _, action := range []string{"labeled", "synchronize", "opened", "ready_for_review"} {
		t.Run(action, func(t *testing.T) {
			c, tags := start(t, github.Config{Owners: []string{"acme"}})
			raw, _ := json.Marshal(map[string]any{"action": action, "repository": map[string]any{"name": "repo", "owner": map[string]string{"login": "acme"}}, "sender": map[string]string{"type": "User"}, "label": map[string]string{"name": "agent:auto"}, "pull_request": map[string]any{"number": 42, "title": "Change", "body": "No mention", "state": "open", "labels": []map[string]string{{"name": "agent:auto"}}, "head": map[string]string{"sha": commitSHA}}})
			w := httptest.NewRecorder()
			c.ServeHTTP(w, deliver(t, "pull_request", "delivery", string(raw)))
			if w.Code != 200 {
				t.Fatalf("status %d", w.Code)
			}
			select {
			case tag := <-tags:
				t.Fatalf("%s started a run: %+v", action, tag)
			default:
			}
		})
	}
}

func TestPRCommentPinsTheHeadBeforeAcceptance(t *testing.T) {
	fake := github.NewFake()
	evidence := github.Context{Subject: github.ContextSubject{Number: 42}}
	evidence.Subject.Head.SHA = commitSHA
	if err := fake.SeedContext(github.Ref{Owner: "urmzd", Repo: "fiber", Number: 42}, evidence); err != nil {
		t.Fatal(err)
	}
	c, tags := start(t, github.Config{API: fake, Checks: true, Agents: mention.NewNames("docs-bot")})
	raw := strings.Replace(issueComment, `"number": 42,`, `"number": 42, "pull_request":{"url":"https://api.github.com/repos/urmzd/fiber/pulls/42"},`, 1)
	w := httptest.NewRecorder()
	c.ServeHTTP(w, deliver(t, "issue_comment", "pin-comment", raw))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	select {
	case tag := <-tags:
		if wire.SurfaceAddress(tag).Params["head_sha"] != commitSHA {
			t.Fatalf("unpinned tag %+v", tag)
		}
	default:
		t.Fatal("missing tag")
	}
}

func TestPinnedReviewCannotReadEvidenceFromANewerCommit(t *testing.T) {
	fake := github.NewFake()
	evidence := github.Context{Subject: github.ContextSubject{Number: 42}}
	evidence.Subject.Head.SHA = strings.Repeat("b", 40)
	if err := fake.SeedContext(github.Ref{Owner: "acme", Repo: "repo", Number: 42}, evidence); err != nil {
		t.Fatal(err)
	}
	c, _ := github.New(github.Config{API: fake})
	target := address.MustParse("github://acme/repo/pull/42").WithParam("head_sha", commitSHA)
	_, err := c.Actions()[0].Invoke(context.Background(), target, json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "PR head changed") {
		t.Fatalf("expected stale evidence error, got %v", err)
	}
}

func TestIssuesDoNotCreateCommitChecks(t *testing.T) {
	fake := github.NewFake()
	c, _ := github.New(github.Config{API: fake, Checks: true})
	if err := c.Deliver(context.Background(), address.MustParse("github://acme/repo/issues/1"), runEvent(1, event.TypeStarted, `{}`)); err != nil {
		t.Fatal(err)
	}
	if len(fake.Checks()) != 0 || len(fake.Comments()) != 1 {
		t.Fatal("incorrect issue delivery")
	}
}
