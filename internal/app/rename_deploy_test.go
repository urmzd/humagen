package app

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/urmzd/humagen/pkg/credref"
)

// kubeSpec is a System document deploying release acme to kubernetes.
const kubeSpec = `api_version: humagen/v1
kind: System
metadata: {name: acme}
spec:
  auth:
    kind: oidc
    oidc: {issuer: "https://login.example.com", client_id: humagen, audience: humagen, client_secret: {ref: "env:WARP_OIDC_CLIENT_SECRET"}}
  deploy: {kind: kubernetes, kubernetes: {namespace: agents}}
`

// deployedValues deploys kubeSpec with helm answering as list and get
// answer, and returns the values helm upgrade was given.
func deployedValues(t *testing.T, list, get string) map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	if err := os.WriteFile(path, []byte(kubeSpec), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HUMAGEN_OIDC_CLIENT_SECRET", "s")
	var values map[string]any
	r := ran{reply: func(args []string) string {
		switch {
		case args[0] == "helm" && args[1] == "list":
			return list
		case args[0] == "helm" && args[1] == "get":
			return get
		case args[0] == "helm" && args[1] == "upgrade":
			i := slices.Index(args, "-f")
			b, err := os.ReadFile(args[i+1])
			if err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal(b, &values); err != nil {
				t.Fatal(err)
			}
		}
		return ""
	}}
	if err := deploy(Options{}, []string{"-f", path}, strings.NewReader(""), io.Discard, credref.Default, r.run); err != nil {
		t.Fatal(err)
	}
	if values == nil {
		t.Fatalf("helm upgrade did not run: %q", r.args)
	}
	return values
}

// A release the warp chart installed keeps its names on every deploy: the
// first, when helm lists the warp chart, and every later one, when it lists
// this chart and the values it was given keep the old name.
func TestDeployKeepsTheNamesOfAReleaseTheWarpChartInstalled(t *testing.T) {
	for name, tc := range map[string]struct {
		list, get string
		want      any
	}{
		"first upgrade":   {`[{"name":"acme","chart":"warp-0.1.0"}]`, "", "warp"},
		"later upgrade":   {`[{"name":"acme","chart":"humagen-0.2.0"}]`, `{"nameOverride":"warp"}`, "warp"},
		"humagen install": {`[{"name":"acme","chart":"humagen-0.2.0"}]`, `null`, nil},
		"no release":      {`[]`, "", nil},
		"another release": {`[{"name":"acme-old","chart":"warp-0.1.0"}]`, "", nil},
	} {
		t.Run(name, func(t *testing.T) {
			if got := deployedValues(t, tc.list, tc.get)["nameOverride"]; got != tc.want {
				t.Errorf("nameOverride = %v, want %v", got, tc.want)
			}
		})
	}
}

// The Secret is read from whichever name the shell sets: a spec written
// before the rename names WARP_X, and the operator renamed it to HUMAGEN_X.
func TestDeployReadsASecretUnderEitherPrefix(t *testing.T) {
	t.Setenv("WARP_OIDC_CLIENT_SECRET", "")
	values := deployedValues(t, "[]", "")
	if values["existingSecret"] != "acme-env" {
		t.Errorf("existingSecret = %v", values["existingSecret"])
	}
}

