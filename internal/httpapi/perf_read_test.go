package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/readaudit"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
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

// quietFixture is a workspace whose store counts only what requests ask of
// it. A platform that runs writes the workspace through seed and is stopped;
// the platform that is then served is opened on the same store and never
// run, so it has no workers, no leader loops and no housekeeping, and the
// only statements the store sees are those of the requests a test makes.
// Each test checks that: the store must see nothing while no request is made.
func quietFixture(t *testing.T, st state.Store, seed func(p *platform.Platform)) (*fixture, *countingStore) {
	t.Helper()
	store := &countingStore{Store: st}
	cfg := platform.Config{
		Backends: platform.Backends{State: store}, Now: seedClock(),
		Workspace: t.TempDir(), Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4},
		Router: func(string) join.Asker { return join.Words },
	}
	writer, err := platform.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- writer.Run(ctx) }()
	seed(writer)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("the seeding platform stopped with %v", err)
	}
	served, err := platform.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f := serveFixture(t, served, nil)
	t.Cleanup(f.srv.Close)
	return f, store
}

// idle fails the test unless the store sees no statement while no request is
// made: what a test then counts is its own requests' and nothing else's.
func (c *countingStore) idle(t *testing.T) {
	t.Helper()
	// What the seeding platform or an earlier request left in flight ends first.
	for last, same := int64(-1), 0; same < 3; {
		now := c.lookups.Load() + c.many.Load() + c.lists.Load() + c.writes.Load()
		if now == last {
			same++
		} else {
			last, same = now, 0
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := c.during(func() { time.Sleep(250 * time.Millisecond) }); got.total() != 0 {
		t.Fatalf("the store saw %d statements with no request made: %v", got.total(), got.by)
	}
}

// The lists a console reads on every load cost a bounded number of
// statements however much the workspace holds: none reads a record per run,
// per session or per alert it passes over. The workspace here holds 66
// sessions, 400 runs and 18 alerts, and the reader is an operator, who reads
// nothing by exception, so no read is audited.
func TestListReadsAreBoundedByThePageNotTheWorkspace(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		reader := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Name: "Ana", Roles: []auth.Role{auth.Operator}}
		f, store := quietFixture(t, open(t), func(p *platform.Platform) {
			seedVolume(t, p, reader, volume{Sessions: 60, Children: 6, Runs: 400, Alerts: 18, Parallel: 4})
		})
		tok := f.person(t, "acme", "ana", "Ana", "humagen-operators")
		for _, c := range []struct {
			path string
			// most is the most statements the read may make, and rows the
			// most of them that may be a read of one record.
			most, rows int64
		}{
			// The newest 200 runs in one page, and their threads in one read.
			{"/api/runs", 12, 8},
			{"/api/threads", 22, 12},
			// A page of 50: the turn keys of each session on it, and no more.
			{"/api/sessions", 64, 8},
			{"/api/sessions?roots=true&mine=true&limit=1", 14, 8},
			{"/api/sessions?limit=5&status=done", 80, 8},
			{"/api/alerts?mine=true&state=open", 10, 6},
			{"/api/alerts/count", 8, 5},
		} {
			// The first read of a path may warm what later ones reuse.
			f.do(t, nil, "GET", c.path, tok, "")
			store.idle(t)
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
	})
}

