package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"

	"github.com/urmzd/humagen/internal/platform"
)

const migrateUsage = `usage: humagen migrate sessions [-state postgres] [-tenant NAME] [-apply]

Files past work into sessions once, so it is listed in Recent and Sessions
as work from now on is. It reads the stores -state names (postgres reads
$HUMAGEN_POSTGRES_URL) and writes through them, never with SQL.

sessions  files each GitHub run from before GitHub sessions in the session
          of its pull request or issue, the one a mention there opens now,
          private to the people the client maps the askers to. Conversations
          from before sessions are listed and left as they are.

It writes nothing unless -apply is given: without it, it prints what it
would do. A second -apply finds the first one's work and writes nothing.`

// migrateCmd runs humagen migrate.
func migrateCmd(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "-help" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(out, migrateUsage)
		return nil
	}
	if args[0] != "sessions" {
		return errors.New(migrateUsage)
	}
	return migrateSessions(context.Background(), args[1:], out, os.Getenv)
}

// migrateSessions is humagen migrate sessions.
func migrateSessions(ctx context.Context, args []string, out io.Writer, getenv func(string) string) error {
	fs := flag.NewFlagSet("migrate sessions", flag.ContinueOnError)
	fs.SetOutput(out)
	stateKind := addStateFlags(fs, getenv)
	tenant := fs.String("tenant", "", "the one tenant to migrate. Empty is every tenant the run index holds")
	apply := fs.Bool("apply", false, "write the changes. Without it nothing is written")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("migrate sessions: unexpected %q", fs.Arg(0))
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	backends, release, _, err := openState(ctx, stateKind, getenv, log)
	if err != nil {
		return err
	}
	defer release()
	// The platform is opened as a server opens it, and never run: no loop,
	// no connector and no turn starts. Attachments go to a directory of
	// its own, which nothing here writes to.
	dir, err := os.MkdirTemp("", "humagen-migrate-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	// What the platform says of itself on opening (its sandbox, say) is
	// not this command's business: only its errors are shown.
	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	p, err := platform.New(platform.Config{Backends: backends, Log: quiet, Workspace: dir})
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()
	return backfill(ctx, p, *tenant, *apply, out)
}

// backfill runs the sessions backfill over one tenant, or every tenant, and
// prints what it did or would do.
func backfill(ctx context.Context, p *platform.Platform, tenant string, apply bool, out io.Writer) error {
	tenants := []string{tenant}
	if tenant == "" {
		tenants = nil
		if err := p.Runs.EachTenant(func(t string) error { tenants = append(tenants, t); return nil }); err != nil {
			return err
		}
	}
	mode := "dry run: nothing is written; run again with -apply to write"
	if apply {
		mode = "applied"
	}
	fmt.Fprintf(out, "humagen migrate sessions (%s)\n", mode)
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
	fmt.Fprintf(out, "\ntenant %s\n", rep.Tenant)
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
