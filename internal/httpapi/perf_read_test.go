package httpapi

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/state"
)

// countingStore counts the reads and writes a store is asked for, each of
// which is one statement on Postgres.
type countingStore struct {
	state.Store
	lookups, many, lists, writes atomic.Int64
	mu                           sync.Mutex
	by                           map[string]int
}

// note counts one statement against its kind and namespace.
func (c *countingStore) note(kind, namespace string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.by == nil {
		c.by = map[string]int{}
	}
	c.by[kind+" "+namespace]++
}

func (c *countingStore) Get(ctx context.Context, namespace, tenant, key string) ([]byte, error) {
	c.lookups.Add(1)
	c.note("lookup", namespace)
	return c.Store.Get(ctx, namespace, tenant, key)
}

func (c *countingStore) Lookup(ctx context.Context, namespace, tenant, key string) (state.Record, error) {
	c.lookups.Add(1)
	c.note("lookup", namespace)
	return c.Store.Lookup(ctx, namespace, tenant, key)
}

// LookupMany counts as the one statement it is on Postgres.
func (c *countingStore) LookupMany(ctx context.Context, namespace, tenant string, keys []string) ([]state.Record, error) {
	c.many.Add(1)
	c.note("many", namespace)
	return c.Store.(state.Many).LookupMany(ctx, namespace, tenant, keys)
}

func (c *countingStore) List(ctx context.Context, namespace, tenant string, q state.Query) ([]state.Record, string, error) {
	c.lists.Add(1)
	c.note("list", namespace)
	return c.Store.List(ctx, namespace, tenant, q)
}

func (c *countingStore) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	c.writes.Add(1)
	c.note("write", namespace)
	return c.Store.Write(ctx, namespace, ops...)
}

// reads is what one request asked the store for.
type reads struct {
	lookups, many, lists, writes int64
	// by is the count of each kind of statement in each namespace.
	by map[string]int
}

func (r reads) total() int64 { return r.lookups + r.many + r.lists + r.writes }

// during counts the store's statements while fn runs.
func (c *countingStore) during(fn func()) reads {
	c.mu.Lock()
	c.by = nil
	c.mu.Unlock()
	before := reads{c.lookups.Load(), c.many.Load(), c.lists.Load(), c.writes.Load(), nil}
	fn()
	c.mu.Lock()
	defer c.mu.Unlock()
	return reads{c.lookups.Load() - before.lookups, c.many.Load() - before.many, c.lists.Load() - before.lists, c.writes.Load() - before.writes, c.by}
}

// The lists a console reads on every load cost a bounded number of
// statements however much the workspace holds: none reads a record per run,
// per session or per alert it passes over. The workspace here holds 66
// sessions, 400 runs and 18 alerts, and the reader is an operator, who reads
// nothing by exception, so no read is audited.
func TestListReadsAreBoundedByThePageNotTheWorkspace(t *testing.T) {
	store := &countingStore{Store: state.NewMemory()}
	f := newFixtureWith(t, platform.Config{Backends: platform.Backends{State: store}, Now: seedClock()})
	tok := f.person(t, "acme", "ana", "Ana", "humagen-operators")
	reader := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Name: "Ana", Roles: []auth.Role{auth.Operator}}
	seedVolume(t, f.p, reader, volume{Sessions: 60, Children: 6, Runs: 400, Alerts: 18, Parallel: 4})
	for _, c := range []struct {
		path string
		// most is the most statements the read may make, and rows the most
		// of them that may be a read of one record.
		most, rows int64
	}{
		// The newest 200 runs in one page, and their threads in one read.
		{"/api/runs", 12, 8},
		// A page of 50: the turn keys of each session on it, and no more.
		{"/api/sessions", 64, 8},
		{"/api/sessions?roots=true&mine=true&limit=1", 14, 8},
		{"/api/sessions?limit=5&status=done", 80, 8},
		{"/api/alerts?mine=true&state=open", 10, 6},
		{"/api/alerts/count", 8, 5},
	} {
		// The first read of a path may warm what later ones reuse.
		f.do(t, nil, "GET", c.path, tok, "")
		var status int
		got := store.during(func() {
			res, _ := f.do(t, nil, "GET", c.path, tok, "")
			status = res.StatusCode
		})
		t.Logf("GET %s = %d: %d statements: %d lookups, %d batched reads, %d lists, %d writes %v", c.path, status, got.total(), got.lookups, got.many, got.lists, got.writes, got.by)
		if status != http.StatusOK {
			t.Errorf("GET %s = %d", c.path, status)
		}
		if got.total() > c.most || got.lookups > c.rows {
			t.Errorf("GET %s made %d statements, %d of them single reads; want at most %d and %d: %v", c.path, got.total(), got.lookups, c.most, c.rows, got.by)
		}
		if got.writes != 0 {
			t.Errorf("GET %s wrote %d times: %v", c.path, got.writes, got.by)
		}
	}
}

