package legacy_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/urmzd/humagen/fiber/pkg/definition"
	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/agents"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

var t0 = time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC)

func oldSpec(t *testing.T, name, prompt string, extra map[string]any) json.RawMessage {
	t.Helper()
	m := map[string]any{"name": name, "provider": "anthropic", "model": "claude-sonnet-4-5", "system_prompt": prompt}
	for k, v := range extra {
		m[k] = v
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func oldRows(t *testing.T) legacy.OldAgentRows {
	return legacy.OldAgentRows{
		{Tenant: "acme", Name: "docs-bot", Revisions: []legacy.OldRevision{
			// Out of order on purpose: the move sorts by revision.
			{Rev: 2, Spec: oldSpec(t, "docs-bot", "Answer well.", map[string]any{"tools": []string{"mcp:github:write", "web_search"}, "tagged_only": true}), CreatedAt: t0.Add(time.Hour), CreatedBy: "grace"},
			{Rev: 1, Spec: oldSpec(t, "docs-bot", "Answer.", nil), CreatedAt: t0, CreatedBy: "ada"},
		}},
		{Tenant: "acme", Name: "old-bot", Deleted: true, Revisions: []legacy.OldRevision{
			{Rev: 1, Spec: oldSpec(t, "old-bot", "Gone.", nil), CreatedAt: t0, CreatedBy: "ada"},
		}},
		{Tenant: "globex", Name: "docs-bot", Revisions: []legacy.OldRevision{
			{Rev: 1, Spec: oldSpec(t, "docs-bot", "Other tenant.", nil), CreatedAt: t0, CreatedBy: "eve"},
		}},
	}
}

func convert(t *testing.T, st state.Store, rows legacy.OldAgents, opts legacy.ConvertOptions) (*agents.Store, legacy.AgentConversion) {
	t.Helper()
	dst, err := agents.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return t0.Add(24 * time.Hour) }
	}
	report, err := legacy.ConvertAgents(context.Background(), st, dst, rows, opts)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	return dst, report
}

func dump(t *testing.T, s *agents.Store) []definition.Record {
	t.Helper()
	ctx := context.Background()
	var out []definition.Record
	for _, tenant := range []string{"acme", "globex"} {
		for _, name := range []string{"docs-bot", "old-bot"} {
			all, err := s.Versions(ctx, tenant, name)
			if errors.Is(err, definition.ErrNotFound) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, all...)
		}
	}
	return out
}

func TestConvertIsDeterministic(t *testing.T) {
	ctx := context.Background()
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		first, report := convert(t, open(t), oldRows(t), legacy.ConvertOptions{})
		second, _ := convert(t, open(t), oldRows(t), legacy.ConvertOptions{})
		a, b := dump(t, first), dump(t, second)
		if len(a) != 4 || !reflect.DeepEqual(a, b) {
			t.Fatalf("two moves differ:\n%+v\n%+v", a, b)
		}
		if report.Converted != 3 || report.Versions != 4 || len(report.Disabled) != 0 {
			t.Fatalf("report = %+v", report)
		}

		v2, err := first.Get(ctx, "acme", "docs-bot", "2.0.0")
		if err != nil {
			t.Fatal(err)
		}
		if !v2.CreatedAt.Equal(t0.Add(time.Hour)) || v2.CreatedBy != "grace" {
			t.Errorf("2.0.0 kept %s by %q", v2.CreatedAt, v2.CreatedBy)
		}
		if !v2.Policy.TaggedOnly || !v2.Policy.MayWrite("github") || !strings.Contains(v2.Content, "Answer well.") {
			t.Errorf("2.0.0 = %+v", v2)
		}
		v1, _ := first.Get(ctx, "acme", "docs-bot", "1.0.0")
		if !v1.CreatedAt.Equal(t0) || v1.CreatedBy != "ada" {
			t.Errorf("1.0.0 kept %s by %q", v1.CreatedAt, v1.CreatedBy)
		}
		if _, err := first.Latest(ctx, "acme", "old-bot"); !errors.Is(err, definition.ErrRetired) {
			t.Errorf("a deleted agent moved live: %v", err)
		}
		if g, err := first.Get(ctx, "globex", "docs-bot", "1.0.0"); err != nil || !strings.Contains(g.Content, "Other tenant.") {
			t.Errorf("globex docs-bot = %+v, %v", g, err)
		}
	})
}