// An alert a run raised is placed by its run, and the runs of every alert a
// read asks about are read together: ten alerts raised by runs cost what
// thirty do, for the alert list, the count, and the session listing that
// their alerts raise rows in.
func TestRunLinkedAlertsAreReadTogetherHoweverMany(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		paths := []string{"/api/alerts?mine=true&state=open", "/api/alerts/count", "/api/sessions?limit=5", "/api/sessions?limit=5&status=done"}
		cost := map[int]map[string]reads{}
		for _, n := range []int{1, 10, 30} {
			reader := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Name: "Ana", Roles: []auth.Role{auth.Operator}}
			f, store := quietFixture(t, open(t), func(p *platform.Platform) {
				seedVolume(t, p, reader, volume{Sessions: 20, Children: 2, Runs: 50, Alerts: n, Parallel: 4})
			})
			tok := f.person(t, "acme", "ana", "Ana", "humagen-operators")
			cost[n] = map[string]reads{}
			for _, path := range paths {
				f.do(t, nil, "GET", path, tok, "")
				store.idle(t)
				cost[n][path] = store.during(func() {
					if res, body := f.do(t, nil, "GET", path, tok, ""); res.StatusCode != http.StatusOK {
						t.Fatalf("GET %s = %d %v", path, res.StatusCode, body)
					}
				})
				t.Logf("%d alerts: GET %s made %d statements %v", n, path, cost[n][path].total(), cost[n][path].by)
			}
			if res, body := f.do(t, nil, "GET", "/api/alerts/count", tok, ""); int(body["waiting"].(float64)) != n-n/3 {
				t.Fatalf("%d alerts: count = %d %v, want %d", n, res.StatusCode, body, n-n/3)
			}
		}
		for _, path := range paths {
			one, ten, thirty := cost[1][path], cost[10][path], cost[30][path]
			if thirty.total() != ten.total() || one.total() > ten.total() {
				t.Errorf("GET %s made %d, %d and %d statements with 1, 10 and 30 alerts raised by runs; want no more for more alerts: %v", path, one.total(), ten.total(), thirty.total(), thirty.by)
			}
			if n := thirty.by["lookup runs.v2"]; n != 0 {
				t.Errorf("GET %s read %d runs one at a time with 30 alerts: %v", path, n, thirty.by)
			}
		}
	})
}

// A reader who reads by exception (projects:govern) is recorded for exactly
// what a page of sessions shows them: every returned session they see only
// by exception, with the session above it when the row names one they also
// see only by exception, and nothing that was examined to order, filter or
// cut the page. It holds for each paging filter, across pages.
func TestASessionPageRecordsExactlyTheExceptionalReadsItShows(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		// Ana, an administrator, starts three sessions in five; the rest are
		// other people's private sessions, which she reads by exception.
		ana := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Name: "Ana", Roles: []auth.Role{auth.Admin}}
		f := newFixtureWith(t, platform.Config{Backends: platform.Backends{State: open(t)}, Now: seedClock()})
		tok := f.person(t, "acme", "ana", "Ana", "humagen-admins")
		seedVolume(t, f.p, ana, volume{Sessions: 25, Children: 8, Runs: 60, Alerts: 6, Parallel: 4})
		// Whose each session is, and the session above it.
		owner, parent := map[string]string{}, map[string]string{}
		recs, err := f.p.Threads.Records("acme", func(threads.Thread) bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		for _, th := range recs {
			if th.Session != nil {
				owner[th.ID], parent[th.ID] = th.CreatedBy, th.Session.Parent
			}
		}
		audited := func() []readaudit.Record {
			t.Helper()
			all, err := f.p.ReadAudit.List("acme", time.Now().Add(-25*24*time.Hour), time.Now().Add(24*time.Hour), readaudit.MaxRows)
			if err != nil {
				t.Fatal(err)
			}
			if len(all) >= readaudit.MaxRows {
				t.Fatal("the test wrote more audit records than one read returns")
			}
			return all
		}
		exceptional := 0
		for _, filter := range []string{"", "&roots=true", "&mine=true", "&status=done", "&project=launch"} {
			after := ""
			for page := 0; page < 3; page++ {
				before := len(audited())
				res, body := f.do(t, nil, "GET", "/api/sessions?limit=3"+filter+"&after="+after, tok, "")
				if res.StatusCode != http.StatusOK {
					t.Fatalf("%q page %d = %d %v", filter, page, res.StatusCode, body)
				}
				want := map[string]bool{}
				for _, raw := range body["sessions"].([]any) {
					id := raw.(map[string]any)["id"].(string)
					if owner[id] == "" {
						t.Fatalf("%q page %d returned %s, which has no record", filter, page, id)
					}
					if owner[id] == "ana" {
						continue
					}
					want[id] = true
					exceptional++
					// The row names the session above it, which is the same
					// person's and as private.
					for up := parent[id]; strings.HasPrefix(up, "s_"); up = parent[up] {
						want[up] = true
					}
				}
				now := audited()
				got := map[string]bool{}
				// The records are newest first: the request's are the new ones.
				for _, r := range now[:len(now)-before] {
					if r.Subject != "ana" || r.Route != "GET /api/sessions" || r.Thread == "" {
						t.Errorf("%q page %d recorded %+v, which is no session ana was shown", filter, page, r)
					}
					got[r.Thread] = true
				}
				if !maps.Equal(got, want) {
					t.Errorf("%q page %d: recorded reads of %v, showed by exception %v", filter, page, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
				}
				next, _ := body["next"].(string)
				if next == "" {
					break
				}
				after = next
			}
		}
		if exceptional < 6 {
			t.Fatalf("only %d rows were shown by exception, too few to say anything", exceptional)
		}
	})
}

