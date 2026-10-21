package app

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urmzd/warp/pkg/credref"
	"github.com/urmzd/warp/pkg/spec"
)

func TestDeployWritesAComposeProjectWithNoSecretsInIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warp.yaml")
	os.WriteFile(path, []byte(`api_version: warp/v1
kind: System
metadata: {name: acme}
spec:
  auth:
    kind: oidc
    oidc: {issuer: "https://login.example.com", client_id: warp, client_secret: {ref: "env:OIDC_SECRET"}}
  deploy:
    kind: compose
    compose: {image: "ghcr.io/urmzd/warp:dev", postgres: true, port: 9090}
---
api_version: warp/v1
kind: Platform
metadata: {name: default}
spec:
  models: [{name: anthropic, kind: anthropic, anthropic: {key: {ref: "env:ANTHROPIC_API_KEY"}}}]
`), 0o600)
	out := filepath.Join(dir, "out")
	if err := deployCmd(Options{}, []string{"-f", path, "-o", out}, strings.NewReader(""), io.Discard); err != nil {
		t.Fatal(err)
	}
	project, _ := os.ReadFile(filepath.Join(out, "compose.yaml"))
	for _, want := range []string{"ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY:-}", "OIDC_SECRET: ${OIDC_SECRET:-}", "postgres:17-alpine", "9090:8080", "WARP_POSTGRES_URL"} {
		if !strings.Contains(string(project), want) {
			t.Errorf("compose.yaml lacks %q:\n%s", want, project)
		}
	}
	written, err := spec.Load(filepath.Join(out, "warp.yaml"))
	if err != nil {
		t.Fatalf("the written spec does not load: %v", err)
	}
	if written.System.Spec.Stores.Kind != "postgres" || written.System.Spec.Server.Addr != "0.0.0.0:8080" {
		t.Errorf("written spec = %+v", written.System.Spec)
	}
	if _, err := exec.LookPath("docker"); err == nil {
		cmd := exec.Command("docker", "compose", "--project-directory", out, "config", "-q")
		if b, err := cmd.CombinedOutput(); err != nil && !strings.Contains(string(b), "Cannot connect") {
			t.Errorf("docker compose config: %v\n%s", err, b)
		}
	}
}

func TestCarryTurnsAKeychainReferenceIntoAnEnvOne(t *testing.T) {
	cfg := spec.Config{Platforms: []spec.Platform{{Metadata: spec.Metadata{Name: "default"}, Spec: spec.PlatformSpec{
		Surfaces: spec.Surfaces{Slack: &spec.Slack{Enabled: true, Team: "T1", BotToken: &spec.Ref{Ref: "keychain:warp/bot"}, AppToken: &spec.Ref{Ref: "env:SLACK_APP_TOKEN"}}},
	}}}}
	keys := credref.Resolver{Run: func(context.Context, string, ...string) ([]byte, error) { return []byte("xoxb-1"), nil }, GOOS: "linux"}
	c, err := carry(context.Background(), cfg, keys)
	if err != nil {
		t.Fatal(err)
	}
	sl := c.cfg.Platforms[0].Spec.Surfaces.Slack
	if sl.BotToken.Ref != "env:WARP_REF_1" || c.values["WARP_REF_1"] != "xoxb-1" || sl.AppToken.Ref != "env:SLACK_APP_TOKEN" {
		t.Fatalf("carried = %+v %+v", sl, c.values)
	}
	b, _ := spec.Marshal(c.cfg)
	if strings.Contains(string(b), "xoxb") {
		t.Fatal("a secret was written into the spec")
	}
}

func TestDeployingDevAuthKeepsItOnLoopback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warp.yaml")
	os.WriteFile(path, []byte("api_version: warp/v1\nkind: System\nspec:\n  deploy: {kind: compose, compose: {image: warp:dev}}\n"), 0o600)
	out := filepath.Join(dir, "out")
	if err := deployCmd(Options{}, []string{"-f", path, "-o", out}, strings.NewReader(""), io.Discard); err != nil {
		t.Fatal(err)
	}
	project, _ := os.ReadFile(filepath.Join(out, "compose.yaml"))
	if !strings.Contains(string(project), "network_mode: host") || !strings.Contains(string(project), "127.0.0.1:8080") {
		t.Errorf("dev auth was published beyond loopback:\n%s", project)
	}
}
