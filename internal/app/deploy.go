package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	deployfs "github.com/urmzd/humagen/deploy"
	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

// deployCmd is `humagen deploy`: run the spec where its System document's
// deploy slot says. local is `humagen start` here; compose writes a Docker
// Compose project and brings it up; kubernetes installs the chart humagen
// carries with Helm. -o writes what would be applied and stops.
//
// A secret never lands in a written file. An env: reference stays one and is
// passed from this process's environment; a keychain: or op:// reference is
// read here and handed to the target as an environment variable it then
// references, because the target cannot reach this machine's keychain.
// People's own references are not the spec's: the server reads them where it
// runs (see remotePersonRefs), so a target is refused person_refs it cannot read.
// Nor does one land on a command line: every user of this machine can read a
// command's arguments (ps, /proc), so a secret reaches a tool on its standard
// input or in its environment.
func deployCmd(o Options, args []string, in io.Reader, out io.Writer) error {
	return deploy(o, args, in, out, credref.Default, (*exec.Cmd).Run)
}

// deploy is deployCmd with what it reaches beyond this process: keys reads
// keychain and 1Password references, and run runs docker, helm and kubectl.
func deploy(o Options, args []string, in io.Reader, out io.Writer, keys credref.Resolver, run func(*exec.Cmd) error) error {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	var paths files
	fs.Var(&paths, "f", "a spec file or directory; repeatable. Empty means humagen.yaml in the config directory")
	target := fs.String("target", "", "local, compose or kubernetes, over the spec's deploy.kind")
	outDir := fs.String("o", "", "write the deployment to this directory and stop, applying nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(paths) == 0 {
		def, err := defaultSpec(o.notes)
		if err != nil {
			return err
		}
		if def != "" {
			paths = append(paths, def)
		}
	}
	var cfg spec.Config
	if len(paths) > 0 {
		var err error
		if cfg, err = loadSpec(o, paths...); err != nil {
			return err
		}
	}
	if err := cfg.Check(o.kinds()); err != nil {
		return err
	}
	cfg.System.Defaults()
	d := cfg.System.Spec.Deploy
	if *target != "" && *target != d.Kind {
		d = spec.Slot{Kind: *target}
	}
	ctx := context.Background()
	switch d.Kind {
	case "", "local":
		if *outDir != "" {
			return errors.New("deploy: local has nothing to write; it is humagen start")
		}
		var startArgs []string
		for _, p := range paths {
			startArgs = append(startArgs, "-f", p)
		}
		return start(o, startArgs, in, out)
	case "compose":
		b, err := spec.Block[spec.Compose](d)
		if err != nil {
			return err
		}
		return deployCompose(ctx, cfg, b, paths, *outDir, keys, run, out)
	case "kubernetes":
		b, err := spec.Block[spec.Kubernetes](d)
		if err != nil {
			return err
		}
		return deployKubernetes(ctx, cfg, b, *outDir, keys, run, out)
	default:
		return fmt.Errorf("deploy: kind %q is not local, compose or kubernetes", d.Kind)
	}
}

// remotePersonRefs checks where the System document lets people keep their
// own tool credentials (person_refs) against a target that is not this
// machine, and returns the env: starts, whose variables must be set there.
//
// A person's reference is not one of the spec's own. The server reads it,
// with its own access, when a run uses it, so carry has nothing to read here
// and nothing to pass. A keychain: or op:// start names this machine's
// keychain or the 1Password account it signed in to, which a container
// cannot reach, so it is refused, naming the entry and what to do instead.
// An env: start names variables, one per person and tool, that nobody knows
// when deploying, so they are not passed from this shell: whoever deploys is
// told to set them in the target.
func remotePersonRefs(cfg spec.Config, kind string) (env []string, err error) {
	byTenant := cfg.System.Spec.PersonRefs
	var refused []string
	for _, tenant := range slices.Sorted(maps.Keys(byTenant)) {
		for i, start := range byTenant[tenant] {
			if strings.HasPrefix(start, "env:") {
				env = append(env, start)
				continue
			}
			where := "this machine's keychain"
			if strings.HasPrefix(start, "op://") {
				where = "the 1Password account this machine signed in to"
			}
			refused = append(refused, fmt.Sprintf("system person_refs.%s[%d] %q names %s", tenant, i, start, where))
		}
	}
	if len(refused) > 0 {
		return nil, fmt.Errorf("deploy: %s, which a %s target cannot reach: humagen reads a person's reference where it runs, when a run uses it. "+
			"For a %s target, list an env: start in person_refs instead (env:HUMAGEN_PERSON_{subject}_) and set those variables in the target, "+
			"let people store their credential with humagen (workspace.person_credentials.values: true), or deploy local",
			strings.Join(refused, "; "), kind, kind)
	}
	return env, nil
}

