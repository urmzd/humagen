package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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

// ran records the commands a deploy runs instead of running them, with what
// each was handed on its standard input.
type ran struct {
	args  [][]string
	stdin []string
}

func (r *ran) run(cmd *exec.Cmd) error {
	r.args = append(r.args, cmd.Args)
	in := ""
	if cmd.Stdin != nil {
		b, err := io.ReadAll(cmd.Stdin)
		if err != nil {
			return err
		}
		in = string(b)
	}
	r.stdin = append(r.stdin, in)
	return nil
}

func TestDeployToKubernetesPutsNoSecretOnAnyCommandLine(t *testing.T) {
	t.Setenv("WARP_POSTGRES_URL", "postgres://warp:pg-from-the-shell@db:5432/warp")
	dir := t.TempDir()
	path := filepath.Join(dir, "warp.yaml")
	if err := os.WriteFile(path, []byte(`api_version: warp/v1
kind: System
metadata: {name: acme}
spec:
  auth:
    kind: oidc
    oidc: {issuer: "https://login.example.com", client_id: warp, audience: warp, client_secret: {ref: "keychain:warp/oidc"}}
  stores: {kind: postgres, postgres: {url: {ref: "env:WARP_POSTGRES_URL"}}}
  deploy:
    kind: kubernetes
    kubernetes: {namespace: agents, context: prod}
---
api_version: warp/v1
kind: Platform
metadata: {name: default}
spec:
  models: [{name: anthropic, kind: anthropic, anthropic: {key: {ref: "keychain:warp/anthropic"}}}]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	keys := credref.Resolver{GOOS: "linux", Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "anthropic") {
			return []byte("sk-from-the-keychain"), nil
		}
		return []byte("oidc-from-the-keychain"), nil
	}}
	var r ran
	if err := deploy(Options{}, []string{"-f", path}, strings.NewReader(""), io.Discard, keys, r.run); err != nil {
		t.Fatal(err)
	}
	secrets := []string{"pg-from-the-shell", "sk-from-the-keychain", "oidc-from-the-keychain"}
	for _, args := range r.args {
		for _, a := range args {
			for _, s := range secrets {
				if strings.Contains(a, s) {
					t.Errorf("%s is on a command line: %q", s, args)
				}
			}
		}
	}
	if len(r.args) != 2 || r.args[0][0] != "kubectl" || r.args[1][0] != "helm" {
		t.Fatalf("ran %q, want kubectl apply then helm", r.args)
	}
	if want := []string{"kubectl", "apply", "-f", "-", "-n", "agents", "--context", "prod"}; !slices.Equal(r.args[0], want) {
		t.Errorf("kubectl args = %q, want %q", r.args[0], want)
	}
	var got struct {
		APIVersion string            `json:"apiVersion"`
		Kind       string            `json:"kind"`
		Type       string            `json:"type"`
		Metadata   map[string]string `json:"metadata"`
		Data       map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(r.stdin[0]), &got); err != nil {
		t.Fatalf("kubectl's stdin is not a manifest: %v\n%s", err, r.stdin[0])
	}
	if got.APIVersion != "v1" || got.Kind != "Secret" || got.Type != "Opaque" || got.Metadata["name"] != "acme-env" || got.Metadata["namespace"] != "agents" {
		t.Errorf("manifest = %+v", got)
	}
	var keychain []string
	for k, v := range got.Data {
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			t.Fatalf("data.%s is not base64: %v", k, err)
		}
		switch {
		case k == "WARP_POSTGRES_URL":
			if !strings.Contains(string(b), "pg-from-the-shell") {
				t.Errorf("data.%s = %q, want the shell's value", k, b)
			}
		case strings.HasPrefix(k, "WARP_REF_"):
			keychain = append(keychain, string(b))
		default:
			t.Errorf("data has %s, which the spec does not reference", k)
		}
	}
	slices.Sort(keychain)
	if want := []string{"oidc-from-the-keychain", "sk-from-the-keychain"}; !slices.Equal(keychain, want) {
		t.Errorf("keychain values in the Secret = %q, want %q", keychain, want)
	}
}

func TestCarryNamesEachReferenceTheSameOnEveryDeploy(t *testing.T) {
	cfg := spec.Config{Platforms: []spec.Platform{{Metadata: spec.Metadata{Name: "default"}, Spec: spec.PlatformSpec{
		Surfaces: spec.Surfaces{Slack: &spec.Slack{Enabled: true, Team: "T1", BotToken: &spec.Ref{Ref: "keychain:warp/bot"}, AppToken: &spec.Ref{Ref: "keychain:warp/app"}}},
	}}}}
	cfg.System.Spec.Auth.Slot = spec.Slot{Kind: "oidc", Block: spec.MustJSON(map[string]any{"client_secret": map[string]any{"ref": "keychain:warp/oidc"}})}
	keys := credref.Resolver{GOOS: "linux", Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		return []byte(strings.Join(args, " ")), nil
	}}
	var first map[string]string
	for range 20 {
		c, err := carry(context.Background(), cfg, keys)
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = c.values
			continue
		}
		if !maps.Equal(c.values, first) {
			t.Fatalf("carry named the references %v, then %v", first, c.values)
		}
	}
}

func TestDeployToComposeLeavesTheCheckedInComposeProjectAlone(t *testing.T) {
	root := t.TempDir()
	checkedIn := filepath.Join(root, "deploy", "compose", "compose.yaml")
	if err := os.MkdirAll(filepath.Dir(checkedIn), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checkedIn, []byte("# the checked-in project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "warp.yaml")
	if err := os.WriteFile(path, []byte("api_version: warp/v1\nkind: System\nmetadata: {name: acme}\nspec:\n  deploy: {kind: compose, compose: {image: warp:dev}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var r ran
	if err := deploy(Options{}, []string{"-f", path}, strings.NewReader(""), io.Discard, credref.Default, r.run); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(checkedIn); string(b) != "# the checked-in project\n" {
		t.Errorf("deploy wrote over deploy/compose/compose.yaml:\n%s", b)
	}
	want := filepath.Join(root, ".warp", "compose")
	for _, f := range []string{"compose.yaml", "warp.yaml"} {
		if _, err := os.Stat(filepath.Join(want, f)); err != nil {
			t.Errorf("the project is not in %s: %v", want, err)
		}
	}
	if len(r.args) != 1 || !slices.Contains(r.args[0], want) {
		t.Errorf("ran %q, want docker compose on %s", r.args, want)
	}
}

// peopleSpec is a System document for kind whose people keep their own tool
// credentials under start.
func peopleSpec(t *testing.T, kind, start string) string {
	t.Helper()
	block := "compose: {image: \"ghcr.io/urmzd/warp:dev\"}"
	if kind == "kubernetes" {
		block = "kubernetes: {namespace: agents}"
	}
	path := filepath.Join(t.TempDir(), "warp.yaml")
	if err := os.WriteFile(path, []byte(`api_version: warp/v1
kind: System
metadata: {name: acme}
spec:
  auth:
    kind: oidc
    oidc: {issuer: "https://login.example.com", client_id: warp, audience: warp, client_secret: {ref: "env:OIDC_SECRET"}}
  person_refs: {default: ["`+start+`"]}
  deploy:
    kind: `+kind+`
    `+block+`
`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A person's reference is read where warp runs, when a run uses it, not
// when deploying, so a container cannot read one in this machine's keychain
// or 1Password. warp deploy refuses such a start for a compose or kubernetes
// target, naming the entry and what to do instead, and writes nothing.
func TestDeployRefusesPersonRefsTheTargetCannotRead(t *testing.T) {
	for _, c := range []struct{ kind, start, names string }{
		{"compose", "op://People/{subject}/", "1Password"},
		{"compose", "keychain:warp-people-{subject}/", "keychain"},
		{"kubernetes", "op://People/{subject}/", "1Password"},
		{"kubernetes", "keychain:warp-people-{subject}/", "keychain"},
	} {
		t.Run(c.kind+" "+c.start, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			r := &ran{}
			err := deploy(Options{}, []string{"-f", peopleSpec(t, c.kind, c.start)}, strings.NewReader(""), io.Discard, credref.Default, r.run)
			if err == nil {
				t.Fatal("deployed people's references the target cannot read")
			}
			for _, want := range []string{`person_refs.default[0] "` + c.start + `"`, c.names, "env:WARP_PERSON_{subject}_", "person_credentials.values: true", "deploy local"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not say %q: %v", want, err)
				}
			}
			if len(r.args) != 0 {
				t.Fatalf("a refused deploy ran %q", r.args)
			}
			if err := deployCmd(Options{}, []string{"-f", peopleSpec(t, c.kind, c.start), "-o", out}, strings.NewReader(""), io.Discard); err == nil {
				t.Fatal("wrote a deployment of people's references the target cannot read")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatalf("a refused deploy wrote %s: %v", out, err)
			}
		})
	}
}

// The kubernetes block's values are merged over the spec's documents after
// warp deploy has carried and checked them, so they may not set the chart's
// system or platforms: a person's keychain start, or one of the spec's own
// references, would otherwise reach the cluster unread and unchecked.
func TestDeployRefusesValuesThatReplaceTheSpec(t *testing.T) {
	for _, c := range []struct{ key, values string }{
		{"system", `{system: {spec: {person_refs: {default: ["keychain:warp-people-{subject}/"]}}}}`},
		{"system", `{system: {spec: {stores: {kind: postgres, postgres: {url: {ref: "op://Infra/warp/db"}}}}}}`},
		{"platforms", `{platforms: [{spec: {models: [{kind: anthropic, anthropic: {api_key: {ref: "keychain:anthropic"}}}]}}]}`},
	} {
		t.Run(c.key, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "warp.yaml")
			if err := os.WriteFile(path, []byte(`api_version: warp/v1
kind: System
metadata: {name: acme}
spec:
  auth:
    kind: oidc
    oidc: {issuer: "https://login.example.com", client_id: warp, audience: warp, client_secret: {ref: "env:OIDC_SECRET"}}
  person_refs: {default: ["env:WARP_PERSON_{subject}_"]}
  deploy:
    kind: kubernetes
    kubernetes: {namespace: agents, values: `+c.values+`}
`), 0o600); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "out")
			r := &ran{}
			err := deploy(Options{}, []string{"-f", path, "-o", out}, strings.NewReader(""), io.Discard, credref.Default, r.run)
			if err == nil || !strings.Contains(err.Error(), "kubernetes values."+c.key) || !strings.Contains(err.Error(), "set it in the spec's documents instead") {
				t.Fatalf("deploy = %v, want values.%s refused", err, c.key)
			}
			if len(r.args) != 0 {
				t.Fatalf("a refused deploy ran %q", r.args)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatalf("a refused deploy wrote %s: %v", out, err)
			}
		})
	}
}

// An env: start names variables, one per person and tool, that nobody knows
// when deploying, so warp deploy cannot pass them. It says so, in what it
// prints and in the file it writes, so whoever deploys sets them in the
// target.
func TestDeploySaysPeoplesVariablesAreTheTargetsToSet(t *testing.T) {
	for kind, file := range map[string]string{"compose": "compose.yaml", "kubernetes": "values.yaml"} {
		t.Run(kind, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			var said strings.Builder
			if err := deployCmd(Options{}, []string{"-f", peopleSpec(t, kind, "env:WARP_PERSON_{subject}_"), "-o", out}, strings.NewReader(""), &said); err != nil {
				t.Fatal(err)
			}
			written, err := os.ReadFile(filepath.Join(out, file))
			if err != nil {
				t.Fatal(err)
			}
			for where, text := range map[string]string{"what it printed": said.String(), file: string(written)} {
				for _, want := range []string{"env:WARP_PERSON_{subject}_", "which warp deploy does not pass"} {
					if !strings.Contains(text, want) {
						t.Errorf("%s does not say %q:\n%s", where, want, text)
					}
				}
			}
			if strings.Contains(string(written), "WARP_PERSON_") && !strings.Contains(string(written), "# people's own tool credentials") {
				t.Errorf("%s names people's variables outside its comment:\n%s", file, written)
			}
		})
	}
}

// The reference's System example deploys where it says: its person_refs are
// ones its deploy target can read.
func TestTheReferenceExampleDeploysItsPeoplesReferences(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```yaml\n(.*?)```").FindAllStringSubmatch(string(doc), -1)
	path := filepath.Join(t.TempDir(), "warp.yaml")
	for _, b := range blocks {
		if strings.Contains(b[1], "kind: System") {
			if err := os.WriteFile(path, []byte(b[1]), 0o600); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	cfg, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	kind := cfg.System.Spec.Deploy.Kind
	if len(cfg.System.Spec.PersonRefs) == 0 || kind == "" || kind == "local" {
		t.Fatalf("the example has person_refs %v and deploys to %q", cfg.System.Spec.PersonRefs, kind)
	}
	if _, err := remotePersonRefs(cfg, kind); err != nil {
		t.Fatalf("the reference's example cannot be deployed as it says: %v", err)
	}
}
