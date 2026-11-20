package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/httpapi"
	"github.com/urmzd/humagen/internal/platform"
	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/saige/agent/agenttest"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/turn/native"
)

const reviewHead = "316c56f81540c3eaca02184173df322a48aebdd9"

func TestFixtureGitHubContextIsLocalAndPinned(t *testing.T) {
	var cfg platform.Config
	if err := fixtureConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	api, ok := cfg.GitHubAPI(fixtureToken).(*github.Fake)
	if !ok || cfg.GitHubAPI("unrelated-token") != api {
		t.Fatal("fixture credentials selected a live GitHub API")
	}
	ref := github.Ref{Owner: "screens", Repo: "launch", Number: 1}
	got, err := api.ReadContext(t.Context(), ref, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject.Head.SHA != reviewHead || got.Subject.Number != 1 || len(got.Files) == 0 {
		t.Fatalf("fixture lost its pinned pull request: %+v", got)
	}
	if got.Files[0].Filename != "plan.md" || got.Files[0].Patch == "" {
		t.Fatalf("fixture lost its reviewed diff: %+v", got.Files)
	}
	got.Subject.Head.SHA = "changed"
	got.Files[0].Patch = "changed"
	again, err := api.ReadContext(t.Context(), ref, 1)
	if err != nil || again.Subject.Head.SHA != reviewHead || again.Files[0].Patch == "changed" {
		t.Fatalf("reading the fixture mutated its snapshot: %+v %v", again, err)
	}
	if _, err := api.ReadContext(t.Context(), github.Ref{Owner: "screens", Repo: "unknown", Number: 1}, 1); err == nil {
		t.Fatal("unknown repository acquired fixture evidence")
	}
}

func TestFixtureCredentialsPreserveExistingConfiguration(t *testing.T) {
	original := map[string][]string{"default": {"EXISTING"}, "another": {"OTHER"}}
	cfg := platform.Config{SecretRefs: original, ClientInterval: 17 * time.Millisecond, Env: func(name string) (string, bool) {
		if name == "EXISTING" {
			return "retained", true
		}
		return "", false
	}}
	if err := fixtureConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"default": {"EXISTING", fixtureTokenName, fixtureSecretName}, "another": {"OTHER"}}
	if !reflect.DeepEqual(cfg.SecretRefs, want) || len(original["default"]) != 1 {
		t.Fatalf("fixture changed unrelated credential grants: %+v", cfg.SecretRefs)
	}
	for name, want := range map[string]string{fixtureTokenName: fixtureToken, fixtureSecretName: fixtureSecret, "EXISTING": "retained"} {
		if got, ok := cfg.Env(name); !ok || got != want {
			t.Fatalf("environment lookup %s = %q, %v", name, got, ok)
		}
	}
	if _, ok := cfg.Env("UNLISTED"); ok || cfg.ClientInterval != 17*time.Millisecond {
		t.Fatal("fixture changed unrelated configuration")
	}
	var empty platform.Config
	if err := fixtureConfig(&empty); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(empty.SecretRefs, map[string][]string{"default": {fixtureTokenName, fixtureSecretName}}) {
		t.Fatalf("fixture grants more than its two dummy references: %+v", empty.SecretRefs)
	}
	if _, ok := empty.Env("HUMAGEN_GITHUB_TOKEN"); ok {
		t.Fatal("fixture read a shell credential")
	}
}