// personEnvNote tells whoever deploys that the variables people's env:
// references name are theirs to set in the target, where says how. Empty
// when no start is an env: one.
func personEnvNote(starts []string, where string) string {
	if len(starts) == 0 {
		return ""
	}
	return fmt.Sprintf("people's own tool credentials are read from variables under %s, one per person and tool, which humagen deploy does not pass. "+
		"Set each %s", strings.Join(starts, ", "), where)
}

// carried is a spec ready to leave this machine: every reference an env:
// one, and the values of those this process must supply.
type carried struct {
	cfg spec.Config
	// env names every variable the spec references, in order.
	env []string
	// values are variables read here, from a keychain or 1Password, under the
	// names the spec now references.
	values map[string]string
}

// carry rewrites the spec's references for a target that reads only its own
// environment.
func carry(ctx context.Context, cfg spec.Config, keys credref.Resolver) (carried, error) {
	c := carried{values: map[string]string{}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return c, err
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return c, err
	}
	seen := map[string]string{}
	var walk func(v any) error
	walk = func(v any) error {
		switch t := v.(type) {
		case map[string]any:
			if ref, ok := t["ref"].(string); ok && len(t) == 1 {
				if name, isEnv := strings.CutPrefix(ref, "env:"); isEnv {
					if !slices.Contains(c.env, name) {
						c.env = append(c.env, name)
					}
					return nil
				}
				name, done := seen[ref]
				if !done {
					val, err := keys.Resolve(ctx, ref)
					if err != nil {
						return err
					}
					name = fmt.Sprintf("HUMAGEN_REF_%d", len(seen)+1)
					seen[ref], c.values[name] = name, val
					c.env = append(c.env, name)
				}
				t["ref"] = "env:" + name
				return nil
			}
			// In key order, so each reference gets the same HUMAGEN_REF_n on
			// every deploy and a written deployment does not churn.
			for _, k := range slices.Sorted(maps.Keys(t)) {
				if err := walk(t[k]); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range t {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(tree); err != nil {
		return c, err
	}
	raw, _ = json.Marshal(tree)
	if err := json.Unmarshal(raw, &c.cfg); err != nil {
		return c, err
	}
	return c, nil
}

// environ is this process's environment plus the values carry read.
func (c carried) environ() []string {
	env := os.Environ()
	for k, v := range c.values {
		env = append(env, k+"="+v)
	}
	return env
}

// loopbackAuth reports whether the spec's sign-in only answers on loopback.
func loopbackAuth(cfg spec.Config) bool {
	switch cfg.System.Spec.Auth.Kind {
	case "dev", "mock", "tailscale":
		return true
	}
	return false
}

// specDir is the directory the spec was read from: the first path when it
// is a directory, else the directory holding it.
func specDir(paths []string) string {
	if len(paths) == 0 {
		return "."
	}
	if st, err := os.Stat(paths[0]); err == nil && st.IsDir() {
		return paths[0]
	}
	return filepath.Dir(paths[0])
}

func deployCompose(ctx context.Context, cfg spec.Config, b spec.Compose, paths []string, outDir string, keys credref.Resolver, run func(*exec.Cmd) error, out io.Writer) error {
	name := cfg.System.Metadata.Name
	dir := b.Dir
	if outDir != "" {
		dir = outDir
	}
	// The default is .humagen/compose beside the spec, which git ignores. A spec
	// at the root of a humagen checkout, where the image is built from, then
	// cannot write over the checked-in deploy/compose project.
	if dir == "" {
		dir = composeDir(specDir(paths), out)
	}
	port := b.Port
	if port == 0 {
		_, p, _ := net.SplitHostPort(cfg.System.Spec.Server.Addr)
		port, _ = strconv.Atoi(p)
	}
	if port == 0 {
		port = 8080
	}
	people, err := remotePersonRefs(cfg, "compose")
	if err != nil {
		return err
	}
	note := personEnvNote(people, "in the humagen service's environment yourself, for example in a compose.override.yaml beside compose.yaml, which humagen deploy does not write")
	// Postgres beside humagen: the stores point at it, by a variable the
	// project sets.
	if b.Postgres {
		cfg.System.Spec.Stores = spec.Slot{Kind: "postgres", Block: spec.MustJSON(spec.URLBlock{URL: &spec.Ref{Ref: "env:" + envPostgres}})}
	}
	c, err := carry(ctx, cfg, keys)
	if err != nil {
		return err
	}
	// Sign-in that answers only on loopback runs on the host's network, so
	// it is still only reachable from this machine.
	hostNet := loopbackAuth(c.cfg)
	addr := "0.0.0.0:8080"
	if hostNet {
		addr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	c.cfg.System.Spec.Server.Addr = addr
	c.cfg.System.Spec.Deploy = spec.Slot{}
	doc, err := spec.Marshal(c.cfg)
	if err != nil {
		return err
	}

	svc := map[string]any{
		"restart": "unless-stopped",
		"command": []string{"start", "-f", legacy.SpecDir + "/humagen.yaml", "-addr", addr, "-web", legacy.WebDir},
		"volumes": []string{legacy.DataVolume + ":" + legacy.DataDir, "./humagen.yaml:" + legacy.SpecDir + "/humagen.yaml:ro"},
	}
	switch {
	case b.Image != "":
		svc["image"] = b.Image
	default:
		ctxDir := b.Build
		if ctxDir == "" {
			wd, _ := os.Getwd()
			ctxDir = wd
		}
		if _, err := os.Stat(filepath.Join(ctxDir, "Dockerfile")); err != nil {
			return fmt.Errorf("deploy: compose needs an image, or build pointing at a humagen checkout (no Dockerfile in %s)", ctxDir)
		}
		abs, _ := filepath.Abs(ctxDir)
		svc["build"] = map[string]any{"context": abs}
		svc["image"] = name + ":local"
	}
	env := map[string]string{}
	for _, v := range c.env {
		env[v] = interpolate(v)
	}
	services := map[string]any{legacy.DeployName: svc}
	if hostNet {
		svc["network_mode"] = "host"
	} else {
		svc["ports"] = []string{fmt.Sprintf("%d:8080", port)}
	}
	volumes := map[string]any{legacy.DataVolume: map[string]any{}}
	if b.Postgres {
		host := "postgres:5432"
		pg := map[string]any{
			"image":       "postgres:17-alpine",
			"restart":     "unless-stopped",
			"environment": map[string]string{"POSTGRES_USER": legacy.DeployName, "POSTGRES_PASSWORD": "${POSTGRES_PASSWORD:-" + legacy.DeployName + "}", "POSTGRES_DB": legacy.DeployName},
			"volumes":     []string{"postgres-data:/var/lib/postgresql/data"},
			"healthcheck": map[string]any{"test": []string{"CMD-SHELL", "pg_isready -U " + legacy.DeployName + " -d " + legacy.DeployName}, "interval": "5s", "retries": 20},
		}
		if hostNet {
			pg["ports"] = []string{"127.0.0.1:55432:5432"}
			host = "127.0.0.1:55432"
		}
		services["postgres"] = pg
		volumes["postgres-data"] = map[string]any{}
		env[envPostgres] = "postgres://" + legacy.DeployName + ":${POSTGRES_PASSWORD:-" + legacy.DeployName + "}@" + host + "/" + legacy.DeployName + "?sslmode=disable"
		svc["depends_on"] = map[string]any{"postgres": map[string]string{"condition": "service_healthy"}}
	}
	svc["environment"] = env
	project, err := yaml.Marshal(map[string]any{"name": name, "services": services, "volumes": volumes})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	header := "# Written by `humagen deploy` from the spec. Edit the spec and deploy again;\n# secrets are passed from the environment and are not in this file.\n" + comment(note)
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), append([]byte(header), project...), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "humagen.yaml"), doc, 0o600); err != nil {
		return err
	}
	if outDir != "" {
		fmt.Fprintf(out, "wrote %s; run it with: docker compose --project-directory %s up -d\n", dir, dir)
		if len(c.env) > 0 {
			fmt.Fprintf(out, "it reads %s from the environment\n", strings.Join(c.env, ", "))
		}
		if note != "" {
			fmt.Fprintln(out, note)
		}
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "--project-directory", dir, "up", "-d", "--build")
	cmd.Env, cmd.Stdout, cmd.Stderr = c.environ(), out, os.Stderr
	fmt.Fprintf(out, "humagen: deploying %s with docker compose from %s\n", name, dir)
	if err := run(cmd); err != nil {
		return fmt.Errorf("deploy: docker compose: %w", err)
	}
	fmt.Fprintf(out, "humagen: %s is up at http://127.0.0.1:%d\n", name, port)
	if note != "" {
		fmt.Fprintln(out, "humagen: "+note)
	}
	return nil
}

// comment is text as YAML comment lines, ending in a newline. Empty stays
// empty.
func comment(text string) string {
	if text == "" {
		return ""
	}
	return "#\n# " + strings.ReplaceAll(text, ". ", ".\n# ") + "\n"
}

func deployKubernetes(ctx context.Context, cfg spec.Config, b spec.Kubernetes, outDir string, keys credref.Resolver, run func(*exec.Cmd) error, out io.Writer) error {
	if loopbackAuth(cfg) {
		return fmt.Errorf("deploy: auth.kind %s only answers on loopback, so it cannot serve a cluster; use oidc", cfg.System.Spec.Auth.Kind)
	}
	release := b.Release
	if release == "" {
		release = cfg.System.Metadata.Name
	}
	ns := b.Namespace
	if ns == "" {
		ns = legacy.DeployName
	}
	if err := ownValues(b); err != nil {
		return err
	}
	people, err := remotePersonRefs(cfg, "kubernetes")
	if err != nil {
		return err
	}
	note := personEnvNote(people, "in the pod's environment yourself, for example as extraEnv in the kubernetes block's values, from a Secret of your own")
	c, err := carry(ctx, cfg, keys)
	if err != nil {
		return err
	}
	c.cfg.System.Spec.Deploy = spec.Slot{}
	values, err := chartValues(c, b, release)
	if err != nil {
		return err
	}
	helmScope := []string{"-n", ns}
	if b.Context != "" {
		helmScope = append(helmScope, "--kube-context", b.Context)
	}
	// A release the warp chart installed keeps its Deployment, Service and
	// volume only under the old name. Asked on every deploy, since after the
	// first upgrade the release's chart is this one.
	if _, set := values["nameOverride"]; !set && outDir == "" {
		old, err := installedByOldChart(ctx, release, helmScope, run)
		if err != nil {
			return err
		}
		if old {
			values["nameOverride"] = legacy.ChartName
			fmt.Fprintf(out, "humagen: release %s was installed by the %s chart; it keeps its names with nameOverride: %s\n", release, legacy.ChartName, legacy.ChartName)
		}
	}
	dir := outDir
	if dir == "" {
		if dir, err = os.MkdirTemp("", "humagen-deploy-"); err != nil {
			return err
		}
		defer os.RemoveAll(dir)
	}
	chart := filepath.Join(dir, "chart")
	if err := os.CopyFS(chart, mustSub(deployfs.Chart, "helm/humagen")); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("deploy: write the chart: %w", err)
	}
	raw, err := yaml.Marshal(values)
	if err != nil {
		return err
	}
	header := "# Written by `humagen deploy` from the spec. Secrets are in the Secret named\n# by existingSecret, created from the deploying shell; none are in here.\n" + comment(note)
	if err := os.WriteFile(filepath.Join(dir, "values.yaml"), append([]byte(header), raw...), 0o600); err != nil {
		return err
	}
	helmArgs := []string{"upgrade", "--install", release, chart, "-n", ns, "--create-namespace", "-f", filepath.Join(dir, "values.yaml")}
	kube := []string{"-n", ns}
	if b.Context != "" {
		helmArgs = append(helmArgs, "--kube-context", b.Context)
		kube = append(kube, "--context", b.Context)
	}
	if outDir != "" {
		fmt.Fprintf(out, "wrote %s (chart/ and values.yaml)\n", dir)
		if len(c.env) > 0 {
			fmt.Fprintf(out, "create the Secret %s-env with %s, then:\n", release, strings.Join(c.env, ", "))
		}
		fmt.Fprintf(out, "  helm %s\n", strings.Join(helmArgs, " "))
		if note != "" {
			fmt.Fprintln(out, note)
		}
		return nil
	}
	if len(c.env) > 0 {
		if err := applySecret(ctx, c, release+"-env", ns, kube, run, out); err != nil {
			return err
		}
	}
	cmd := exec.CommandContext(ctx, "helm", helmArgs...)
	cmd.Stdout, cmd.Stderr = out, os.Stderr
	fmt.Fprintf(out, "humagen: deploying %s to namespace %s with helm\n", release, ns)
	if err := run(cmd); err != nil {
		return fmt.Errorf("deploy: helm: %w", err)
	}
	if note != "" {
		fmt.Fprintln(out, "humagen: "+note)
	}
	return nil
}

// ownValues refuses a kubernetes block whose values set the chart's system
// or platforms. Those are the spec's documents, written from what carry and
// remotePersonRefs checked, and the block's values are merged over them
// after, so a reference set there, a person's start or one of the spec's
// own, would reach the target unread and unchecked.
func ownValues(b spec.Kubernetes) error {
	for _, k := range []string{"system", "platforms"} {
		if _, ok := b.Values[k]; ok {
			return fmt.Errorf("deploy: kubernetes values.%s: the chart's %s is written from the spec, whose references humagen deploy carries and checks, "+
				"and values are merged over it unchecked; set it in the spec's documents instead", k, k)
		}
	}
	return nil
}

// chartValues is the chart's values for a carried spec: the documents
// verbatim, the Secret their references read, and the ingress the block
// asks for, with the block's own values merged over them.
func chartValues(c carried, b spec.Kubernetes, release string) (map[string]any, error) {
	var docs struct {
		System    map[string]any   `json:"system"`
		Platforms []map[string]any `json:"platforms"`
	}
	raw, err := json.Marshal(c.cfg)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, err
	}
	values := map[string]any{"system": docs.System, "platforms": docs.Platforms}
	if docs.Platforms == nil {
		values["platforms"] = []any{}
	}
	if len(c.env) > 0 {
		values["existingSecret"] = release + "-env"
	}
	if b.Image.Repository != "" || b.Image.Tag != "" {
		img := map[string]any{}
		if b.Image.Repository != "" {
			img["repository"] = b.Image.Repository
		}
		if b.Image.Tag != "" {
			img["tag"] = b.Image.Tag
		}
		values["image"] = img
	}
	if b.Host != "" {
		ing := map[string]any{
			"enabled": true,
			"hosts":   []any{map[string]any{"host": b.Host, "paths": []any{map[string]any{"path": "/", "pathType": "Prefix"}}}},
		}
		if b.IngressClass != "" {
			ing["className"] = b.IngressClass
		}
		if b.TLSSecret != "" {
			ing["tls"] = []any{map[string]any{"secretName": b.TLSSecret, "hosts": []any{b.Host}}}
		}
		values["ingress"] = ing
	}
	return merge(values, b.Values), nil
}

