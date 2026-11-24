package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire/sessionstate"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/state"
)

const migrateUsage = `usage: humagen migrate sessions -state postgres [-tenant NAME] [-server-tenant NAME] [-apply]
       humagen migrate journal -state postgres [-tenant NAME] [-unmatched leave|replace] [-apply]

Each migration runs once, after the release that needs it. It reads the
stores -state names (postgres reads $HUMAGEN_POSTGRES_URL) and writes
through them, never with SQL. It writes nothing unless -apply is given:
without it, it prints what it would do. A second -apply finds the first
one's work and writes nothing. It refuses the in-memory store, and a store
that holds no runs, which have nothing to migrate: a dry run there would
report nothing to do.

sessions  files past work into sessions, so it is listed in Recent and
          Sessions as work from now on is. It files each GitHub run from
          before GitHub sessions in the session of its pull request or
          issue, the one a mention there opens now, private to the people
          the client maps the askers to, and dated from its runs: started
          when the first was asked, last active when the latest run or
          message was. A session found, whose record was created after the
          first run filed in it had ended, is dated from its runs too,
          archived or not. A session whose first run has not ended is left:
          when that run is not the mention that opened it, it is listed as
          not dated. A deleted session is left. A conversation from before
          sessions that names one agent becomes that agent's session under
          the id it has, read by the people who read it before; any other
          is listed and left as it is. The clients the server's environment
          configures are built from this process's environment, as serve
          builds them, in -server-tenant: run it where the server's
          environment is.

journal   rewrites each journaled request to the request as it was asked,
          as every turn journals it now. A request journaled with the
          passages its turn retrieved before it is set to the request as it
          was asked; one already as it was asked is left. A request whose
          run the ledger does not hold, or whose text is neither, is listed
          and left, or with -unmatched replace set to "` + sessionstate.UnkeptRequest + `".
          Answers are left. A thread that holds a run that has not ended is
          listed and left: run the command again once it has ended. It
          reads the runs that have not ended once, and with -apply again
          just before it writes each entry, and leaves the rest of a thread
          where a turn started meanwhile. A run that has not ended and
          cannot be read leaves its tenant, or every tenant when the run
          index holds it in none. Apply it once every server runs this
          release. It reads one tenant at a time, a page at a time, and
          writes every record of an entry together, at the version it read.
          It prints counts and record keys, never the text of an entry.`

// migrateCmd runs humagen migrate.
func migrateCmd(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "-help" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(out, migrateUsage)
		return nil
	}
	switch args[0] {
	case "sessions":
		return migrateSessions(context.Background(), args[1:], out, os.Getenv)
	case "journal":
		return migrateJournal(context.Background(), args[1:], out, os.Getenv)
	}
	fmt.Fprintln(out, migrateUsage)
	return fmt.Errorf("unknown migration %q", args[0])
}

// errNothingToMigrate refuses a store that cannot hold what a migration is
// for. Every migration wraps it, after its own name where it has one.
var errNothingToMigrate = errors.New("nothing to migrate")

