package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urmzd/playscape/pkg/isolate"
	"github.com/urmzd/playscape/pkg/isolate/docker"
	"github.com/urmzd/playscape/pkg/isolate/local"

	"github.com/urmzd/humagen/fiber/pkg/isolation"
	poolleaser "github.com/urmzd/humagen/fiber/pkg/isolation/isolate"
	"github.com/urmzd/humagen/pkg/spec"
)

// sandboxFlags say where a run's tools execute (docs/reference/spec.md,
// sandbox).
type sandboxFlags struct {
	kind, runtime, image, connectorsImage, workRoot *string
	insecure                                        *bool
}

func addSandboxFlags(fs *flag.FlagSet, runtime string, insecure bool) sandboxFlags {
	if runtime == "" {
		runtime = spec.RuntimeGVisor
	}
	return sandboxFlags{
		kind:            fs.String("sandbox", "docker", `where a run's tools execute: "docker", an isolated instance per run; "local" or "unisolated", on this host, for development only`),
		runtime:         fs.String("sandbox-runtime", runtime, `with -sandbox=docker, the OCI runtime: "runsc" (gVisor) or "runc", which shares the host's kernel and is refused without -sandbox-insecure-development-runtime`),
		insecure:        fs.Bool("sandbox-insecure-development-runtime", insecure, `with -sandbox=docker, accept a runtime that shares the host's kernel, such as runc. For development machines without gVisor, running your own code`),
		image:           fs.String("sandbox-image", spec.DefaultSandboxImage, "with -sandbox=docker, the image a run's shell and files are in"),
		connectorsImage: fs.String("sandbox-connectors-image", "", "with -sandbox=docker, the image stdio MCP servers run in. Empty means -sandbox-image"),
		workRoot:        fs.String("sandbox-work-root", "", "with -sandbox=docker, the host directory runs' work directories are made in; the Docker daemon must be able to mount it. With -sandbox=local, where instances are made. Empty means the system temporary directory"),
	}
}

// openSandbox builds the sandbox the flags name, and what to close on
// shutdown. A docker sandbox that cannot reach its CLI does not stop the
// server: every run's tools fail closed with the reason instead, and the
// reason is logged now.
func openSandbox(ctx context.Context, o Options, l loaded, f sandboxFlags, log *slog.Logger) (isolation.Sandbox, func(), error) {
	none := func() {}
	switch kind := strings.TrimSpace(*f.kind); kind {
	case "unisolated":
		return isolation.Sandbox{Name: "unisolated"}, none, nil
	case "local":
		leaser, err := poolleaser.New(local.Driver{Dir: sandboxWorkRoot(*f.workRoot), Log: io.Discard}, poolleaser.Config{Image: "local"})
		if err != nil {
			return isolation.Sandbox{}, none, fmt.Errorf("sandbox: local: %w", err)
		}
		log.Warn("sandbox: local: a run's tools execute as host processes with this server's privileges. Use it for development only")
		return isolation.Sandbox{Leaser: leaser, Name: "local (not isolated)"}, closer(leaser), nil
	case "", "docker":
		runtime := strings.TrimSpace(*f.runtime)
		if runtime == "" {
			runtime = spec.RuntimeGVisor
		}
		name := "docker/" + runtime
		var cfg poolleaser.Config
		if l.cfg.System.Spec.Sandbox.Kind == "docker" {
			b, _ := spec.Block[spec.DockerSandbox](l.cfg.System.Spec.Sandbox)
			cfg.Pool = isolate.PoolConfig{Warm: b.Warm, Max: b.Max}
		}
		cfg.Image = *f.image
		if cfg.Image == "" {
			cfg.Image = spec.DefaultSandboxImage
		}
		cfg.ConnectorsImage = *f.connectorsImage
		cfg.Labels = map[string]string{"humagen.deployment": l.cfg.System.Metadata.Name}
		if cfg.Labels["humagen.deployment"] == "" {
			delete(cfg.Labels, "humagen.deployment")
		}
		cfg.Pool.OnError = func(err error) { log.Warn("sandbox: background work failed", "sandbox", name, "error", err) }
		d, err := docker.New(docker.Config{
			Runtime:                    runtime,
			InsecureDevelopmentRuntime: *f.insecure,
			Log:                        warnWriter{log: log, sandbox: name},
			WorkRoot:                   sandboxWorkRoot(*f.workRoot),
		})
		if err != nil {
			// Fail closed, not at start: the server answers, and every
			// tool that needs an instance says why it has none.
			log.Error("sandbox: docker is unavailable: tools that need a sandbox will refuse until it is", "error", err)
			return isolation.Sandbox{Leaser: unavailable{err: fmt.Errorf("sandbox %s is unavailable: %w", name, err)}, Name: name}, none, nil
		}
		leaser, err := poolleaser.New(d, cfg)
		if err != nil {
			return isolation.Sandbox{}, none, fmt.Errorf("sandbox: docker: %w", err)
		}
		return isolation.Sandbox{Leaser: leaser, Name: name}, closer(leaser), nil
	default:
		if l.cfg.System.Spec.Sandbox.Kind == kind {
			leaser, err := build[isolation.Leaser](ctx, o, spec.SlotSandbox, l.cfg.System.Spec.Sandbox)
			if err != nil {
				return isolation.Sandbox{}, none, err
			}
			return isolation.Sandbox{Leaser: leaser, Name: kind}, none, nil
		}
		return isolation.Sandbox{}, none, fmt.Errorf("sandbox: -sandbox=%q is not docker, local or unisolated", kind)
	}
}

// sandboxWorkRoot is where runs' work directories are made: dir, or one
// under the user cache directory, which Docker Desktop and Colima share with
// their VM (the system temporary directory on macOS they do not).
func sandboxWorkRoot(dir string) string {
	if dir != "" {
		return dir
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir = filepath.Join(cache, "humagen", "sandbox")
	if os.MkdirAll(dir, 0o700) != nil {
		return ""
	}
	return dir
}

func closer(l *poolleaser.Leaser) func() {
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = l.Close(ctx)
	}
}

// warnWriter logs what playscape writes, such as its warning that a
// runtime shares the host's kernel, as a warning.
type warnWriter struct {
	log     *slog.Logger
	sandbox string
}

func (w warnWriter) Write(p []byte) (int, error) {
	w.log.Warn("sandbox: "+strings.TrimSpace(string(p)), "sandbox", w.sandbox)
	return len(p), nil
}

// unavailable is a sandbox whose driver could not be built: every lease
// fails with why.
type unavailable struct{ err error }

func (u unavailable) Lease(context.Context, isolation.Request) (isolation.Instance, error) {
	return nil, u.err
}