// This smoke test uses the same dev-auth HTTP routes as screens/run.ts. The
// model calls the connector, and the host validates its actual tool receipt.
func TestReviewFixtureSurvivesHTTPProjectionWithRecordedContext(t *testing.T) {
	const toolID = "screens-review-context"
	report := map[string]any{
		"format": "humagen.review.v2", "title": "Launch readiness review", "verdict": "request_changes",
		"scope": map[string]string{"commit": reviewHead}, "summary": "Confirm the briefing owner.",
		"findings": []any{map[string]any{
			"id": "briefing-owner", "severity": "warning", "title": "Confirm the owner",
			"suggestion": "Name the owner before publication.", "prompt": "Confirm the owner.",
			"evidence": []any{
				map[string]string{"observation": "The reviewed diff changes plan.md.", "source": "plan.md", "step": toolID},
				map[string]string{"observation": "Invented reference must not become proof.", "source": "plan.md", "step": "invented-step"},
			},
		}},
	}
	rawReport, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	cfg := platform.Config{Workspace: t.TempDir(), AgentOptions: []native.Option{native.WithScript(
		agenttest.ToolCallResponse(toolID, "github_read_context", map[string]any{"address": "https://github.com/screens/launch/pull/1"}),
		agenttest.TextResponse(string(rawReport)),
	)}}
	if err := fixtureConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	p, err := platform.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	who := auth.Principal{Tenant: "default", Subject: "dev", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	srv := httptest.NewServer((&httpapi.Server{P: p, Authn: &httpapi.Authn{Dev: &who}, Log: slog.New(slog.DiscardHandler)}).Handler())
	t.Cleanup(func() {
		srv.Close()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("platform shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("platform did not stop within five seconds")
		}
		if err := p.Close(); err != nil {
			t.Errorf("platform close: %v", err)
		}
	})
	request := func(method, path string, body, out any, status int) {
		t.Helper()
		var data []byte
		if body != nil {
			var err error
			data, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, srv.URL+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		response, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != status {
			t.Fatalf("%s %s: %d, %s", method, path, res.StatusCode, response)
		}
		if out != nil {
			if err := json.Unmarshal(response, out); err != nil {
				t.Fatalf("%s %s response: %v", method, path, err)
			}
		}
	}
	request("POST", "/api/agents", wire.AgentSpec{Name: "screens-reviewer", Provider: "offline", Model: "offline", SystemPrompt: "Review the fixture using the context tool.", Tools: []string{"github_read_context"}}, nil, http.StatusCreated)
	var client clients.Client
	request("POST", "/api/clients", map[string]any{
		"id": "screens-github", "name": "Screen fixture GitHub", "surface": "github", "enabled": true,
		"identity": map[string]any{"owners": []string{"screens"}}, "agents": []string{"screens-reviewer"},
		"github":      map[string]any{"read_repositories": []string{"screens/launch"}},
		"credentials": map[string]any{"token": map[string]string{"ref": "env:" + fixtureTokenName}, "signing_secret": map[string]string{"ref": "env:" + fixtureSecretName}},
	}, &client, http.StatusCreated)
	request("PUT", "/api/clients/screens-github/capabilities", map[string]any{
		"version": client.Version, "events": []string{}, "actions": []string{"github_read_context"}, "deliver_outcomes": false,
	}, nil, http.StatusOK)
	var session platform.SessionView
	request("POST", "/api/sessions", platform.SessionInput{Agent: "screens-reviewer", Text: "Review the canonical session fixture."}, &session, http.StatusCreated)
	var runID, answer string
	for {
		if session.Status == "failed" || session.Status == "stopped" {
			t.Fatalf("fixture session %s: %+v", session.Status, session.Messages)
		}
		// The summary can observe completion after the message snapshot was
		// read. Wait for this review's terminal projection before checking it.
		runID, answer = "", ""
		for _, message := range session.Messages {
			if message.Run != nil && message.Run.Agent == "screens-reviewer" && message.Status == "done" {
				runID, answer = message.Run.ID, message.Run.Answer
			}
		}
		if session.Status == "done" && runID != "" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("fixture session did not finish within twenty seconds")
		case <-time.After(10 * time.Millisecond):
		}
		request("GET", "/api/sessions/"+session.ID, nil, &session, http.StatusOK)
	}
	var shown presentation.ReviewReport
	if err := json.Unmarshal([]byte(answer), &shown); err != nil || shown.Format != "humagen.review.v2" || shown.Scope == nil || shown.Scope.Commit != reviewHead {
		t.Fatalf("v2 review did not survive the HTTP projection: %q (%v)", answer, err)
	}
	if len(shown.Findings) != 1 || len(shown.Findings[0].Evidence) != 2 || shown.Findings[0].Evidence[0].Step != toolID || shown.Findings[0].Evidence[1].Step != "" {
		t.Fatalf("HTTP projection did not retain only recorded evidence: %+v", shown.Findings)
	}
	run, err := p.Runs.Get(who.Tenant, runID)
	if err != nil || wire.SurfaceOf(run.Tag()).GitHubHeadSHA != "" {
		t.Fatalf("console fixture injected a native trusted head: %v", err)
	}
	events, err := p.Journal.Events(who.Tenant, []string{runID})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, envelope := range events {
		if result, ok := envelope.Data.(event.ToolDone); ok && result.ID == toolID && result.Name == "github_read_context" {
			if result.Error != "" || !strings.Contains(result.Result, reviewHead) || !strings.Contains(result.Result, "plan.md") {
				t.Fatalf("context tool did not record fixture evidence: %+v", result)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("no github_read_context receipt for %s", runID)
	}
}
