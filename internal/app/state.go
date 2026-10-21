package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/registry"

	"github.com/urmzd/warp/internal/platform"
)

// postgresURLEnv names the database everything is kept in with
// -state=postgres. It is read from the environment, never from a flag: the
// URL usually carries a password.
const postgresURLEnv = "WARP_POSTGRES_URL"

// stateFlags say where the platform keeps what must outlive the process.
type stateFlags struct {
	kind *string
}

func addStateFlags(fs *flag.FlagSet, getenv func(string) string) stateFlags {
	def := "memory"
	if strings.TrimSpace(getenv(postgresURLEnv)) != "" {
		def = "postgres"
	}
	return stateFlags{
		kind: fs.String("state", def, `where agents, runs, settings, clients and every other store are kept: "memory", lost on exit, or "postgres", the database $`+postgresURLEnv+` names. The default is postgres when that is set. One process per database`),
	}
}

// openState returns the backends the flags name and a function that lets
// them go, to be called once the platform has stopped. The zero Backends
// with no error means memory.
func openState(ctx context.Context, f stateFlags, getenv func(string) string, log *slog.Logger, providerNames ...string) (platform.Backends, func(), error) {
	switch strings.TrimSpace(*f.kind) {
	case "", "memory":
		log.Warn("state is kept in memory and lost on exit; set " + postgresURLEnv + " to keep it")
		return platform.Backends{}, func() {}, nil
	case "postgres":
	default:
		return platform.Backends{}, nil, fmt.Errorf("state: -state=%q is not memory or postgres", *f.kind)
	}
	url := strings.TrimSpace(getenv(postgresURLEnv))
	if url == "" {
		return platform.Backends{}, nil, fmt.Errorf("state: -state=postgres needs $%s", postgresURLEnv)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		// The error could quote the URL, password and all.
		return platform.Backends{}, nil, fmt.Errorf("state: $%s is not a Postgres URL", postgresURLEnv)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return platform.Backends{}, nil, fmt.Errorf("state: postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return platform.Backends{}, nil, fmt.Errorf("state: postgres: %w", err)
	}
	b, release, err := platform.OpenPostgres(ctx, pool, providerNames...)
	if err != nil {
		pool.Close()
		return platform.Backends{}, nil, fmt.Errorf("state: postgres: %w", err)
	}
	log.Info("state is kept in postgres", "host", cfg.ConnConfig.Host, "database", cfg.ConnConfig.Database)
	return b, func() {
		if err := release(); err != nil {
			log.Warn("state: postgres: release", "error", err)
		}
		pool.Close()
	}, nil
}

// registerSpec puts an agent spec from a file in the registry: a new agent,
// or the next revision of one a kept registry already holds. A spec that has
// not changed adds no revision.
func registerSpec(ctx context.Context, agents registry.Store, tenant string, spec agentspec.AgentSpec) error {
	_, err := agents.Create(ctx, tenant, spec, "startup")
	if !errors.Is(err, registry.ErrExists) {
		return err
	}
	head, err := agents.Get(ctx, tenant, spec.Name, 0)
	if err != nil {
		return err
	}
	_, err = agents.Revise(ctx, tenant, spec, head.Rev, "startup")
	return err
}