func TestConvertRunsOnce(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemory()
	_, report := convert(t, st, oldRows(t), legacy.ConvertOptions{})
	called := false
	again, err := agents.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	got, err := legacy.ConvertAgents(ctx, st, again, loadFunc(func() { called = true }), legacy.ConvertOptions{})
	if err != nil || called || !reflect.DeepEqual(got, report) {
		t.Fatalf("second move = %+v, %v, read old agents: %v", got, err, called)
	}
	kept, ok, err := legacy.AgentConversionOf(ctx, st)
	if err != nil || !ok || !reflect.DeepEqual(kept, report) {
		t.Fatalf("kept report = %+v, %v, %v", kept, ok, err)
	}
}

type loadFunc func()

func (f loadFunc) LoadOldAgents(context.Context) ([]legacy.OldAgent, error) { f(); return nil, nil }

func TestConvertDisablesBadNames(t *testing.T) {
	ctx := context.Background()
	rows := append(oldRows(t),
		legacy.OldAgent{Tenant: "acme", Name: "docs_bot", Revisions: []legacy.OldRevision{
			{Rev: 1, Spec: oldSpec(t, "docs_bot", "Bad name.", nil), CreatedAt: t0, CreatedBy: "ada"},
		}},
		legacy.OldAgent{Tenant: "acme", Name: "lead", Revisions: []legacy.OldRevision{
			{Rev: 1, Spec: oldSpec(t, "lead", "Calls a bad name.", map[string]any{"access": map[string]any{"spawn": []string{"Helper.Bot"}}}), CreatedAt: t0, CreatedBy: "ada"},
		}},
	)
	var logged []string
	st := state.NewMemory()
	dst, report := convert(t, st, rows, legacy.ConvertOptions{Logf: func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }})
	if report.Converted != 3 || len(report.Disabled) != 2 {
		t.Fatalf("report = %+v", report)
	}
	for i, name := range []string{"docs_bot", "lead"} {
		d := report.Disabled[i]
		if d.Tenant != "acme" || d.Name != name || !strings.Contains(d.Reason, "name rule") {
			t.Errorf("disabled[%d] = %+v", i, d)
		}
		if _, err := dst.Versions(ctx, "acme", name); !errors.Is(err, definition.ErrNotFound) {
			t.Errorf("%s was moved: %v", name, err)
		}
	}
	if len(logged) != 2 || !strings.Contains(logged[0], "docs_bot") {
		t.Errorf("logged %q", logged)
	}
	// The setup status reads the report on every later start.
	kept, ok, err := legacy.AgentConversionOf(ctx, st)
	if err != nil || !ok || len(kept.Disabled) != 2 {
		t.Errorf("kept report = %+v, %v, %v", kept, ok, err)
	}
}

func TestBuiltinBadNameFailsStartup(t *testing.T) {
	ctx := context.Background()
	rows := append(oldRows(t), legacy.OldAgent{Tenant: "acme", Name: "Platform_Writer", Revisions: []legacy.OldRevision{
		{Rev: 1, Spec: oldSpec(t, "Platform_Writer", "Write.", nil), CreatedAt: t0, CreatedBy: "system"},
	}})
	st := state.NewMemory()
	dst, err := agents.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	builtin := func(name string) bool { return name == "Platform_Writer" }
	_, err = legacy.ConvertAgents(ctx, st, dst, rows, legacy.ConvertOptions{Builtin: builtin})
	if err == nil || !strings.Contains(err.Error(), "Platform_Writer") || !errors.Is(err, definition.ErrInvalid) {
		t.Fatalf("convert = %v, want the builtin's error", err)
	}
	if _, ok, _ := legacy.AgentConversionOf(ctx, st); ok {
		t.Fatal("a failed move was marked done")
	}
}

