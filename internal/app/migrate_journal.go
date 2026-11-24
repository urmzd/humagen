package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire/sessionstate"
	"github.com/urmzd/humagen/pkg/runs"
)

// journalTarget is what humagen migrate journal reads besides its store.
type journalTarget struct {
	// tenant is the one tenant to migrate, "" for every one.
	tenant string
	// replace sets a request not matched to its run to
	// sessionstate.UnkeptRequest instead of leaving it.
	replace           bool
	apply, allowEmpty bool
}

// migrateJournal is humagen migrate journal.
func migrateJournal(ctx context.Context, args []string, out io.Writer, getenv func(string) string) error {
	fs := flag.NewFlagSet("migrate journal", flag.ContinueOnError)
	fs.SetOutput(out)
	stateKind := addStateFlags(fs, getenv)
	tenant := fs.String("tenant", "", "the one tenant to migrate. Empty is every tenant the run index holds")
	apply := fs.Bool("apply", false, "write the changes. Without it nothing is written")
	unmatched := fs.String("unmatched", "leave", `what to do with a request not matched to its run: "leave" lists it and leaves it, "replace" sets it to "`+sessionstate.UnkeptRequest+`"`)
	allowEmpty := fs.Bool("allow-empty-store", false, "for tests only: run against the in-memory store, or a store that holds no runs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("migrate journal: unexpected %q", fs.Arg(0))
	}
	if *unmatched != "leave" && *unmatched != "replace" {
		return fmt.Errorf("migrate journal: -unmatched is leave or replace, not %q", *unmatched)
	}
	kind := strings.TrimSpace(*stateKind.kind)
	if (kind == "" || kind == "memory") && !*allowEmpty {
		return fmt.Errorf("%w: -state is memory, which holds nothing to migrate; set $%s and -state postgres to read the database", errNothingToMigrate, postgresURLEnv)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	backends, release, _, err := openState(ctx, stateKind, getenv, log)
	if err != nil {
		return err
	}
	defer release()
	return journalWith(ctx, backends, storeName(kind, getenv), journalTarget{tenant: *tenant, replace: *unmatched == "replace", apply: *apply, allowEmpty: *allowEmpty}, out)
}

// journalWith opens the platform on backends, as a server opens it but never
// runs it (no loop, connector or turn starts), and rewrites each tenant's
// journaled requests.
func journalWith(ctx context.Context, backends platform.Backends, store string, at journalTarget, out io.Writer) error {
	dir, err := os.MkdirTemp("", "humagen-migrate-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	p, err := platform.New(platform.Config{Backends: backends, Log: quiet, Workspace: dir})
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()
	tenants, err := migrationTenants(p, store, at.tenant, at.allowEmpty)
	if err != nil {
		return err
	}
	mode := "dry run: nothing is written; run again with -apply to write"
	if at.apply {
		mode = "applied"
	}
	unmatched := "leave"
	if at.replace {
		unmatched = "replace with " + sessionstate.UnkeptRequest
	}
	fmt.Fprintf(out, "humagen migrate journal (%s)\nstore: %s\nunmatched requests: %s\ntenants: %s\n", mode, store, unmatched, strings.Join(tenants, ", "))
	total := 0
	for _, t := range tenants {
		rep, err := p.MigrateJournalRequests(ctx, t, at.apply, at.replace)
		if err != nil {
			return fmt.Errorf("tenant %s: %w", t, err)
		}
		printJournalReport(out, rep)
		total += rep.Changes()
	}
	verb := "would write"
	if at.apply {
		verb = "wrote"
	}
	fmt.Fprintf(out, "\n%s %d changes in %d tenants\n", verb, total, len(tenants))
	return nil
}

// migrationTenants are the tenants the run index holds, or the one named
// when it holds runs of it. A store, or a tenant, that holds no runs is
// refused unless allowEmpty.
func migrationTenants(p *platform.Platform, store, tenant string, allowEmpty bool) ([]string, error) {
	var tenants []string
	if err := p.Runs.EachTenant(func(t string) error { tenants = append(tenants, t); return nil }); err != nil {
		return nil, err
	}
	if tenant != "" {
		held := false
		if err := p.Runs.Each(tenant, func(runs.Run) bool { held = true; return false }); err != nil {
			return nil, err
		}
		tenants = nil
		if held {
			tenants = []string{tenant}
		}
	}
	if len(tenants) == 0 && !allowEmpty {
		where := "the store"
		if tenant != "" {
			where = "tenant " + tenant
		}
		return nil, fmt.Errorf("%w: %s on %s holds no runs; check $%s and -tenant", errNothingToMigrate, where, store, postgresURLEnv)
	}
	return tenants, nil
}

// printJournalReport prints one tenant's report: a line for each entry
// changed or left and each thread left, by record key and run, then the
// counts. It never prints an entry's text.
func printJournalReport(out io.Writer, rep sessionstate.RequestReport) {
	fmt.Fprintf(out, "\ntenant %s: %d requests read\n", rep.Tenant, rep.Requests)
	for _, l := range rep.Lines {
		line := "  " + l.Kind + ": " + l.Key
		if l.Run != "" {
			line += " run " + l.Run
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
		fmt.Fprintln(out, "    no requests")
	}
	for _, k := range kinds {
		fmt.Fprintf(out, "    %-34s %d\n", k, rep.Counts[k])
	}
}