// When the record of a read by exception cannot be written, the response
// that would show it is refused whole: an error, no rows and no cursor,
// whichever row's record failed, and no record of any of its rows is kept,
// since nothing was shown. The same request answers once records can be
// written again, and then exactly its rows are recorded.
func TestASessionPageIsRefusedWholeWhenARecordCannotBeWritten(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		store := &failingAudit{Store: open(t)}
		f := newFixtureWith(t, platform.Config{Backends: platform.Backends{State: store}, Now: seedClock()})
		// Olu starts everything; Ana, an administrator, reads it by exception.
		olu := auth.Principal{Subject: "olu", Tenant: "acme", Kind: auth.User, Name: "Olu", Roles: []auth.Role{auth.Admin}}
		seedVolume(t, f.p, olu, volume{Sessions: 12, Children: 2, Runs: 20, Alerts: 0, Parallel: 4})
		ana := f.person(t, "acme", "ana", "Ana", "humagen-admins")
		// kept is the threads of the read records the store holds, in the
		// order they were written.
		kept := func() []string {
			t.Helper()
			all, err := f.p.ReadAudit.List("acme", time.Now().Add(-25*24*time.Hour), time.Now().Add(24*time.Hour), readaudit.MaxRows)
			if err != nil || len(all) >= readaudit.MaxRows {
				t.Fatalf("the read records = %d, %v", len(all), err)
			}
			out := make([]string, len(all))
			for i, r := range all {
				out[len(all)-1-i] = r.Thread
			}
			return out
		}
		page := func(path string) ([]string, string) {
			t.Helper()
			res, body := f.do(t, nil, "GET", path, ana, "")
			if res.StatusCode != http.StatusOK {
				t.Fatalf("GET %s = %d %v", path, res.StatusCode, body)
			}
			var ids []string
			for _, raw := range body["sessions"].([]any) {
				ids = append(ids, raw.(map[string]any)["id"].(string))
			}
			next, _ := body["next"].(string)
			return ids, next
		}
		// refused makes a request that must be refused, tell of no row, and
		// leave no read record behind.
		refused := func(path, why string, ids []string) {
			t.Helper()
			before := kept()
			res, body := f.do(t, nil, "GET", path, ana, "")
			if res.StatusCode != http.StatusNotFound || len(body) != 1 || body["error"] != "not found" {
				t.Fatalf("%s, %s: GET = %d %v, want 404 and nothing but that it was not found", path, why, res.StatusCode, body)
			}
			for name, values := range res.Header {
				for _, v := range values {
					for _, id := range ids {
						if strings.Contains(v, id) || strings.Contains(strings.ToLower(name), "next") {
							t.Fatalf("%s, %s: the refusal's header %s: %s tells of a row", path, why, name, v)
						}
					}
				}
			}
			if after := kept(); !slices.Equal(after, before) {
				t.Fatalf("%s, %s: the refused request left read records of %v", path, why, after[len(before):])
			}
		}
		// answered makes a request that must answer, and returns the threads
		// it left read records of.
		answered := func(get func()) []string {
			t.Helper()
			before := len(kept())
			get()
			return slices.Sorted(slices.Values(kept()[before:]))
		}
		for _, path := range []string{"/api/sessions?limit=3", "/api/sessions?limit=3&status=done"} {
			ids, next := page(path)
			if len(ids) != 3 || next == "" {
				t.Fatalf("GET %s = %v, next %q; want a page of 3 and a page after it", path, ids, next)
			}
			// What a page that answers records: its rows, each with the
			// session above it.
			want := map[string]bool{}
			for _, id := range ids {
				want[id] = true
				_, body := f.do(t, nil, "GET", "/api/sessions/"+id, ana, "")
				if up, _ := body["parent"].(string); strings.HasPrefix(up, "s_") {
					want[up] = true
				}
			}
			store.all.Store(true)
			refused(path, "no record can be written", ids)
			store.all.Store(false)
			for i, where := range []string{"the first row", "the middle row", "the last row, which the cursor names"} {
				store.thread.Store(ids[i])
				refused(path, "the record of "+where+" cannot be written", ids)
				store.thread.Store("")
				got := answered(func() {
					if again, after := page(path); !slices.Equal(again, ids) || after != next {
						t.Fatalf("GET %s once records can be written = %v, next %q; want %v, %q", path, again, after, ids, next)
					}
				})
				if !slices.Equal(got, slices.Sorted(maps.Keys(want))) {
					t.Fatalf("GET %s once records can be written recorded %v, want exactly its rows %v", path, got, slices.Sorted(maps.Keys(want)))
				}
			}
		}
		// A session's own read shows the sessions under it too: when the
		// record of one of those cannot be written the read is refused, and
		// no record is kept of the session itself either.
		all, _ := page("/api/sessions?limit=200")
		under := map[string][]string{}
		for _, id := range all {
			_, body := f.do(t, nil, "GET", "/api/sessions/"+id, ana, "")
			if up, _ := body["parent"].(string); strings.HasPrefix(up, "s_") {
				under[up] = append(under[up], id)
			}
		}
		if len(under) == 0 {
			t.Fatal("no session under another was seeded")
		}
		for up, children := range under {
			path := "/api/sessions/" + up
			for _, fail := range []string{up, children[0]} {
				store.thread.Store(fail)
				refused(path, "the record of "+fail+" cannot be written", append([]string{up}, children...))
				store.thread.Store("")
			}
			store.all.Store(true)
			refused(path, "no record can be written", append([]string{up}, children...))
			store.all.Store(false)
			got := answered(func() {
				if res, _ := f.do(t, nil, "GET", path, ana, ""); res.StatusCode != http.StatusOK {
					t.Fatalf("GET %s once records can be written = %d", path, res.StatusCode)
				}
			})
			if want := slices.Sorted(slices.Values(append([]string{up}, children...))); !slices.Equal(got, want) {
				t.Fatalf("GET %s recorded %v, want the session and those under it %v", path, got, want)
			}
		}
	})
}

