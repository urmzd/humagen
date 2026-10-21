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
	if err := os.WriteFile(path, []byte(`api_version: warp/v1
kind: System
metadata: {name: acme}
spec:
  auth:
    kind: oidc
    oidc: {issuer: "https://login.example.com", client_id: warp, audience: warp, client_secret: {ref: "env:OIDC_SECRET"}}
  deploy:
    kind: compose
    compose: {image: "ghcr.io/urmzd/warp:dev", postgres: true, port: 9090}
---
api_version: warp/v1
kind: Platform
metadata: {name: default}
spec:
  models: [{name: anthropic, kind: anthropic, anthropic: {key: {ref: "env:ANTHROPIC_API_KEY"}}}]
`), 0o600); err != nil {
		t.Fatal(err)
	}
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
	if err := os.WriteFile(path, []byte("api_version: warp/v1\nkind: System\nspec:\n  deploy: {kind: compose, compose: {image: warp:dev}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if err := deployCmd(Options{}, []string{"-f", path, "-o", out}, strings.NewReader(""), io.Discard); err != nil {
		t.Fatal(err)
	}
	project, _ := os.ReadFile(filepath.Join(out, "compose.yaml"))
	if !strings.Contains(string(project), "network_mode: host") || !strings.Contains(string(project), "127.0.0.1:8080") {
		t.Errorf("dev auth was published beyond loopback:\n%s", project)
	}
}

func TestDeployWritesTheChartAndItsValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warp.yaml")
	if err := os.WriteFile(path, []byte(`api_version: warp/v1
kind: System
metadata: {name: acme}
spec:
  auth:
    kind: oidc
    oidc: {issuer: "https://login.example.com", client_id: warp, audience: warp, client_secret: {ref: "env:OIDC_SECRET"}}
  stores: {kind: postgres, postgres: {url: {ref: "env:WARP_POSTGRES_URL"}}}
  deploy:
    kind: kubernetes
    kubernetes: {namespace: agents, host: warp.example.com, image: {repository: ghcr.io/acme/warp, tag: v1}}
---
api_version: warp/v1
kind: Platform
metadata: {name: default, display_name: Acme}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if err := deployCmd(Options{}, []string{"-f", path, "-o", out}, strings.NewReader(""), io.Discard); err != nil {
		t.Fatal(err)
	}
	values, _ := os.ReadFile(filepath.Join(out, "values.yaml"))
	for _, want := range []string{"existingSecret: acme-env", "repository: ghcr.io/acme/warp", "host: warp.example.com", "display_name: Acme", "env:OIDC_SECRET"} {
		if !strings.Contains(string(values), want) {
			t.Errorf("values lack %q:\n%s", want, values)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "chart", "Chart.yaml")); err != nil {
		t.Fatalf("the chart was not written: %v", err)
	}
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm not on PATH; the render is not checked")
	}
	rendered, err := exec.Command(helm, "template", "acme", filepath.Join(out, "chart"), "-f", filepath.Join(out, "values.yaml")).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, rendered)
	}
	if !strings.Contains(string(rendered), "kind: Platform") || !strings.Contains(string(rendered), "warp.example.com") {
		t.Errorf("render:\n%s", rendered)
	}
}

func TestDeployRefusesLoopbackSignInForACluster(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warp.yaml")
	if err := os.WriteFile(path, []byte("api_version: warp/v1\nkind: System\nspec:\n  deploy: {kind: kubernetes}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := deployCmd(Options{}, []string{"-f", path, "-o", t.TempDir()}, strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("err = %v", err)
	}
}