// Generated compose keeps the identities an existing project's volumes and
// Postgres were made with, and passes a variable the spec names under its
// old name from the shell under either name.
func TestGeneratedComposeKeepsItsIdentities(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "humagen.yaml")
	doc := "api_version: humagen/v1\nkind: System\nmetadata: {name: acme}\nspec:\n" +
		"  auth: {kind: oidc, oidc: {issuer: \"https://login.example.com\", client_id: c, audience: a, client_secret: {ref: \"env:WARP_OIDC_CLIENT_SECRET\"}}}\n" +
		"  deploy: {kind: compose, compose: {image: humagen:dev, postgres: true}}\n"
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")
	if err := deploy(Options{}, []string{"-f", path, "-o", out}, strings.NewReader(""), io.Discard, credref.Default, (&ran{}).run); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var project struct {
		Services map[string]struct {
			Command     []string          `yaml:"command"`
			Volumes     []string          `yaml:"volumes"`
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
		Volumes map[string]any `yaml:"volumes"`
	}
	if err := yaml.Unmarshal(b, &project); err != nil {
		t.Fatal(err)
	}
	svc, ok := project.Services["warp"]
	if !ok {
		t.Fatalf("no warp service:\n%s", b)
	}
	if _, ok := project.Volumes["warp-data"]; !ok {
		t.Errorf("no warp-data volume:\n%s", b)
	}
	for _, want := range []string{"warp-data:/var/lib/warp", "./humagen.yaml:/etc/warp/humagen.yaml:ro"} {
		if !slices.Contains(svc.Volumes, want) {
			t.Errorf("volumes %q lack %q", svc.Volumes, want)
		}
	}
	if !slices.Contains(svc.Command, "/usr/share/warp/web") || !slices.Contains(svc.Command, "/etc/warp/humagen.yaml") {
		t.Errorf("command %q", svc.Command)
	}
	if got := svc.Environment["WARP_OIDC_CLIENT_SECRET"]; got != "${HUMAGEN_OIDC_CLIENT_SECRET:-${WARP_OIDC_CLIENT_SECRET:-}}" {
		t.Errorf("WARP_OIDC_CLIENT_SECRET is passed as %q", got)
	}
	if got := svc.Environment["HUMAGEN_POSTGRES_URL"]; !strings.HasPrefix(got, "postgres://warp:${POSTGRES_PASSWORD:-warp}@") || !strings.HasSuffix(got, "/warp?sslmode=disable") {
		t.Errorf("HUMAGEN_POSTGRES_URL = %q", got)
	}
	if !strings.Contains(string(b), "POSTGRES_USER: warp") || !strings.Contains(string(b), "POSTGRES_DB: warp") {
		t.Errorf("the bundled Postgres lost its user or database:\n%s", b)
	}
}

// A project an earlier deploy wrote to .warp/compose is written there again.
func TestDeployReusesTheOldComposeDirectory(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, ".warp", "compose")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "humagen.yaml")
	if err := os.WriteFile(path, []byte("api_version: humagen/v1\nkind: System\nmetadata: {name: acme}\nspec:\n  deploy: {kind: compose, compose: {image: humagen:dev}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	var r ran
	if err := deploy(Options{}, []string{"-f", path}, strings.NewReader(""), &out, credref.Default, r.run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(old, "compose.yaml")); err != nil {
		t.Errorf("the project is not in %s: %v", old, err)
	}
	if !strings.Contains(out.String(), old) {
		t.Errorf("no note naming %s: %q", old, out.String())
	}
}

// A compose image from before the rename cannot read the spec deploy writes,
// so deploy refuses it, naming why; a tag it cannot date is left alone.
func TestDeployRefusesAComposeImageFromBeforeTheRename(t *testing.T) {
	for image, refused := range map[string]bool{
		"humagen:0.20.2": true, "ghcr.io/acme/warp:v0.19.0": true, "warp:0.9.0": true,
		"humagen:0.21.0": false, "humagen:v0.22.1": false, "humagen:latest": false, "humagen:sha-abc123": false, "humagen": false,
	} {
		path := filepath.Join(t.TempDir(), "humagen.yaml")
		doc := "api_version: humagen/v1\nkind: System\nmetadata: {name: acme}\nspec:\n  deploy: {kind: compose, compose: {image: \"" + image + "\"}}\n"
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		err := deploy(Options{}, []string{"-f", path, "-o", t.TempDir()}, strings.NewReader(""), io.Discard, credref.Default, (&ran{}).run)
		if refused && (err == nil || !strings.Contains(err.Error(), "before the rename")) {
			t.Errorf("%s: err = %v, want a refusal", image, err)
		}
		if !refused && err != nil {
			t.Errorf("%s: %v", image, err)
		}
	}
}

func TestDeployRefusesAKubernetesImageFromBeforeTheRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	doc := strings.Replace(kubeSpec, "kubernetes: {namespace: agents}", "kubernetes: {namespace: agents, image: {tag: \"0.20.2\"}}", 1)
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	var r ran
	err := deploy(Options{}, []string{"-f", path}, strings.NewReader(""), io.Discard, credref.Default, r.run)
	if err == nil || !strings.Contains(err.Error(), "before the rename") || len(r.args) != 0 {
		t.Fatalf("err = %v, ran %q", err, r.args)
	}
}