// auditFixture is a workspace where Olu started every session and Ana, an
// administrator, reads them by exception, on a store whose read records can
// be made to fail. kept is the threads of the read records it holds, oldest
// first, and raw makes a request with a client that gives up after two
// seconds, returning the response once its body has been read to the end.
type auditFixture struct {
	*fixture
	store    *failingAudit
	ana      string
	sessions []string
}

func newAuditFixture(t *testing.T, st state.Store) *auditFixture {
	t.Helper()
	store := &failingAudit{Store: st}
	f := newFixtureWith(t, platform.Config{Backends: platform.Backends{State: store}, Now: seedClock()})
	olu := auth.Principal{Subject: "olu", Tenant: "acme", Kind: auth.User, Name: "Olu", Roles: []auth.Role{auth.Admin}}
	seedVolume(t, f.p, olu, volume{Sessions: 4, Children: 0, Runs: 6, Alerts: 0, Parallel: 2})
	a := &auditFixture{fixture: f, store: store, ana: f.person(t, "acme", "ana", "Ana", "humagen-admins")}
	recs, err := f.p.Threads.Records("acme", func(th threads.Thread) bool { return th.Session != nil })
	if err != nil || len(recs) != 4 {
		t.Fatalf("seeded sessions = %d, %v", len(recs), err)
	}
	for _, th := range recs {
		a.sessions = append(a.sessions, th.ID)
	}
	slices.Sort(a.sessions)
	return a
}

func (a *auditFixture) kept(t *testing.T) []string {
	t.Helper()
	all, err := a.p.ReadAudit.List("acme", time.Now().Add(-25*24*time.Hour), time.Now().Add(24*time.Hour), readaudit.MaxRows)
	if err != nil || len(all) >= readaudit.MaxRows {
		t.Fatalf("the read records = %d, %v", len(all), err)
	}
	out := make([]string, len(all))
	for i, r := range all {
		out[len(all)-1-i] = r.Thread
	}
	return out
}

// raw answers with the response, its whole body and how long both took. open
// is called once the headers have arrived, before the body is read.
func (a *auditFixture) raw(t *testing.T, method, path string, open func(*http.Response)) (*http.Response, []byte, time.Duration, error) {
	t.Helper()
	req, err := http.NewRequest(method, a.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+a.ana)
	started := time.Now()
	res, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return nil, nil, time.Since(started), err
	}
	defer res.Body.Close()
	if open != nil {
		open(res)
	}
	body, err := io.ReadAll(res.Body)
	return res, body, time.Since(started), err
}