// merge lays over onto base, recursing into maps.
func merge(base, over map[string]any) map[string]any {
	for k, v := range over {
		if bm, ok := base[k].(map[string]any); ok {
			if om, ok := v.(map[string]any); ok {
				base[k] = merge(bm, om)
				continue
			}
		}
		base[k] = v
	}
	return base
}

// applySecret creates or replaces the Secret the chart reads the references
// from, with values from this shell's environment and from carry. A variable
// the spec references but nobody set stops the deploy here, not at start.
// The manifest is built here and goes to kubectl on its standard input, so no
// value is on its command line.
func applySecret(ctx context.Context, c carried, name, ns string, kube []string, run func(*exec.Cmd) error, out io.Writer) error {
	data := map[string][]byte{}
	var missing []string
	for _, v := range c.env {
		val, ok := c.values[v]
		if !ok {
			val = legacy.Getenv(v)
		}
		if val == "" {
			missing = append(missing, v)
			continue
		}
		data[v] = []byte(val)
	}
	if len(missing) > 0 {
		return fmt.Errorf("deploy: the spec references %s, which this shell has not set", strings.Join(missing, ", "))
	}
	manifest, err := json.Marshal(kubeSecret{APIVersion: "v1", Kind: "Secret", Metadata: kubeMeta{Name: name, Namespace: ns}, Type: "Opaque", Data: data})
	if err != nil {
		return err
	}
	apply := exec.CommandContext(ctx, "kubectl", append([]string{"apply", "-f", "-"}, kube...)...)
	apply.Stdin, apply.Stdout, apply.Stderr = bytes.NewReader(manifest), out, os.Stderr
	if err := run(apply); err != nil {
		return fmt.Errorf("deploy: kubectl apply: %w", err)
	}
	return nil
}