func TestPostgresOldAgents(t *testing.T) {
	ctx := context.Background()
	pool := statetest.Pool(t)
	empty, err := legacy.PostgresOldAgents{Pool: pool}.LoadOldAgents(ctx)
	if err != nil || len(empty) != 0 {
		t.Fatalf("no tables = %v, %v", empty, err)
	}
	createOldTables(t, pool, "fiber_")
	got, err := legacy.PostgresOldAgents{Pool: pool}.LoadOldAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Name != "docs-bot" || len(got[0].Revisions) != 2 || got[0].Revisions[0].Rev != 1 || !got[1].Deleted || got[2].Tenant != "globex" {
		t.Fatalf("old agents = %+v", got)
	}
	st := state.NewMemory()
	fromPG, _ := convert(t, st, legacy.PostgresOldAgents{Pool: pool}, legacy.ConvertOptions{})
	fromRows, _ := convert(t, state.NewMemory(), oldRows(t), legacy.ConvertOptions{})
	if !reflect.DeepEqual(dump(t, fromPG), dump(t, fromRows)) {
		t.Fatal("the move from Postgres differs from the move from rows")
	}
}

// A database still carrying the prefix from before the rename moves its
// agents too, and the tables are renamed on the way.
func TestPostgresOldAgentsUnderTheOldPrefix(t *testing.T) {
	ctx := context.Background()
	pool := statetest.Pool(t)
	createOldTables(t, pool, legacy.FiberPrefixBeforeRename)
	st := state.NewMemory()
	fromPG, report := convert(t, st, legacy.PostgresOldAgents{Pool: pool}, legacy.ConvertOptions{})
	if report.Converted != 3 {
		t.Fatalf("report = %+v", report)
	}
	fromRows, _ := convert(t, state.NewMemory(), oldRows(t), legacy.ConvertOptions{})
	if !reflect.DeepEqual(dump(t, fromPG), dump(t, fromRows)) {
		t.Fatal("the move from the old prefix differs from the move from rows")
	}
	var renamed bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('fiber_agents') IS NOT NULL AND to_regclass('fiber_agent_revisions') IS NOT NULL`).Scan(&renamed); err != nil || !renamed {
		t.Fatalf("tables renamed = %v, %v", renamed, err)
	}
}

// createOldTables writes oldRows into fiber's registry schema of v0.25, its
// tables named with prefix.
func createOldTables(t *testing.T, pool *pgxpool.Pool, prefix string) {
	t.Helper()
	ctx := context.Background()
	agentsT, revsT := pgx.Identifier{prefix + "agents"}.Sanitize(), pgx.Identifier{prefix + "agent_revisions"}.Sanitize()
	if _, err := pool.Exec(ctx, `
CREATE TABLE `+agentsT+` (tenant text NOT NULL, name text COLLATE "C" NOT NULL, head integer NOT NULL, deleted boolean NOT NULL, PRIMARY KEY (tenant, name));
CREATE TABLE `+revsT+` (tenant text NOT NULL, name text COLLATE "C" NOT NULL, rev integer NOT NULL, hash text NOT NULL, spec jsonb NOT NULL, created_at timestamptz NOT NULL, created_by text NOT NULL, PRIMARY KEY (tenant, name, rev));`); err != nil {
		t.Fatal(err)
	}
	for _, a := range oldRows(t) {
		if _, err := pool.Exec(ctx, `INSERT INTO `+agentsT+` VALUES ($1, $2, $3, $4)`, a.Tenant, a.Name, len(a.Revisions), a.Deleted); err != nil {
			t.Fatal(err)
		}
		for _, r := range a.Revisions {
			if _, err := pool.Exec(ctx, `INSERT INTO `+revsT+` VALUES ($1, $2, $3, 'h', $4, $5, $6)`, a.Tenant, a.Name, r.Rev, string(r.Spec), r.CreatedAt, r.CreatedBy); err != nil {
				t.Fatal(err)
			}
		}
	}
}
