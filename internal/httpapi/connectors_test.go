package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	githubflow "github.com/urmzd/humagen/pkg/integrations/github"
	slackflow "github.com/urmzd/humagen/pkg/integrations/slack"
	"github.com/urmzd/saige/flow/signature"
)

func TestSignedIntegrationsRunThroughThePlatformAndAppearInItsConsoleAPI(t *testing.T) {
	for _, surface := range []string{"github", "slack"} {
		t.Run(surface, func(t *testing.T) {
			const secret = "humagen-integration-secret"
			cfg := platform.Config{Workspace: t.TempDir()}
			var peer platform.Peer
			var replies func() int
			var body, path string
			if surface == "github" {
				api := githubflow.NewFake()
				c, err := githubflow.New(githubflow.Config{API: api, Verifier: signature.NewGitHub(secret), Owners: []string{"acme"}, Bots: map[string]string{"helper": "helper"}, Interval: -1})
				if err != nil {
					t.Fatal(err)
				}
				peer = c
				replies = func() int { return len(api.Comments()) }
				path = "/connectors/github"
				body = `{"tenant":"evil","action":"created","repository":{"name":"repo","owner":{"login":"acme"}},"sender":{"login":"human","type":"User"},"issue":{"number":7},"comment":{"id":91,"body":"@helper explain"}}`
			} else {
				// On Slack a run starts only for a person a client maps,
				// so the connector is a client's, made once the platform runs.
				api := slackflow.NewFake()
				pol, err := auth.ParsePolicy("test", "role viewer { allow read on * }\nrole operator extends viewer { allow write on runs }\ngrant operator to subject \"ana\"\n")
				if err != nil {
					t.Fatal(err)
				}
				cfg.SlackAPI, cfg.ClientInterval, cfg.Policy = func(string) slackflow.API { return api }, -1, pol
				replies = func() int { return len(api.Messages()) }
				path = "/connectors/slack"
				body = `{"tenant":"evil","type":"event_callback","event_id":"slack-1","team_id":"T01","event":{"type":"app_mention","user":"U01","channel":"C01","ts":"1700000000.000100","text":"<@U0HELPER> explain"}}`
			}
			if peer != nil {
				cfg.Connections = func(wire.AgentStore) ([]platform.Connection, error) {
					return []platform.Connection{{Tenant: "acme", Peer: peer}}, nil
				}
			}
			p, err := platform.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.Agents.Create(context.Background(), "acme", wire.AgentSpec{Name: "helper", Provider: "offline", Model: "offline", SystemPrompt: "Answer questions."}, "test"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			done := make(chan error, 1)
			go func() { done <- p.Run(ctx) }()
			t.Cleanup(func() {
				cancel()
				if err := <-done; err != nil {
					t.Error(err)
				}
			})
			wait := func(ready func() bool) {
				t.Helper()
				for !ready() {
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(time.Millisecond):
					}
				}
			}
			if peer == nil {
				if _, err := p.CreateClient(ctx, "acme", "test", platform.ClientInput{
					Client: clients.Client{
						ID: "helper-app", Name: "Helper", Surface: clients.Slack, Enabled: true,
						Identity: clients.Identity{AppID: "A0HELPER", Team: "T01"}, DefaultAgent: "helper",
						Bots:  map[string]string{"U0HELPER": "helper"},
						Slack: &clients.SlackOptions{Sessions: true, Machine: "svc-slack", People: map[string]string{"U01": "ana"}},
					},
					Token: clients.SecretInput{Value: "xoxb-integration"}, SigningSecret: clients.SecretInput{Value: secret},
				}); err != nil {
					t.Fatal(err)
				}
				wait(func() bool {
					w := httptest.NewRecorder()
					p.IngressHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
					return w.Code == http.StatusOK
				})
			} else {
				wait(peer.Ingesting)
			}
			s := &Server{P: p, Authn: &Authn{Dev: &auth.Principal{Subject: "test", Tenant: "acme", Kind: auth.User, Roles: []auth.Role{auth.Admin}}}}
			h := s.Handler()
			for _, path := range []string{"/api/me", "/api/runs", "/auth/login", "/"} {
				w := httptest.NewRecorder()
				p.IngressHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				if w.Code != http.StatusNotFound {
					t.Fatalf("forwarded ingress exposes %s: %d", path, w.Code)
				}
			}
			// Even a dev-auth console must reject unsigned external webhooks.
			bad := httptest.NewRecorder()
			h.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
			if bad.Code < 400 {
				t.Fatalf("unsigned request accepted: %d", bad.Code)
			}
			r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			mac := hmac.New(sha256.New, []byte(secret))
			if surface == "github" {
				mac.Write([]byte(body))
				r.Header.Set(signature.HeaderGitHubSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
				r.Header.Set("X-GitHub-Event", "issue_comment")
				r.Header.Set("X-GitHub-Delivery", "github-1")
			} else {
				ts := strconv.FormatInt(time.Now().Unix(), 10)
				mac.Write([]byte("v0:" + ts + ":" + body))
				r.Header.Set(signature.HeaderSlackTimestamp, ts)
				r.Header.Set(signature.HeaderSlackSignature, "v0="+hex.EncodeToString(mac.Sum(nil)))
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("webhook: %d %s", w.Code, w.Body.String())
			}
			wait(func() bool { return replies() == 1 })
			// The first reply can be the surface's placeholder, posted
			// before the run is admitted, so the run is waited for too.
			wait(func() bool { return len(p.Runs.List("acme", 1)) == 1 })
			w = httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
			if w.Code != 200 || !strings.Contains(w.Body.String(), "helper") {
				t.Fatalf("console API: %d %s", w.Code, w.Body.String())
			}
			s.Authn = &Authn{Dev: &auth.Principal{Subject: "other", Tenant: "evil", Kind: auth.User, Roles: []auth.Role{auth.Admin}}}
			w = httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
			if strings.Contains(w.Body.String(), "helper") {
				t.Fatal("external payload selected the tenant")
			}
		})
	}
}
