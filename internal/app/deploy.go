package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/urmzd/warp/pkg/credref"
	"github.com/urmzd/warp/pkg/spec"
)

// deployCmd is `warp deploy`: run the spec where its System document's
// deploy slot says. local is `warp start` here; compose writes a Docker
// Compose project and brings it up; kubernetes installs the chart warp
// carries with Helm. -o writes what would be applied and stops.
//
// A secret never lands in a written file. An env: reference stays one and is
// passed from this process's environment; a keychain: or op:// reference is
// read here and handed to the target as an environment variable it then
// references, because the target cannot reach this machine's keychain.
func deployCmd(o Options, args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	var paths files
	fs.Var(&paths, "f", "a spec file or directory; repeatable. Empty means warp.yaml in the config directory")
	target := fs.String("target", "", "local, compose or kubernetes, over the spec's deploy.kind")
	outDir := fs.String("o", "", "write the deployment to this directory and stop, applying nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(paths) == 0 {
		if def := defaultSpec(os.Getenv); def != "" {
			paths = append(paths, def)
		}
	}
	var cfg spec.Config
	if len(paths) > 0 {
		var err error
		if cfg, err = spec.Load(paths...); err != nil {
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
			return errors.New("deploy: local has nothing to write; it is warp start")
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
		return deployCompose(ctx, cfg, b, paths, *outDir, credref.Default, out)
	case "kubernetes":
		b, err := spec.Block[spec.Kubernetes](d)
		if err != nil {
			return err
		}
		return deployKubernetes(ctx, cfg, b, paths, *outDir, credref.Default, out)
	default:
		return fmt.Errorf("deploy: kind %q is not local, compose or kubernetes", d.Kind)
	}
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
					name = fmt.Sprintf("WARP_REF_%d", len(seen)+1)
					seen[ref], c.values[name] = name, val
					c.env = append(c.env, name)
				}
				t["ref"] = "env:" + name
				return nil
			}
			for _, child := range t {
				if err := walk(child); err != nil {
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

func deployCompose(ctx context.Context, cfg spec.Config, b spec.Compose, paths []string, outDir string, keys credref.Resolver, out io.Writer) error {
	name := cfg.System.Metadata.Name
	dir := b.Dir
	if outDir != "" {
		dir = outDir
	}
	if dir == "" {
		base := "."
		if len(paths) > 0 {
			base = filepath.Dir(paths[0])
		}
		dir = filepath.Join(base, "deploy", "compose")
	}
	port := b.Port
	if port == 0 {
		_, p, _ := net.SplitHostPort(cfg.System.Spec.Server.Addr)
		port, _ = strconv.Atoi(p)
	}
	if port == 0 {
		port = 8080
	}
	// Postgres beside warp: the stores point at it, by a variable the
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

	warp := map[string]any{
		"restart": "unless-stopped",
		"command": []string{"start", "-f", "/etc/warp/warp.yaml", "-addr", addr, "-web", "/usr/share/warp/web"},
		"volumes": []string{"warp-data:/var/lib/warp", "./warp.yaml:/etc/warp/warp.yaml:ro"},
	}
	switch {
	case b.Image != "":
		warp["image"] = b.Image
	default:
		ctxDir := b.Build
		if ctxDir == "" {
			wd, _ := os.Getwd()
			ctxDir = wd
		}
		if _, err := os.Stat(filepath.Join(ctxDir, "Dockerfile")); err != nil {
			return fmt.Errorf("deploy: compose needs an image, or build pointing at a warp checkout (no Dockerfile in %s)", ctxDir)
		}
		abs, _ := filepath.Abs(ctxDir)
		warp["build"] = map[string]any{"context": abs}
		warp["image"] = name + ":local"
	}
	env := map[string]string{}
	for _, v := range c.env {
		env[v] = "${" + v + ":-}"
	}
	services := map[string]any{"warp": warp}
	if hostNet {
		warp["network_mode"] = "host"
	} else {
		warp["ports"] = []string{fmt.Sprintf("%d:8080", port)}
	}
	volumes := map[string]any{"warp-data": map[string]any{}}
	if b.Postgres {
		host := "postgres:5432"
		pg := map[string]any{
			"image":       "postgres:17-alpine",
			"restart":     "unless-stopped",
			"environment": map[string]string{"POSTGRES_USER": "warp", "POSTGRES_PASSWORD": "${POSTGRES_PASSWORD:-warp}", "POSTGRES_DB": "warp"},
			"volumes":     []string{"postgres-data:/var/lib/postgresql/data"},
			"healthcheck": map[string]any{"test": []string{"CMD-SHELL", "pg_isready -U warp -d warp"}, "interval": "5s", "retries": 20},
		}
		if hostNet {
			pg["ports"] = []string{"127.0.0.1:55432:5432"}
			host = "127.0.0.1:55432"
		}
		services["postgres"] = pg
		volumes["postgres-data"] = map[string]any{}
		env[envPostgres] = "postgres://warp:${POSTGRES_PASSWORD:-warp}@" + host + "/warp?sslmode=disable"
		warp["depends_on"] = map[string]any{"postgres": map[string]string{"condition": "service_healthy"}}
	}
	warp["environment"] = env
	project, err := yaml.Marshal(map[string]any{"name": name, "services": services, "volumes": volumes})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	header := "# Written by `warp deploy` from the spec. Edit the spec and deploy again;\n# secrets are passed from the environment and are not in this file.\n"
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), append([]byte(header), project...), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "warp.yaml"), doc, 0o600); err != nil {
		return err
	}
	if outDir != "" {
		fmt.Fprintf(out, "wrote %s; run it with: docker compose --project-directory %s up -d\n", dir, dir)
		if len(c.env) > 0 {
			fmt.Fprintf(out, "it reads %s from the environment\n", strings.Join(c.env, ", "))
		}
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "--project-directory", dir, "up", "-d", "--build")
	cmd.Env, cmd.Stdout, cmd.Stderr = c.environ(), out, os.Stderr
	fmt.Fprintf(out, "warp: deploying %s with docker compose from %s\n", name, dir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("deploy: docker compose: %w", err)
	}
	fmt.Fprintf(out, "warp: %s is up at http://127.0.0.1:%d\n", name, port)
	return nil
}

func deployKubernetes(ctx context.Context, cfg spec.Config, b spec.Kubernetes, paths []string, outDir string, keys credref.Resolver, out io.Writer) error {
	return errors.New("deploy: kubernetes is not built yet")
}