// migrateSessions is humagen migrate sessions.
func migrateSessions(ctx context.Context, args []string, out io.Writer, getenv func(string) string) error {
	fs := flag.NewFlagSet("migrate sessions", flag.ContinueOnError)
	fs.SetOutput(out)
	stateKind := addStateFlags(fs, getenv)
	tenant := fs.String("tenant", "", "the one tenant to migrate. Empty is every tenant the run index holds")
	apply := fs.Bool("apply", false, "write the changes. Without it nothing is written")
	serverTenant := fs.String("server-tenant", "default", "the tenant the server's -tenant names: the clients the environment configures ($HUMAGEN_GITHUB_*, $HUMAGEN_SLACK_*) are built in it, as serve builds them, since they are never stored")
	allowEmpty := fs.Bool("allow-empty-store", false, "for tests only: run against the in-memory store, or a store that holds no runs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("migrate sessions: unexpected %q", fs.Arg(0))
	}
	kind := strings.TrimSpace(*stateKind.kind)
	if (kind == "" || kind == "memory") && !*allowEmpty {
		return fmt.Errorf("migrate sessions: %w: -state is memory, which starts empty; set $%s and -state postgres to read the database", errNothingToMigrate, postgresURLEnv)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	backends, release, _, err := openState(ctx, stateKind, getenv, log)
	if err != nil {
		return err
	}
	defer release()
	return migrateWith(ctx, backends, storeName(kind, getenv), migrateTarget{tenant: *tenant, serverTenant: *serverTenant, getenv: getenv}, *apply, *allowEmpty, out)
}

// storeName says which store -state reads, without its credentials.
func storeName(kind string, getenv func(string) string) string {
	if kind != "postgres" {
		return "memory (lost on exit)"
	}
	u, err := url.Parse(strings.TrimSpace(getenv(postgresURLEnv)))
	if err != nil || u.Host == "" {
		return "postgres"
	}
	return "postgres " + u.Host + u.Path
}

// migrateTarget is what a migration reads besides its store: the tenant to
// migrate ("" for every one), the tenant the server's environment clients
// belong to, and the environment they are built from.
type migrateTarget struct {
	tenant, serverTenant string
	getenv               func(string) string
}

// migrateWith opens the platform on backends, as a server opens it but
// never runs it (no loop, connector or turn starts), builds the clients the
// environment configures as serve does (seedClients), and backfills.
func migrateWith(ctx context.Context, backends platform.Backends, store string, at migrateTarget, apply, allowEmpty bool, out io.Writer) error {
	// Attachments go to a directory of their own, which nothing here
	// writes to.
	dir, err := os.MkdirTemp("", "humagen-migrate-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	// What the platform says of itself on opening (its sandbox, say) is
	// not this command's business: only its errors are shown.
	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	// The environment's clients name their secrets by reference, as serve
	// lets its own tenant's do. Creating one keeps a marker of each secret;
	// those go to a store of this process's, so a dry run writes nothing.
	refs, err := secretRefs("", at.serverTenant, at.getenv)
	if err != nil {
		return err
	}
	secrets, err := clients.OpenSecrets(state.NewMemory())
	if err != nil {
		return err
	}
	env := func(name string) (string, bool) {
		v := at.getenv(name)
		return v, v != ""
	}
	p, err := platform.New(platform.Config{Backends: backends, Log: quiet, Workspace: dir, Secrets: secrets, Env: env, SecretRefs: refs})
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()
	// A client the environment configures is never stored: every server
	// builds it again at start, and so does this.
	if err := seedClients(ctx, p, at.serverTenant, at.getenv); err != nil {
		return err
	}
	note := "none"
	if seeded, err := defaultClients(at.getenv); err == nil && len(seeded) > 0 {
		ids := make([]string, len(seeded))
		for i, in := range seeded {
			ids[i] = in.ID
		}
		note = strings.Join(ids, ", ") + " in tenant " + at.serverTenant
	}
	return backfill(ctx, p, store, note, at.tenant, apply, allowEmpty, out)
}

// backfill runs the sessions backfill over one tenant, or every tenant, and
// prints what it did or would do. A store, or a tenant, that holds no runs
// is refused unless allowEmpty (migrationTenants).
func backfill(ctx context.Context, p *platform.Platform, store, envClients, tenant string, apply, allowEmpty bool, out io.Writer) error {
	tenants, err := migrationTenants(p, store, tenant, allowEmpty)
	if err != nil {
		return err
	}
	mode := "dry run: nothing is written; run again with -apply to write"
	if apply {
		mode = "applied"
	}
	fmt.Fprintf(out, "humagen migrate sessions (%s)\nstore: %s\nenvironment clients: %s\ntenants: %s\n", mode, store, envClients, strings.Join(tenants, ", "))
	total := 0
	for _, t := range tenants {
		rep, err := p.BackfillSessions(ctx, t, apply)
		if err != nil {
			return fmt.Errorf("tenant %s: %w", t, err)
		}
		printBackfill(out, rep)
		total += rep.Changes()
	}
	verb := "would write"
	if apply {
		verb = "wrote"
	}
	fmt.Fprintf(out, "\n%s %d changes in %d tenants\n", verb, total, len(tenants))
	return nil
}

func printBackfill(out io.Writer, rep platform.BackfillReport) {
	fmt.Fprintf(out, "\ntenant %s: %d runs read\n", rep.Tenant, rep.Runs)
	for _, w := range rep.Warnings {
		fmt.Fprintln(out, "  WARNING: "+w)
	}
	for _, l := range rep.Lines {
		line := "  " + l.Kind + ": " + l.Subject
		if l.Session != "" {
			line += " -> " + l.Session
		}
		if l.Detail != "" {
			line += " (" + l.Detail + ")"
		}
		fmt.Fprintln(out, line)
	}
	kinds := make([]string, 0, len(rep.Counts))
	for k := range rep.Counts {
		kinds = append(kinds, k)
	}
	slices.Sort(kinds)
	fmt.Fprintln(out, "  counts:")
	if len(kinds) == 0 {
		fmt.Fprintln(out, "    nothing to file")
	}
	for _, k := range kinds {
		fmt.Fprintf(out, "    %-34s %d\n", k, rep.Counts[k])
	}
}