// A HEAD of a route registered for GET is a read like the GET: what it reads
// by exception is recorded before its headers leave, the same records the
// GET writes, and when they cannot be written it answers 404 with none of
// the handler's headers and no record kept.
func TestAHeadIsRecordedAndRefusedAsItsGetIs(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		a := newAuditFixture(t, open(t))
		path := "/api/sessions/" + a.sessions[0]
		before := len(a.kept(t))
		if res, body, _, err := a.raw(t, "GET", path, nil); err != nil || res.StatusCode != http.StatusOK || len(body) == 0 {
			t.Fatalf("GET = %v, %v", res, err)
		}
		byGet := a.kept(t)[before:]
		if !slices.Equal(byGet, []string{a.sessions[0]}) {
			t.Fatalf("the GET recorded %v, want the session", byGet)
		}
		before = len(a.kept(t))
		res, body, _, err := a.raw(t, "HEAD", path, nil)
		if err != nil || res.StatusCode != http.StatusOK || len(body) != 0 || res.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("HEAD = %v, a body of %d bytes, %v", res, len(body), err)
		}
		if byHead := a.kept(t)[before:]; !slices.Equal(byHead, byGet) {
			t.Fatalf("the HEAD recorded %v, its GET %v", byHead, byGet)
		}
		// The stream's handler sets headers of its own before it answers.
		for _, path := range []string{path, path + "/stream"} {
			a.store.all.Store(true)
			before := a.kept(t)
			res, body, took, err := a.raw(t, "HEAD", path, nil)
			a.store.all.Store(false)
			if err != nil || res.StatusCode != http.StatusNotFound || len(body) != 0 {
				t.Fatalf("HEAD %s with no record written = %v, %v after %s", path, res, err, took)
			}
			if res.Header.Get("Cache-Control") == "no-cache" || res.Header.Get("X-Accel-Buffering") != "" || strings.Contains(res.Header.Get("Content-Type"), "event-stream") {
				t.Fatalf("the refused HEAD %s carries the handler's headers: %v", path, res.Header)
			}
			if after := a.kept(t); !slices.Equal(after, before) {
				t.Fatalf("the refused HEAD %s left read records of %v", path, after[len(before):])
			}
		}
	})
}

// A stream whose opening read cannot be recorded is refused at once and ends:
// the client is told 404 and reaches the end of the answer, which it does
// only when the handler has returned, well inside its two seconds; the room
// has no viewer left; and no record is kept. With records written the same
// stream opens.
func TestARefusedStreamAnswersAtOnceAndEnds(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		a := newAuditFixture(t, open(t))
		session := a.sessions[0]
		turns, err := a.p.Runs.Conversation("acme", session)
		if err != nil || len(turns) == 0 {
			t.Fatalf("the session's runs = %d, %v", len(turns), err)
		}
		// The workspace's stream reads nothing by exception before its first
		// byte, so there is nothing to refuse it for: it opens, and each
		// event it would show by exception is recorded as it is granted, or
		// not sent. With no record written it shows this run nothing and
		// keeps no record.
		a.store.all.Store(true)
		kept := a.kept(t)
		opened := false
		_, _, _, _ = a.raw(t, "GET", "/api/events?run="+turns[0].ID, func(res *http.Response) {
			opened = res.StatusCode == http.StatusOK
			res.Body.Close()
		})
		a.store.all.Store(false)
		if after := a.kept(t); !opened || !slices.Equal(after, kept) {
			t.Fatalf("the workspace stream with no record written: opened %v, records of %v", opened, after[len(kept):])
		}
		for name, path := range map[string]string{
			"session stream": "/api/sessions/" + session + "/stream",
			"room stream":    "/api/events?thread=" + session,
		} {
			a.store.all.Store(true)
			before := a.kept(t)
			res, body, took, err := a.raw(t, "GET", path, nil)
			a.store.all.Store(false)
			if err != nil {
				t.Fatalf("%s, refused: %v after %s", name, err, took)
			}
			if res.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "not found") || strings.Contains(res.Header.Get("Content-Type"), "event-stream") {
				t.Fatalf("%s, refused = %d %q %v", name, res.StatusCode, body, res.Header)
			}
			if took > time.Second {
				t.Fatalf("%s: the refusal and the end of the stream took %s", name, took)
			}
			// The handler had returned when the answer ended, and left the room.
			if n := a.p.Rooms.Viewers("acme", session); n != 0 {
				t.Fatalf("%s: the room still has %d viewers after the refusal", name, n)
			}
			if after := a.kept(t); !slices.Equal(after, before) {
				t.Fatalf("%s: the refused stream left read records of %v", name, after[len(before):])
			}
			// With records written the stream opens; the client then leaves.
			opened := false
			_, _, _, _ = a.raw(t, "GET", path, func(res *http.Response) {
				opened = res.StatusCode == http.StatusOK && strings.Contains(res.Header.Get("Content-Type"), "event-stream")
				res.Body.Close()
			})
			if !opened {
				t.Fatalf("%s did not open once its records could be written", name)
			}
			// The client has left: the open stream's handler lets go of the
			// room before the next case counts its viewers.
			for deadline := time.Now().Add(2 * time.Second); a.p.Rooms.Viewers("acme", session) != 0; time.Sleep(5 * time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatalf("%s: the room kept %d viewers after its client left", name, a.p.Rooms.Viewers("acme", session))
				}
			}
		}
	})
}