// A reader who reads by exception (projects:govern) is audited for the
// private sessions a page shows them, not for every session the page was
// cut from: a page of five audits at most six reads, the five and the one
// that says a page follows.
func TestASessionPageAuditsOnlyWhatItShows(t *testing.T) {
	store := &countingStore{Store: state.NewMemory()}
	f := newFixtureWith(t, platform.Config{Backends: platform.Backends{State: store}, Now: seedClock()})
	// Olu starts everything; Ana, an administrator, takes part in none of it.
	olu := auth.Principal{Subject: "olu", Tenant: "acme", Kind: auth.User, Name: "Olu", Roles: []auth.Role{auth.Admin}}
	seedVolume(t, f.p, olu, volume{Sessions: 30, Children: 0, Runs: 40, Alerts: 0, Parallel: 4})
	ana := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	var shown int
	got := store.during(func() {
		res, body := f.do(t, nil, "GET", "/api/sessions?limit=5", ana, "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("sessions = %d %v", res.StatusCode, body)
		}
		shown = len(body["sessions"].([]any))
	})
	if shown != 5 {
		t.Fatalf("the page holds %d sessions, want 5", shown)
	}
	// Each record is a read of the count and a write; the first also marks
	// the tenant as holding records.
	if n, writes := got.by["lookup read-audit"], got.by["write read-audit"]; n < 1 || n > 6 || writes > n+1 {
		t.Fatalf("a page of 5 audited %d reads in %d writes, want 1 to 6: %v", n, writes, got.by)
	}
}

