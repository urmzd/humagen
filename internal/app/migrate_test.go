package app

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
)

// The command refuses the in-memory store, and a store with no runs: a dry
// run there reports nothing to do whatever production holds.
func TestMigrateSessionsRefusesAStoreWithNothingInIt(t *testing.T) {
	var out bytes.Buffer
	noURL := func(string) string { return "" }
	if err := migrateSessions(t.Context(), nil, &out, noURL); !errors.Is(err, errNothingToMigrate) || !strings.HasPrefix(err.Error(), "migrate sessions: nothing to migrate: ") {
		t.Fatalf("the memory store was read: %v\n%s", err, out.String())
	}
	if err := migrateSessions(t.Context(), []string{"-apply"}, &out, noURL); !errors.Is(err, errNothingToMigrate) {
		t.Fatalf("the memory store was written: %v", err)
	}
	empty := platform.Backends{State: state.NewMemory()}
	if err := migrateWith(t.Context(), empty, "postgres db/humagen", migrateTarget{serverTenant: "default", getenv: noEnv}, false, false, &out); !errors.Is(err, errNothingToMigrate) || !strings.Contains(err.Error(), "postgres db/humagen holds no runs") {
		t.Fatalf("a store with no runs was read: %v", err)
	}
	out.Reset()
	if err := migrateSessions(t.Context(), []string{"-allow-empty-store"}, &out, noURL); err != nil || !strings.Contains(out.String(), "store: memory") {
		t.Fatalf("-allow-empty-store = %v\n%s", err, out.String())
	}
	if err := migrateCmd([]string{"threads"}, &out); err == nil {
		t.Fatal("an unknown migration was accepted")
	}
	if got := storeName("postgres", func(string) string { return "postgres://u:secret@db:5432/humagen" }); got != "postgres db:5432/humagen" {
		t.Fatalf("the store is named %q", got)
	}
}

// A dry run over a store holding a past GitHub run writes nothing, and says
// which store and tenant it read and what an apply would do.
func TestMigrateSessionsIsADryRunByDefault(t *testing.T) {
	_, b, load := pastGitHubStore(t, "acme", "gh")
	before := load()
	var out bytes.Buffer
	if err := migrateWith(t.Context(), b, "postgres db/humagen", migrateTarget{serverTenant: "default", getenv: noEnv}, false, false, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, load()) {
		t.Fatal("a dry run wrote to the store")
	}
	got := out.String()
	for _, want := range []string{"dry run: nothing is written", "store: postgres db/humagen", "tenants: acme", "tenant acme: 1 runs read", "WARNING: GitHub client gh is not in the store", "would write"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the dry run did not say %q:\n%s", want, got)
		}
	}
}

func noEnv(string) string { return "" }

// pastGitHubStore is a store holding one GitHub run from before sessions,
// asked by octocat on pull 12 through client in tenant, and a reading of
// what the migration could write: the backfill's stores, the clients and
// their secrets.
func pastGitHubStore(t *testing.T, tenant, client string) (state.Store, platform.Backends, func() map[string][]state.Record) {
	t.Helper()
	st := state.NewMemory()
	b := platform.Backends{State: st}
	seed, err := platform.New(platform.Config{Backends: b, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	at := "github://acme/handbook/pull/12?head_sha=" + strings.Repeat("a", 40)
	tag := wire.NewEvent(wire.Raise{ID: "gh-past", Agent: "docs", Tenant: tenant, Origin: "github", Text: "@docs what changed?", Channel: at, Actor: event.Actor{ID: "octocat"}, At: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), Context: map[string]string{runs.MetaClient: client}})
	tag.Source, tag.Type = at, event.TypeGitHubIssueCommentCreated
	tag.Data = event.GitHubIssueCommentCreated{
		Request:   event.Request{Text: "@docs what changed?", Actor: event.Actor{ID: "octocat"}, Mentions: []string{"docs"}, Context: wire.ContextOf(tag)},
		GitHubRef: event.GitHubRef{Owner: "acme", Repo: "handbook", Number: 12, Pull: true, Title: "Fix the handbook", Sender: "octocat"},
	}
	if err := seed.Runs.Record(runtime.RunID(tenant, "gh-past"), tag, 1); err != nil {
		t.Fatal(err)
	}
	_ = seed.Close()
	load := func() map[string][]state.Record {
		out := map[string][]state.Record{}
		for _, ns := range []string{"threads", "runs.v2", "agent-sessions", "session-audiences", "clients", "clients.secret"} {
			recs, err := st.Load(t.Context(), ns)
			if err != nil {
				t.Fatal(err)
			}
			out[ns] = recs
		}
		return out
	}
	return st, b, load
}

// The GitHub client the server's environment configures is never stored,
// so the migration builds it as serve does: its people map names who asked,
// and no warning says it maps nobody. Building it writes nothing.
func TestMigrateSessionsReadsTheClientTheEnvironmentConfigures(t *testing.T) {
	_, b, load := pastGitHubStore(t, "default", "default-github")
	env := map[string]string{
		EnvGitHubToken: "token", EnvGitHubSecret: "secret", EnvGitHubOwners: "acme",
		EnvGitHubPeople: "octocat=tailscale:ana@github",
	}
	getenv := func(name string) string { return env[name] }
	before := load()
	var out bytes.Buffer
	if err := migrateWith(t.Context(), b, "postgres db/humagen", migrateTarget{serverTenant: "default", getenv: getenv}, false, false, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, load()) {
		t.Fatal("a dry run wrote to the store")
	}
	got := out.String()
	if strings.Contains(got, "WARNING") {
		t.Fatalf("the dry run warned of the environment's client:\n%s", got)
	}
	for _, want := range []string{"environment clients: default-github in tenant default", "tenant default: 1 runs read", "asked by tailscale:ana@github", "github session opened"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the dry run did not say %q:\n%s", want, got)
		}
	}
	// Without the people map the client is still built, and the warning
	// says it maps nobody.
	delete(env, EnvGitHubPeople)
	out.Reset()
	if err := migrateWith(t.Context(), b, "postgres db/humagen", migrateTarget{serverTenant: "default", getenv: getenv}, false, false, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "WARNING: GitHub client default-github maps no GitHub login to a person") || !strings.Contains(got, "asked by nobody the client maps") {
		t.Fatalf("the dry run without a people map said:\n%s", got)
	}
}