// kubeSecret is a core/v1 Secret as kubectl apply reads it. encoding/json
// writes a []byte as standard base64, which is what the API expects in data.
type kubeSecret struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   kubeMeta          `json:"metadata"`
	Type       string            `json:"type"`
	Data       map[string][]byte `json:"data"`
}

type kubeMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

func mustSub(f fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// composeDir is where deploy writes a compose project beside a spec:
// .humagen/compose, which git ignores, or the old .humagen/compose when an
// earlier deploy wrote there. The project's identity is its name, not its
// directory, so either keeps its volumes.
func composeDir(base string, out io.Writer) string {
	old := filepath.Join(base, legacy.ComposeDir, "compose")
	if st, err := os.Stat(old); err == nil && st.IsDir() {
		fmt.Fprintf(out, "humagen: writing to %s, where an earlier deploy wrote; it still works\n", old)
		return old
	}
	return filepath.Join(base, ".humagen", "compose")
}

// interpolate is how generated compose passes a variable the spec names to
// the container: by its own name, from the deploying shell, where it may be
// set under either prefix (the new one wins, as legacy.Lookup reads it).
func interpolate(v string) string {
	if old := legacy.OldName(v); old != "" {
		return "${" + legacy.Canonical(v) + ":-${" + old + ":-}}"
	}
	return "${" + v + ":-}"
}

// helmRelease is a release as helm list -o json writes it.
type helmRelease struct {
	Name  string `json:"name"`
	Chart string `json:"chart"`
}

// installedByOldChart reports whether release was installed by the chart before
// the rename and so keeps its names: its chart is the warp chart, or, once
// upgraded to this one, it was given nameOverride: warp. scope is helm's
// namespace and context flags. No release is not an error.
func installedByOldChart(ctx context.Context, release string, scope []string, run func(*exec.Cmd) error) (bool, error) {
	var listed bytes.Buffer
	list := exec.CommandContext(ctx, "helm", append([]string{"list", "--filter", "^" + regexp.QuoteMeta(release) + "$", "-o", "json"}, scope...)...)
	list.Stdout, list.Stderr = &listed, os.Stderr
	if err := run(list); err != nil {
		return false, fmt.Errorf("deploy: helm list: %w", err)
	}
	var releases []helmRelease
	if b := bytes.TrimSpace(listed.Bytes()); len(b) > 0 {
		if err := json.Unmarshal(b, &releases); err != nil {
			return false, fmt.Errorf("deploy: helm list: %w", err)
		}
	}
	i := slices.IndexFunc(releases, func(r helmRelease) bool { return r.Name == release })
	if i < 0 {
		return false, nil
	}
	if strings.HasPrefix(releases[i].Chart, legacy.ChartName+"-") {
		return true, nil
	}
	var got bytes.Buffer
	get := exec.CommandContext(ctx, "helm", append([]string{"get", "values", release, "-o", "json"}, scope...)...)
	get.Stdout, get.Stderr = &got, os.Stderr
	if err := run(get); err != nil {
		return false, fmt.Errorf("deploy: helm get values: %w", err)
	}
	var v struct {
		NameOverride string `json:"nameOverride"`
	}
	if b := bytes.TrimSpace(got.Bytes()); len(b) > 0 && !bytes.Equal(b, []byte("null")) {
		if err := json.Unmarshal(b, &v); err != nil {
			return false, fmt.Errorf("deploy: helm get values: %w", err)
		}
	}
	return v.NameOverride == legacy.ChartName, nil
}