// The count of what waits on a person is the count of what their alert list
// holds, is theirs alone, and never counts another workspace's.
func TestAlertCountIsWhatTheListHoldsForTheCallerAlone(t *testing.T) {
	f := newFixtureWith(t, platform.Config{Now: seedClock()})
	ana := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	reader := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Name: "Ana", Roles: []auth.Role{auth.Admin}}
	seeded := seedVolume(t, f.p, reader, volume{Sessions: 10, Children: 2, Runs: 20, Alerts: 9, Parallel: 4})

	count := func(tok string) int {
		t.Helper()
		res, body := f.do(t, nil, "GET", "/api/alerts/count", tok, "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("count = %d %v", res.StatusCode, body)
		}
		return int(body["waiting"].(float64))
	}
	listed := func(tok string) int {
		t.Helper()
		res, body := f.do(t, nil, "GET", "/api/alerts?mine=true&state=open", tok, "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("list = %d %v", res.StatusCode, body)
		}
		n := 0
		for _, raw := range body["alerts"].([]any) {
			a := raw.(map[string]any)
			if a["session"] != nil && a["session"] != "" && a["priority"] != "low" {
				n++
			}
		}
		return n
	}
	if got := count(ana); got != seeded.Waiting || got != listed(ana) || got != 6 {
		t.Fatalf("ana's count = %d, her list holds %d, seeded %d", got, listed(ana), seeded.Waiting)
	}
	// An operator in the same workspace is addressed by none of them, and
	// reads none of ana's private sessions.
	if got := count(f.person(t, "acme", "olu", "Olu", "humagen-operators")); got != 0 {
		t.Fatalf("another person's count = %d, want 0", got)
	}
	// Another workspace's person, with the same subject, counts their own.
	if got := count(f.person(t, "elsewhere", "ana", "Ana", "humagen-admins")); got != 0 {
		t.Fatalf("another workspace's count = %d, want 0", got)
	}
	// A machine without alerts:read is refused.
	if res, _ := f.do(t, nil, "GET", "/api/alerts/count", f.machine(t, "acme", "runs:read"), ""); res.StatusCode != http.StatusForbidden {
		t.Fatalf("without alerts:read = %d, want 403", res.StatusCode)
	}
	// Resolving one takes it off the count.
	_, body := f.do(t, nil, "GET", "/api/alerts?mine=true&state=open&priority=urgent", ana, "")
	id := body["alerts"].([]any)[0].(map[string]any)["id"].(string)
	if res, out := f.do(t, nil, "PATCH", "/api/alerts/"+id, ana, `{"state":"resolved"}`); res.StatusCode != http.StatusOK {
		t.Fatalf("resolve = %d %v", res.StatusCode, out)
	}
	if got := count(ana); got != 5 || got != listed(ana) {
		t.Fatalf("after resolving one, count = %d and the list holds %d, want 5", got, listed(ana))
	}
}

// Paging the sessions reads each once, in the order one whole page holds
// them, with the reader's open alerts first: only a page is summarized, and
// the cursor still names a place in the whole listing.
func TestSessionPagesAreTheWholeListingInOrder(t *testing.T) {
	f := newFixtureWith(t, platform.Config{Now: seedClock()})
	tok := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	reader := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Name: "Ana", Roles: []auth.Role{auth.Admin}}
	seedVolume(t, f.p, reader, volume{Sessions: 23, Children: 4, Runs: 60, Alerts: 6, Parallel: 4})
	ids := func(path string) ([]string, string) {
		t.Helper()
		res, body := f.do(t, nil, "GET", path, tok, "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d %v", path, res.StatusCode, body)
		}
		var out []string
		for _, raw := range body["sessions"].([]any) {
			out = append(out, raw.(map[string]any)["id"].(string))
		}
		next, _ := body["next"].(string)
		return out, next
	}
	for _, filter := range []string{"", "&roots=true", "&mine=true", "&status=done", "&project=launch"} {
		whole, next := ids("/api/sessions?limit=200" + filter)
		if next != "" || len(whole) == 0 {
			t.Fatalf("%q: the whole listing = %d sessions, next %q", filter, len(whole), next)
		}
		var paged []string
		for after, pages := "", 0; ; pages++ {
			if pages > 20 {
				t.Fatalf("%q: the pages do not end", filter)
			}
			page, next := ids("/api/sessions?limit=4" + filter + "&after=" + after)
			paged = append(paged, page...)
			if next == "" {
				break
			}
			after = next
		}
		if !slices.Equal(paged, whole) {
			t.Errorf("%q: paged = %v\nwhole = %v", filter, paged, whole)
		}
	}
	// The reader's open alerts raise their sessions to the top.
	whole, _ := ids("/api/sessions?limit=200")
	_, body := f.do(t, nil, "GET", "/api/alerts?mine=true&state=open", tok, "")
	raised := map[string]bool{}
	for _, raw := range body["alerts"].([]any) {
		raised[raw.(map[string]any)["session"].(string)] = true
	}
	for i, id := range whole {
		if raised[id] != (i < len(raised)) {
			t.Fatalf("session %d of the listing (%s): raised = %v, with %d raised sessions", i, id, raised[id], len(raised))
		}
	}
}