// failingAudit fails the writes of read records: all of them while all is
// set, or those of one thread while thread names it.
type failingAudit struct {
	state.Store
	all    atomic.Bool
	thread atomic.Value
}

func (s *failingAudit) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	if namespace == "read-audit" {
		thread, _ := s.thread.Load().(string)
		for _, op := range ops {
			if s.all.Load() || (thread != "" && strings.Contains(string(op.Value), `"thread":"`+thread+`"`)) {
				return statetest.ErrInjected
			}
		}
	}
	return s.Store.Write(ctx, namespace, ops...)
}

func (s *failingAudit) LookupMany(ctx context.Context, namespace, tenant string, keys []string) ([]state.Record, error) {
	return state.LookupMany(ctx, s.Store, namespace, tenant, keys)
}

// A session read whose runs cannot be read is shown without them, and still
// names its people as the directory names them, as the same read does when
// its runs can be read.
func TestASessionShownWithoutItsRunsNamesPeopleAsTheDirectoryDoes(t *testing.T) {
	store := &failingRuns{Store: state.NewMemory()}
	f := newFixtureWith(t, platform.Config{Backends: platform.Backends{State: store}, Now: seedClock()})
	// A subject that is not the handle the directory gives the person.
	tok := f.person(t, "acme", "auth0|7f3a", "Ana Lima", "humagen-admins")
	reader := auth.Principal{Subject: "auth0|7f3a", Tenant: "acme", Kind: auth.User, Name: "Ana Lima", Roles: []auth.Role{auth.Admin}}
	seedVolume(t, f.p, reader, volume{Sessions: 1, Children: 0, Runs: 2, Alerts: 0, Parallel: 1})
	_, list := f.do(t, nil, "GET", "/api/sessions?limit=1", tok, "")
	id := list["sessions"].([]any)[0].(map[string]any)["id"].(string)
	read := func() map[string]any {
		t.Helper()
		res, body := f.do(t, nil, "GET", "/api/sessions/"+id, tok, "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("session = %d %v", res.StatusCode, body)
		}
		return body["conversation"].(map[string]any)
	}
	whole := read()
	store.fail.Store(true)
	without := read()
	store.fail.Store(false)
	if store.failed.Load() == 0 {
		t.Fatal("no read of the session's runs failed, so the fallback was not taken")
	}
	people := fmt.Sprint(whole["people"])
	if strings.Contains(people, "auth0|7f3a") || people == "[]" {
		t.Fatalf("with its runs, the session names its people %s", people)
	}
	if got := fmt.Sprint(without["people"]); got != people {
		t.Errorf("without its runs the session names its people %s, with them %s", got, people)
	}
	if by, _ := without["last_by"].(string); strings.Contains(by, "auth0|7f3a") {
		t.Errorf("without its runs the session says %q spoke last, a subject", by)
	}
}

// failingRuns fails the reads of several runs at once while fail is set, as
// a database that drops a statement does.
type failingRuns struct {
	state.Store
	fail   atomic.Bool
	failed atomic.Int64
}

func (s *failingRuns) LookupMany(ctx context.Context, namespace, tenant string, keys []string) ([]state.Record, error) {
	if s.fail.Load() && namespace == "runs.v2" && len(keys) > 0 && strings.HasPrefix(keys[0], "o:") {
		s.failed.Add(1)
		return nil, errors.New("the statement was dropped")
	}
	return state.LookupMany(ctx, s.Store, namespace, tenant, keys)
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
